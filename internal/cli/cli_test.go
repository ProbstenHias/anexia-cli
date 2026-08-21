package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ProbstenHias/anexia-cli/internal/cli"
	"github.com/ProbstenHias/anexia-cli/internal/errmap"
)

// isolate points config discovery at an empty temporary directory so tests
// never read the developer's real config file or environment.
func isolate(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	t.Setenv("ANEXIA_CONFIG", path)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("ANEXIA_TOKEN", "")
	t.Setenv("ANEXIA_API_BASE_URL", "")

	return path
}

// run executes the root command and returns stdout, stderr, and the error.
func run(t *testing.T, args ...string) (stdoutText, stderrText string, err error) {
	t.Helper()

	return runWithInput(t, "", args...)
}

// runWithInput is run with stdin wired up, for the confirmation prompts.
func runWithInput(t *testing.T, input string, args ...string) (stdoutText, stderrText string, err error) {
	t.Helper()

	var stdout, stderr bytes.Buffer

	cmd := cli.NewRootCommand(cli.Deps{Stdout: &stdout, Stderr: &stderr})
	cmd.SetArgs(args)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(input))

	err = cmd.Execute()

	return stdout.String(), stderr.String(), err
}

// server serves a canned JSON body on any path and records the last request.
func server(t *testing.T, status int, body string) (srv *httptest.Server, last *request) {
	t.Helper()

	seen := request{}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.path = r.URL.Path
		seen.query = r.URL.RawQuery
		seen.method = r.Method

		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			seen.body = string(raw)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv, &seen
}

// request captures what the CLI sent, so tests can assert on the wire format.
type request struct {
	method string
	path   string
	query  string
	body   string
}

func TestRootWithNoArgsPrintsHelp(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t)
	require.NoError(t, err)
	require.Contains(t, stdout, "anexia")
	require.Contains(t, stdout, "core")
	require.Contains(t, stdout, "config")
	require.Contains(t, stdout, "version")
}

func TestRootHelpListsGlobalFlags(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t)
	require.NoError(t, err)
	require.Contains(t, stdout, "--no-headers")
	require.Contains(t, stdout, "--yes")
	require.Contains(t, stdout, "table, json, yaml, tsv")
}

func TestVersionCommand(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "version")
	require.NoError(t, err)
	require.Contains(t, stdout, "anexia dev")
}

func TestRootRejectsOldLocationCommand(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "location", "list")
	require.ErrorContains(t, err, `unknown command "location"`)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
}

// TestUsageMistakesExitWithUsageCode pins the exit code for every way a user
// can get the invocation wrong, since scripts branch on it.
func TestUsageMistakesExitWithUsageCode(t *testing.T) {
	isolate(t)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown command", []string{"bogus"}, `unknown command "bogus"`},
		{"unknown command in group", []string{"core", "bogus"}, `unknown command "bogus"`},
		{"unknown flag", []string{"core", "location", "list", "--bogus"}, "unknown flag: --bogus"},
		{"too many arguments", []string{"core", "location", "get", "a", "b"}, "accepts 1 arg(s), received 2"},
		{"missing argument", []string{"core", "location", "get"}, "accepts 1 arg(s), received 0"},
		{"argument to a group", []string{"config", "view", "extra"}, "unknown command"},
		{"invalid output format", []string{"core", "location", "list", "-o", "xml"}, "invalid output format"},
		{"invalid page", []string{"core", "location", "list", "--page", "0"}, "--page 0 must be"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := run(t, tt.args...)

			require.ErrorContains(t, err, tt.want)
			require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err), "%v must exit with the usage code", tt.args)
		})
	}
}

func TestMissingTokenExitsWithAuthCode(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "core", "location", "list")

	require.ErrorContains(t, err, "not authenticated")
	require.Equal(t, errmap.ExitAuth, errmap.ExitCode(err))
}

// TestEngineFailuresExitWithTheDocumentedCode walks the real command tree so
// the exit-code contract is checked end to end, on both clients. The generic
// and legacy commands must agree: the client a command happens to use is an
// implementation detail, not something a script should have to know.
func TestEngineFailuresExitWithTheDocumentedCode(t *testing.T) {
	commands := map[string][]string{
		"generic": {"core", "location", "get", "id-1"},
		"legacy":  {"core", "tag", "get", "t-1"},
	}

	statuses := []struct {
		name   string
		status int
		want   int
	}{
		{"unauthorized", http.StatusUnauthorized, errmap.ExitAuth},
		{"forbidden", http.StatusForbidden, errmap.ExitAuth},
		{"not found", http.StatusNotFound, errmap.ExitNotFound},
		{"too many requests", http.StatusTooManyRequests, errmap.ExitRateLimited},
		{"server error", http.StatusInternalServerError, errmap.ExitError},
	}

	// The Engine does not always repeat its status in the response body, and
	// the exit code must not depend on whether it does.
	bodies := map[string]func(int) string{
		"body echoes the status": func(status int) string {
			return `{"error":{"code":` + strconv.Itoa(status) + `,"message":"nope"}}`
		},
		"body omits the status": func(int) string {
			return `{"error":{"message":"nope"}}`
		},
	}

	for client, args := range commands {
		for body, render := range bodies {
			for _, tt := range statuses {
				t.Run(client+" "+body+" "+tt.name, func(t *testing.T) {
					isolate(t)
					srv, _ := server(t, tt.status, render(tt.status))

					_, _, err := run(t, append(args, "--token", "tok", "--api-base-url", srv.URL)...)

					require.Error(t, err)
					require.Equal(t, tt.want, errmap.ExitCode(err))
				})
			}
		}
	}
}

// TestTimeoutExitsWithTimeoutCode pins exit code 5 on both clients.
func TestTimeoutExitsWithTimeoutCode(t *testing.T) {
	commands := map[string][]string{
		"generic": {"core", "location", "list"},
		"legacy":  {"core", "tag", "list"},
	}

	for client, args := range commands {
		t.Run(client, func(t *testing.T) {
			isolate(t)

			srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				<-r.Context().Done()
			}))
			t.Cleanup(srv.Close)

			_, _, err := run(t, append(args,
				"--timeout", "20ms", "--token", "tok", "--api-base-url", srv.URL)...)

			require.Error(t, err)
			require.Equal(t, errmap.ExitTimeout, errmap.ExitCode(err))
			require.Contains(t, errmap.Message(err), "raise it with --timeout")
		})
	}
}

// TestDeclinedConfirmationExitsWithCanceledCode pins exit code 7.
func TestDeclinedConfirmationExitsWithCanceledCode(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, `{}`)

	_, stderrText, err := runWithInput(t, "n\n", "core", "tag", "delete", "t-1",
		"--service", "s-1", "--token", "tok", "--api-base-url", srv.URL)

	require.Error(t, err)
	require.Equal(t, errmap.ExitCanceled, errmap.ExitCode(err))
	require.Contains(t, stderrText, `delete tag "t-1"`)
	require.Empty(t, last.path, "a declined confirmation must not reach the Engine")
}

// TestEngineFailureMessagesAreReadable pins that the legacy client's struct
// dump never reaches the user.
func TestEngineFailureMessagesAreReadable(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusNotFound, `{"error":{"code":404,"message":"tag not found"}}`)

	_, _, err := run(t, "core", "tag", "get", "t-1", "--token", "tok", "--api-base-url", srv.URL)

	require.Error(t, err)
	require.Contains(t, errmap.Message(err), `reading tag "t-1"`)
	require.Contains(t, errmap.Message(err), "tag not found (404)")
	require.NotContains(t, errmap.Message(err), "received error from api")
}

// TestNounsAcceptTheirPlural pins the documented alias on every noun, whether
// it is registry-driven or hand-written.
func TestNounsAcceptTheirPlural(t *testing.T) {
	t.Parallel()

	plurals := []string{"locations", "resources", "tags", "services"}

	for _, plural := range plurals {
		t.Run(plural, func(t *testing.T) {
			t.Parallel()

			root := cli.NewRootCommand(cli.Deps{})

			found, _, err := root.Find([]string{"core", plural, "list"})

			require.NoError(t, err)
			require.Equal(t, "list", found.Name())
		})
	}
}

func TestConfigPath(t *testing.T) {
	path := isolate(t)

	stdout, _, err := run(t, "config", "path")
	require.NoError(t, err)
	require.Equal(t, path+"\n", stdout)
}

func TestConfigInitCreatesFile(t *testing.T) {
	path := isolate(t)

	stdout, _, err := run(t, "config", "init")
	require.NoError(t, err)
	require.Contains(t, stdout, path)

	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

func TestConfigInitRefusesExistingFile(t *testing.T) {
	path := isolate(t)
	require.NoError(t, os.WriteFile(path, []byte("token: t\n"), 0o600))

	_, _, err := run(t, "config", "init")
	require.ErrorContains(t, err, "already exists")

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "token: t\n", string(body), "existing config must be untouched")
}

func TestConfigInitForceOverwrites(t *testing.T) {
	path := isolate(t)
	require.NoError(t, os.WriteFile(path, []byte("token: t\n"), 0o600))

	_, _, err := run(t, "config", "init", "--force")
	require.NoError(t, err)

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "{}\n", string(body))
}

func TestConfigSetThenGet(t *testing.T) {
	path := isolate(t)

	_, _, err := run(t, "config", "set", "token", "abcdefghij")
	require.NoError(t, err)

	_, _, err = run(t, "config", "set", "api_base_url", "https://engine.example.com")
	require.NoError(t, err)

	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())

	stdout, _, err := run(t, "config", "get", "token")
	require.NoError(t, err)
	require.Equal(t, "******ghij\n", stdout, "token must be masked")

	stdout, _, err = run(t, "config", "get", "api_base_url")
	require.NoError(t, err)
	require.Equal(t, "https://engine.example.com\n", stdout)
}

func TestConfigSetPreservesOtherKeys(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "config", "set", "token", "abcdefghij")
	require.NoError(t, err)
	_, _, err = run(t, "config", "set", "api_base_url", "https://engine.example.com")
	require.NoError(t, err)

	stdout, _, err := run(t, "config", "get", "token")
	require.NoError(t, err)
	require.Equal(t, "******ghij\n", stdout)
}

func TestConfigSetUnknownKey(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "config", "set", "nope", "x")
	require.ErrorContains(t, err, `unknown config key "nope"`)
	require.ErrorContains(t, err, "token, api_base_url")
}

func TestConfigGetUnknownKey(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "config", "get", "nope")
	require.ErrorContains(t, err, `unknown config key "nope"`)
}

func TestConfigViewTableMasksToken(t *testing.T) {
	path := isolate(t)
	require.NoError(t, os.WriteFile(path,
		[]byte("token: abcdefghij\napi_base_url: https://engine.example.com\n"), 0o600))

	stdout, _, err := run(t, "config", "view")
	require.NoError(t, err)
	require.NotContains(t, stdout, "abcdefghij")
	require.Contains(t, stdout, "******ghij")
	require.Contains(t, stdout, "https://engine.example.com")
}

func TestConfigViewJSONMasksToken(t *testing.T) {
	path := isolate(t)
	require.NoError(t, os.WriteFile(path,
		[]byte("token: abcdefghij\napi_base_url: https://engine.example.com\n"), 0o600))

	stdout, _, err := run(t, "config", "view", "-o", "json")
	require.NoError(t, err)

	var got map[string]string
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	require.Equal(t, "******ghij", got["token"])
	require.Equal(t, "https://engine.example.com", got["api_base_url"])
}

func TestConfigCommandsNeedNoToken(t *testing.T) {
	isolate(t)

	for _, args := range [][]string{{"config", "path"}, {"config", "view"}} {
		_, _, err := run(t, args...)
		require.NoError(t, err, args)
	}
}

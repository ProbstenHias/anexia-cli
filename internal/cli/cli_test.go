package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ProbstenHias/anexia-cli/internal/cli"
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

	var stdout, stderr bytes.Buffer

	cmd := cli.NewRootCommand(cli.Deps{Stdout: &stdout, Stderr: &stderr})
	cmd.SetArgs(args)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err = cmd.Execute()

	return stdout.String(), stderr.String(), err
}

// locationServer serves a canned location.json response and records the query.
func locationServer(t *testing.T, status int, body string) (srv *httptest.Server, lastQuery *string) {
	t.Helper()

	query := ""
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/vsphere/v1/provisioning/location.json", r.URL.Path)
		query = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv, &query
}

const twoLocations = `{"data":[
  {"code":"ANX04","name":"Vienna","country":"AT","country_name":"Austria","id":"id-1"},
  {"code":"ANX63","name":"Frankfurt","country":"DE","id":"id-2"}
]}`

func TestRootWithNoArgsPrintsHelp(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t)
	require.NoError(t, err)
	require.Contains(t, stdout, "anexia")
	require.Contains(t, stdout, "location")
	require.Contains(t, stdout, "config")
	require.Contains(t, stdout, "version")
}

func TestVersionCommand(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "version")
	require.NoError(t, err)
	require.Contains(t, stdout, "anexia dev")
}

func TestLocationWithoutSubcommandPrintsHelp(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "location")
	require.NoError(t, err)
	require.Contains(t, stdout, "list")
}

func TestLocationListTable(t *testing.T) {
	isolate(t)
	srv, query := locationServer(t, http.StatusOK, twoLocations)

	stdout, stderr, err := run(t, "location", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Equal(t, "page=1&limit=50", *query)
	require.Equal(t,
		"CODE    NAME        COUNTRY   ID\n"+
			"ANX04   Vienna      Austria   id-1\n"+
			"ANX63   Frankfurt   DE        id-2\n",
		stdout)
}

func TestLocationListJSON(t *testing.T) {
	isolate(t)
	srv, _ := locationServer(t, http.StatusOK, twoLocations)

	stdout, _, err := run(t, "location", "list", "-o", "json", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)

	var got []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	require.Len(t, got, 2)
	require.Equal(t, "ANX04", got[0]["code"])
	require.Equal(t, "Austria", got[0]["country_name"])
}

func TestLocationListPassesFilters(t *testing.T) {
	isolate(t)
	srv, query := locationServer(t, http.StatusOK, twoLocations)

	_, _, err := run(t, "location", "list",
		"--token", "tok", "--api-base-url", srv.URL,
		"--page", "3", "--limit", "7",
		"--location-code", "ANX04", "--organization", "org-1")
	require.NoError(t, err)
	require.Equal(t, "page=3&limit=7&location_code=ANX04&organization=org-1", *query)
}

func TestLocationListEmptyTable(t *testing.T) {
	isolate(t)
	srv, _ := locationServer(t, http.StatusOK, `{"data":[]}`)

	stdout, stderr, err := run(t, "location", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "CODE   NAME   COUNTRY   ID\n", stdout)
	require.Equal(t, "no locations found\n", stderr)
}

func TestLocationListEmptyJSON(t *testing.T) {
	isolate(t)
	srv, _ := locationServer(t, http.StatusOK, `{"data":[]}`)

	stdout, _, err := run(t, "location", "list", "-o", "json", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "[]\n", stdout)
}

func TestLocationListServerError(t *testing.T) {
	isolate(t)
	srv, _ := locationServer(t, http.StatusInternalServerError, `{"error":"boom"}`)

	_, _, err := run(t, "location", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.ErrorContains(t, err, "listing locations")
}

func TestLocationListMissingToken(t *testing.T) {
	isolate(t)

	_, stderr, err := run(t, "location", "list")
	require.ErrorContains(t, err, "no API token")
	require.NotContains(t, stderr, "Usage:")
}

func TestLocationListInvalidOutputFormat(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "location", "list", "-o", "yaml", "--token", "tok")
	require.ErrorContains(t, err, `invalid output format "yaml"`)
}

func TestLocationListInvalidPage(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "location", "list", "--page", "0", "--token", "tok")
	require.ErrorContains(t, err, "invalid --page 0")
}

func TestLocationListInvalidLimit(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "location", "list", "--limit", "1001", "--token", "tok")
	require.ErrorContains(t, err, "invalid --limit 1001")
}

func TestLocationListUsesConfigFile(t *testing.T) {
	path := isolate(t)
	srv, _ := locationServer(t, http.StatusOK, twoLocations)

	require.NoError(t, os.WriteFile(path,
		[]byte("token: file-token\napi_base_url: "+srv.URL+"\n"), 0o600))

	stdout, _, err := run(t, "location", "list")
	require.NoError(t, err)
	require.Contains(t, stdout, "ANX04")
}

func TestLocationListRejectsMalformedConfig(t *testing.T) {
	path := isolate(t)
	require.NoError(t, os.WriteFile(path, []byte("nope: 1\n"), 0o600))

	_, _, err := run(t, "location", "list")
	require.ErrorContains(t, err, `unknown config key "nope"`)
	require.ErrorContains(t, err, path)
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

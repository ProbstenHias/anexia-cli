package resource_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/apis/common/gs"
	"go.anx.io/go-anxcloud/pkg/client"

	"github.com/ProbstenHias/anexia-cli/internal/anx"
	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/output"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// widget is a fictional Engine object supporting every verb, so the verb
// builders can be exercised without depending on a real API.
type widget struct {
	Identifier string `json:"identifier,omitempty"`
	Name       string `json:"name,omitempty"`
	StateType  int    `json:"state_type,omitempty"`
}

func (*widget) EndpointURL(context.Context) (*url.URL, error) {
	return url.Parse("/api/test/v1/widget.json")
}

func (w *widget) GetIdentifier(context.Context) (string, error) {
	return w.Identifier, nil
}

func (w *widget) StateOK() bool {
	return w.StateType == gs.StateTypeOK
}

func (w *widget) StatePending() bool {
	return w.StateType == gs.StateTypePending
}

func (w *widget) StateError() bool {
	return w.StateType == gs.StateTypeError
}

// gadget is an object without state, so it can never be waited on.
type gadget struct {
	Identifier string `json:"identifier,omitempty"`
}

func (*gadget) EndpointURL(context.Context) (*url.URL, error) {
	return url.Parse("/api/test/v1/gadget.json")
}

func (g *gadget) GetIdentifier(context.Context) (string, error) {
	return g.Identifier, nil
}

// widgetSpec declares the full-CRUD resource used by most tests.
func widgetSpec() resource.Spec[widget, *widget] {
	return resource.Spec[widget, *widget]{
		Noun:    "widget",
		Aliases: []string{"widgets"},
		Short:   "Work with widgets",
		Columns: []resource.Column[widget]{
			{Name: "identifier", Value: func(w *widget) string { return w.Identifier }},
			{Name: "name", Value: func(w *widget) string { return w.Name }},
		},
		List:      true,
		Get:       true,
		Delete:    true,
		Awaitable: true,
		Identify: func(w *widget, id string) {
			w.Identifier = id
		},
		Filters: func(flags *pflag.FlagSet) func(*widget) {
			name := flags.String("name", "", "filter by name")

			return func(w *widget) {
				w.Name = *name
			}
		},
		Create: func(flags *pflag.FlagSet) func(*widget) {
			name := flags.String("name", "", "widget name")

			return func(w *widget) {
				w.Name = *name
			}
		},
		Update: func(flags *pflag.FlagSet) func(*widget) {
			name := flags.String("name", "", "widget name")

			return func(w *widget) {
				w.Name = *name
			}
		},
	}
}

// env implements resource.Env against a test server.
type env struct {
	baseURL   string
	format    output.Format
	assumeYes bool
	noAPI     bool
}

func (e *env) Writer(out io.Writer) (*output.Writer, error) {
	return output.NewWriter(out, e.format), nil
}

func (e *env) options() anx.Options {
	return anx.Options{Token: "tok", BaseURL: e.baseURL}
}

func (e *env) API(*pflag.FlagSet) (api.API, error) {
	if e.noAPI {
		return nil, errmap.ErrAuth
	}

	return anx.NewAPI(e.options())
}

func (e *env) Client(*pflag.FlagSet) (client.Client, error) {
	return anx.NewClient(e.options())
}

func (*env) Context(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 10*time.Second)
}

func (*env) Fail(err error) error {
	return err
}

func (e *env) AssumeYes() bool {
	return e.assumeYes
}

// request records what the test server received.
type request struct {
	method string
	path   string
	query  string
	body   string
}

// serve starts a test server whose handler is driven by the given responses,
// one per request in order. The last response repeats once exhausted.
func serve(t *testing.T, responses ...string) (*env, *[]request) {
	t.Helper()

	seen := make([]request, 0, len(responses))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		seen = append(seen, request{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			body:   string(body),
		})

		i := min(len(seen), len(responses)) - 1

		// An empty canned response means "no content", which is what the
		// generic client expects from a successful Destroy.
		if responses[i] == "" {
			w.WriteHeader(http.StatusNoContent)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responses[i]))
	}))
	t.Cleanup(srv.Close)

	return &env{baseURL: srv.URL, format: output.FormatTable}, &seen
}

// exec runs cmd with the given arguments and returns its streams.
func exec(cmd *cobra.Command, input string, args ...string) (stdoutText, stderrText string, err error) {
	var stdout, stderr bytes.Buffer

	cmd.SetArgs(args)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(input))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err = cmd.Execute()

	return stdout.String(), stderr.String(), err
}

// paged wraps objects in the envelope the generic client expects.
func paged(t *testing.T, objects ...widget) string {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"page":        1,
			"total_pages": 1,
			"total_items": len(objects),
			"limit":       50,
			"data":        objects,
		},
	})
	require.NoError(t, err)

	return string(body)
}

func TestCommandRegistersEveryVerb(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "{}")
	cmd := resource.Command(e, widgetSpec())

	names := make([]string, 0, len(cmd.Commands()))
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}

	assert.ElementsMatch(t, []string{"list", "get", "create", "update", "delete"}, names)
	assert.Equal(t, []string{"widgets"}, cmd.Aliases)
}

func TestCommandOmitsDisabledVerbs(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "{}")
	cmd := resource.Command(e, resource.Spec[widget, *widget]{Noun: "widget", List: true})

	require.Len(t, cmd.Commands(), 1)
	assert.Equal(t, "list", cmd.Commands()[0].Name())
}

func TestGroupPrintsHelp(t *testing.T) {
	t.Parallel()

	group := resource.Group("core", "Core resources", resource.Group("child", "Child"))

	stdout, _, err := exec(group, "")

	require.NoError(t, err)
	assert.Contains(t, stdout, "child")
}

func TestListRendersTable(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, paged(t, widget{Identifier: "w-1", Name: "first"}))

	stdout, _, err := exec(resource.Command(e, widgetSpec()), "", "list")

	require.NoError(t, err)
	assert.Equal(t, "IDENTIFIER   NAME\nw-1          first\n", stdout)
	require.Len(t, *seen, 1)
	assert.Equal(t, "/api/test/v1/widget.json", (*seen)[0].path)
	assert.Equal(t, "limit=50&page=1", (*seen)[0].query)
}

func TestListPassesFilters(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, paged(t))

	_, stderrText, err := exec(resource.Command(e, widgetSpec()), "", "list", "--name", "first", "--page", "2", "--limit", "5")

	require.NoError(t, err)
	assert.Contains(t, stderrText, "no widgets found")
}

func TestListRejectsBadPaging(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"page zero", []string{"list", "--page", "0"}, "--page 0 must be 1 or greater"},
		{"limit zero", []string{"list", "--limit", "0"}, "--limit 0 must be between 1 and 1000"},
		{"limit too large", []string{"list", "--limit", "1001"}, "--limit 1001 must be between 1 and 1000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e, _ := serve(t, "{}")

			_, _, err := exec(resource.Command(e, widgetSpec()), "", tt.args...)

			require.ErrorIs(t, err, errmap.ErrUsage)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestListReportsAPIFailure(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "{}")
	e.noAPI = true

	_, _, err := exec(resource.Command(e, widgetSpec()), "", "list")

	assert.ErrorIs(t, err, errmap.ErrAuth)
}

func TestGetRendersObject(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, `{"identifier":"w-1","name":"first"}`)

	stdout, _, err := exec(resource.Command(e, widgetSpec()), "", "get", "w-1")

	require.NoError(t, err)
	assert.Equal(t, "IDENTIFIER   NAME\nw-1          first\n", stdout)
	assert.Equal(t, "/api/test/v1/widget.json/w-1", (*seen)[0].path)
}

func TestGetRejectsUnidentifiableResource(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "{}")
	spec := widgetSpec()
	spec.Identify = nil

	_, _, err := exec(resource.Command(e, spec), "", "get", "w-1")

	require.ErrorIs(t, err, errmap.ErrUsage)
	assert.Contains(t, err.Error(), "widget cannot be addressed by identifier")
}

func TestCreatePostsObject(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, `{"identifier":"w-9","name":"new"}`)

	stdout, _, err := exec(resource.Command(e, widgetSpec()), "", "create", "--name", "new")

	require.NoError(t, err)
	assert.Contains(t, stdout, "w-9")
	require.Len(t, *seen, 1)
	assert.Equal(t, http.MethodPost, (*seen)[0].method)
	assert.JSONEq(t, `{"name":"new"}`, (*seen)[0].body)
}

func TestCreateReportsFailure(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "{}")
	e.noAPI = true

	_, _, err := exec(resource.Command(e, widgetSpec()), "", "create", "--name", "new")

	assert.ErrorIs(t, err, errmap.ErrAuth)
}

func TestCreateWaitsForOKState(t *testing.T) {
	t.Parallel()

	e, seen := serve(t,
		`{"identifier":"w-9","name":"new","state_type":2}`,
		`{"identifier":"w-9","name":"new","state_type":1}`,
	)

	stdout, _, err := exec(resource.Command(e, widgetSpec()), "", "create", "--name", "new", "--wait")

	require.NoError(t, err)
	assert.Contains(t, stdout, "w-9")
	require.Len(t, *seen, 2)
	assert.Equal(t, http.MethodPost, (*seen)[0].method)
	assert.Equal(t, http.MethodGet, (*seen)[1].method)
}

func TestCreateReportsWaitFailure(t *testing.T) {
	t.Parallel()

	e, _ := serve(t,
		`{"identifier":"w-9","state_type":2}`,
		`{"identifier":"w-9","state_type":0}`,
	)

	_, _, err := exec(resource.Command(e, widgetSpec()), "", "create", "--name", "new", "--wait")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "waiting for widget")
	assert.ErrorIs(t, err, gs.ErrStateError)
}

func TestWaitFlagAbsentWhenNotAwaitable(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "{}")
	spec := widgetSpec()
	spec.Awaitable = false

	cmd := resource.Command(e, spec)

	create, _, err := cmd.Find([]string{"create"})
	require.NoError(t, err)
	assert.Nil(t, create.Flags().Lookup("wait"))
}

func TestWaitRejectsStatelessResource(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, `{"identifier":"g-1"}`)

	spec := resource.Spec[gadget, *gadget]{
		Noun:      "gadget",
		Awaitable: true,
		Columns:   []resource.Column[gadget]{{Name: "identifier", Value: func(g *gadget) string { return g.Identifier }}},
		Create: func(*pflag.FlagSet) func(*gadget) {
			return func(*gadget) {}
		},
	}

	_, _, err := exec(resource.Command(e, spec), "", "create", "--wait")

	require.Error(t, err)
	assert.ErrorIs(t, err, api.ErrOperationNotSupported)
}

func TestUpdateReadsThenWrites(t *testing.T) {
	t.Parallel()

	e, seen := serve(t,
		`{"identifier":"w-1","name":"old"}`,
		`{"identifier":"w-1","name":"renamed"}`,
	)

	stdout, _, err := exec(resource.Command(e, widgetSpec()), "", "update", "w-1", "--name", "renamed")

	require.NoError(t, err)
	assert.Contains(t, stdout, "renamed")
	require.Len(t, *seen, 2)
	assert.Equal(t, http.MethodGet, (*seen)[0].method)
	assert.Equal(t, http.MethodPut, (*seen)[1].method)
	assert.Equal(t, "/api/test/v1/widget.json/w-1", (*seen)[1].path)
	assert.JSONEq(t, `{"identifier":"w-1","name":"renamed"}`, (*seen)[1].body)
}

func TestUpdateRejectsUnidentifiableResource(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "{}")
	spec := widgetSpec()
	spec.Identify = nil

	_, _, err := exec(resource.Command(e, spec), "", "update", "w-1", "--name", "x")

	assert.ErrorIs(t, err, errmap.ErrUsage)
}

func TestUpdateReportsFailure(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "{}")
	e.noAPI = true

	_, _, err := exec(resource.Command(e, widgetSpec()), "", "update", "w-1", "--name", "x")

	assert.ErrorIs(t, err, errmap.ErrAuth)
}

func TestDeleteAsksForConfirmation(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, "")

	_, stderrText, err := exec(resource.Command(e, widgetSpec()), "y\n", "delete", "w-1")

	require.NoError(t, err)
	assert.Contains(t, stderrText, `delete widget "w-1" [y/N]: `)
	assert.Contains(t, stderrText, "deleted widget w-1")
	require.Len(t, *seen, 1)
	assert.Equal(t, http.MethodDelete, (*seen)[0].method)
	assert.Equal(t, "/api/test/v1/widget.json/w-1", (*seen)[0].path)
}

func TestDeleteHonorsDeclinedConfirmation(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, "")

	_, _, err := exec(resource.Command(e, widgetSpec()), "n\n", "delete", "w-1")

	require.ErrorIs(t, err, errmap.ErrCanceled)
	assert.Empty(t, *seen)
}

func TestDeleteSkipsPromptWithAssumeYes(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, "")
	e.assumeYes = true

	_, stderrText, err := exec(resource.Command(e, widgetSpec()), "", "delete", "w-1")

	require.NoError(t, err)
	assert.NotContains(t, stderrText, "[y/N]")
	assert.Len(t, *seen, 1)
}

func TestDeleteAcceptsDestroyAlias(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, "")
	e.assumeYes = true

	_, _, err := exec(resource.Command(e, widgetSpec()), "", "destroy", "w-1")

	require.NoError(t, err)
	assert.Len(t, *seen, 1)
}

func TestDeleteRejectsUnidentifiableResource(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "")
	spec := widgetSpec()
	spec.Identify = nil

	_, _, err := exec(resource.Command(e, spec), "", "delete", "w-1")

	assert.ErrorIs(t, err, errmap.ErrUsage)
}

func TestDeleteReportsFailure(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "")
	e.noAPI = true
	e.assumeYes = true

	_, _, err := exec(resource.Command(e, widgetSpec()), "", "delete", "w-1")

	assert.ErrorIs(t, err, errmap.ErrAuth)
}

func TestPluralDefaultsToNounWithS(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, paged(t))
	spec := widgetSpec()
	spec.Plural = "widget entries"

	_, stderrText, err := exec(resource.Command(e, spec), "", "list")

	require.NoError(t, err)
	assert.Contains(t, stderrText, "no widget entries found")
}

func TestListStructuredOutput(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, paged(t, widget{Identifier: "w-1", Name: "first"}))
	e.format = output.FormatJSON

	stdout, _, err := exec(resource.Command(e, widgetSpec()), "", "list")

	require.NoError(t, err)
	assert.JSONEq(t, `[{"identifier":"w-1","name":"first"}]`, stdout)
}

func TestListStructuredOutputEmpty(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, paged(t))
	e.format = output.FormatJSON

	stdout, _, err := exec(resource.Command(e, widgetSpec()), "", "list")

	require.NoError(t, err)
	assert.JSONEq(t, `[]`, stdout)
}

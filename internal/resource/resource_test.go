package resource_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.anx.io/go-anxcloud/pkg/api"
	corev1 "go.anx.io/go-anxcloud/pkg/apis/core/v1"
	"go.anx.io/go-anxcloud/pkg/client"

	"github.com/ProbstenHias/anexia-cli/internal/anx"
	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/output"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// locationEndpoint is where corev1.Location lives, so tests can assert that
// the registry drives the real object's own EndpointURL.
const locationEndpoint = "/api/core/v1/location.json"

// locationSpec mirrors the shipped "core location" spec. Tests run against a
// real Engine object rather than a fictional one, so a change in go-anxcloud's
// hooks or endpoints fails here instead of passing against a stand-in.
func locationSpec() resource.Spec[corev1.Location, *corev1.Location] {
	return resource.Spec[corev1.Location, *corev1.Location]{
		Noun:  "location",
		Short: "Work with locations",
		List:  true,
		Get:   true,
		Identify: func(l *corev1.Location, id string) {
			l.Identifier = id
		},
		Filters: func(flags *pflag.FlagSet) func(*corev1.Location) {
			code := flags.String("code", "", "filter by code")

			return func(l *corev1.Location) {
				l.Code = *code
			}
		},
		Columns: []resource.Column[corev1.Location]{
			{Name: "identifier", Value: func(l *corev1.Location) string { return l.Identifier }},
			{Name: "code", Value: func(l *corev1.Location) string { return l.Code }},
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

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responses[i]))
	}))
	t.Cleanup(srv.Close)

	return &env{baseURL: srv.URL, format: output.FormatTable}, &seen
}

// exec runs cmd with the given arguments and returns its streams.
func exec(cmd *cobra.Command, args ...string) (stdoutText, stderrText string, err error) {
	var stdout, stderr bytes.Buffer

	cmd.SetArgs(args)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err = cmd.Execute()

	return stdout.String(), stderr.String(), err
}

// paged wraps objects in the envelope the generic client expects. Callers pass
// the page number and total so pagination behavior can be driven precisely.
func paged(t *testing.T, page, totalPages, limit int, objects ...corev1.Location) string {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"page":        page,
			"total_pages": totalPages,
			"total_items": len(objects),
			"limit":       limit,
			"data":        objects,
		},
	})
	require.NoError(t, err)

	return string(body)
}

// onePage is the common case: a single complete page of results.
func onePage(t *testing.T, objects ...corev1.Location) string {
	t.Helper()

	return paged(t, 1, 1, 50, objects...)
}

func TestCommandRegistersReadVerbs(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "{}")
	cmd := resource.Command(e, locationSpec())

	names := make([]string, 0, len(cmd.Commands()))
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}

	assert.ElementsMatch(t, []string{"list", "get"}, names)
	assert.Equal(t, []string{"locations"}, cmd.Aliases)
}

func TestCommandOmitsDisabledVerbs(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "{}")
	cmd := resource.Command(e, resource.Spec[corev1.Location, *corev1.Location]{Noun: "location", List: true})

	require.Len(t, cmd.Commands(), 1)
	assert.Equal(t, "list", cmd.Commands()[0].Name())
}

func TestGroupPrintsHelp(t *testing.T) {
	t.Parallel()

	group := resource.Group("core", "Core resources", resource.Group("child", "Child"))

	stdout, _, err := exec(group)

	require.NoError(t, err)
	assert.Contains(t, stdout, "child")
}

func TestNounCarriesPluralAlias(t *testing.T) {
	t.Parallel()

	cmd := resource.Noun("service", "services", "Work with services")

	assert.Equal(t, []string{"services"}, cmd.Aliases)
}

func TestListRendersTable(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, onePage(t, corev1.Location{Identifier: "id-1", Code: "ANX04"}))

	stdout, _, err := exec(resource.Command(e, locationSpec()), "list")

	require.NoError(t, err)
	assert.Equal(t, "IDENTIFIER   CODE\nid-1         ANX04\n", stdout)
	require.Len(t, *seen, 1)
	assert.Equal(t, locationEndpoint, (*seen)[0].path)
	assert.Equal(t, "limit=50&page=1", (*seen)[0].query)
}

func TestListPassesFilters(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, onePage(t))

	_, stderrText, err := exec(resource.Command(e, locationSpec()), "list", "--code", "ANX04", "--page", "2", "--limit", "5")

	require.NoError(t, err)
	assert.Contains(t, stderrText, "no locations found")
	require.Len(t, *seen, 1)
	assert.Equal(t, "limit=5&page=2", (*seen)[0].query)
}

func TestListRequestsTheAskedForPage(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, paged(t, 3, 9, 50, corev1.Location{Identifier: "id-3"}))

	stdout, _, err := exec(resource.Command(e, locationSpec()), "list", "--page", "3")

	require.NoError(t, err)
	assert.Contains(t, stdout, "id-3")
	require.Len(t, *seen, 1)
	assert.Equal(t, "limit=50&page=3", (*seen)[0].query)
}

// TestListAllWalksEveryPage pins the paging contract: --all must request each
// page exactly once, in order, starting at --page.
func TestListAllWalksEveryPage(t *testing.T) {
	t.Parallel()

	const totalPages = 4

	tests := []struct {
		name      string
		startPage int
		wantPages []int
		wantIDs   []string
	}{
		{
			name:      "from the first page",
			startPage: 1,
			wantPages: []int{1, 2, 3, 4},
			wantIDs:   []string{"id-p1", "id-p2", "id-p3", "id-p4"},
		},
		{
			name:      "from a later page",
			startPage: 2,
			wantPages: []int{2, 3, 4},
			wantIDs:   []string{"id-p2", "id-p3", "id-p4"},
		},
		{
			name:      "from the last page",
			startPage: totalPages,
			wantPages: []int{4},
			wantIDs:   []string{"id-p4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var seen []int

			// One object per page, with --limit 1 so every page is full and
			// the walk only stops at the reported total.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				page, err := strconv.Atoi(r.URL.Query().Get("page"))
				require.NoError(t, err)

				seen = append(seen, page)

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(paged(t, page, totalPages, 1,
					corev1.Location{Identifier: fmt.Sprintf("id-p%d", page)})))
			}))
			t.Cleanup(srv.Close)

			e := &env{baseURL: srv.URL, format: output.FormatJSON}

			stdout, _, err := exec(resource.Command(e, locationSpec()),
				"list", "--all", "--limit", "1", "--page", fmt.Sprint(tt.startPage))

			require.NoError(t, err)
			assert.Equal(t, tt.wantPages, seen)

			var got []struct {
				Identifier string `json:"identifier"`
			}
			require.NoError(t, json.Unmarshal([]byte(stdout), &got))

			ids := make([]string, 0, len(got))
			for _, g := range got {
				ids = append(ids, g.Identifier)
			}

			assert.Equal(t, tt.wantIDs, ids)
		})
	}
}

// TestListAllStopsOnShortPage covers an Engine that reports no total: the walk
// has to end when a page comes back smaller than the requested limit.
func TestListAllStopsOnShortPage(t *testing.T) {
	t.Parallel()

	e, seen := serve(t,
		paged(t, 1, 0, 2, corev1.Location{Identifier: "id-1"}, corev1.Location{Identifier: "id-2"}),
		paged(t, 2, 0, 2, corev1.Location{Identifier: "id-3"}),
	)

	stdout, _, err := exec(resource.Command(e, locationSpec()), "list", "--all", "--limit", "2")

	require.NoError(t, err)
	assert.Len(t, *seen, 2)
	assert.Contains(t, stdout, "id-3")
}

// TestListAllStopsWhenPagingIsIgnored guards against the endpoint that returns
// the same full page whatever page is asked for. Without a stop condition the
// walk would run until --timeout.
func TestListAllStopsWhenPagingIsIgnored(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, paged(t, 1, 1, 1, corev1.Location{Identifier: "id-1"}))

	_, _, err := exec(resource.Command(e, locationSpec()), "list", "--all", "--limit", "1")

	require.NoError(t, err)
	assert.Len(t, *seen, 1)
}

func TestListWithoutAllFetchesOnePage(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, paged(t, 1, 5, 1, corev1.Location{Identifier: "id-1"}))

	_, _, err := exec(resource.Command(e, locationSpec()), "list", "--limit", "1")

	require.NoError(t, err)
	assert.Len(t, *seen, 1)
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

			_, _, err := exec(resource.Command(e, locationSpec()), tt.args...)

			require.ErrorIs(t, err, errmap.ErrUsage)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestValidatePagingAcceptsTheDefaults(t *testing.T) {
	t.Parallel()

	require.NoError(t, resource.ValidatePaging(1, 50))
	require.NoError(t, resource.ValidatePaging(1, resource.MaxLimit))
}

func TestListReportsAPIFailure(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "{}")
	e.noAPI = true

	_, _, err := exec(resource.Command(e, locationSpec()), "list")

	assert.ErrorIs(t, err, errmap.ErrAuth)
}

func TestListReportsEngineFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	e := &env{baseURL: srv.URL, format: output.FormatTable}

	_, _, err := exec(resource.Command(e, locationSpec()), "list")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing locations")
}

func TestGetRendersObject(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, `{"identifier":"id-1","code":"ANX04"}`)

	stdout, _, err := exec(resource.Command(e, locationSpec()), "get", "id-1")

	require.NoError(t, err)
	assert.Equal(t, "IDENTIFIER   CODE\nid-1         ANX04\n", stdout)
	assert.Equal(t, locationEndpoint+"/id-1", (*seen)[0].path)
}

func TestGetRejectsUnidentifiableResource(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "{}")
	spec := locationSpec()
	spec.Identify = nil

	_, _, err := exec(resource.Command(e, spec), "get", "id-1")

	require.ErrorIs(t, err, errmap.ErrUsage)
	assert.Contains(t, err.Error(), "location cannot be addressed by identifier")
}

func TestGetReportsFailure(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "{}")
	e.noAPI = true

	_, _, err := exec(resource.Command(e, locationSpec()), "get", "id-1")

	assert.ErrorIs(t, err, errmap.ErrAuth)
}

func TestGetStructuredOutput(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, `{"identifier":"id-1","code":"ANX04"}`)
	e.format = output.FormatJSON

	stdout, _, err := exec(resource.Command(e, locationSpec()), "get", "id-1")

	require.NoError(t, err)
	assert.Contains(t, stdout, `"code": "ANX04"`)
}

func TestPluralOverridesTheDefault(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, onePage(t))
	spec := locationSpec()
	spec.Plural = "location entries"

	cmd := resource.Command(e, spec)

	_, stderrText, err := exec(cmd, "list")

	require.NoError(t, err)
	assert.Contains(t, stderrText, "no location entries found")
	assert.Equal(t, []string{"location entries"}, cmd.Aliases)
}

func TestListStructuredOutput(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, onePage(t, corev1.Location{Identifier: "id-1", Code: "ANX04"}))
	e.format = output.FormatJSON

	stdout, _, err := exec(resource.Command(e, locationSpec()), "list")

	require.NoError(t, err)
	assert.Contains(t, stdout, `"identifier": "id-1"`)
}

func TestListStructuredOutputEmpty(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, onePage(t))
	e.format = output.FormatJSON

	stdout, _, err := exec(resource.Command(e, locationSpec()), "list")

	require.NoError(t, err)
	assert.JSONEq(t, `[]`, stdout)
}

func TestRenderListIsSharedWithLegacyCommands(t *testing.T) {
	t.Parallel()

	type row struct {
		Name string `json:"name"`
	}

	tests := []struct {
		name       string
		format     output.Format
		items      []row
		wantStdout string
		wantStderr string
	}{
		{
			name:       "table with rows",
			format:     output.FormatTable,
			items:      []row{{Name: "first"}},
			wantStdout: "NAME\nfirst\n",
		},
		{
			name:       "table without rows notes on stderr",
			format:     output.FormatTable,
			items:      nil,
			wantStdout: "NAME\n",
			wantStderr: "no things found\n",
		},
		{
			name:       "json without rows is an empty array",
			format:     output.FormatJSON,
			items:      nil,
			wantStdout: "[]\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer

			cmd := &cobra.Command{}
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			w := output.NewWriter(&stdout, tt.format)

			err := resource.RenderList(cmd, w, "things", tt.items, []string{"name"},
				func(r *row) []string { return []string{r.Name} })

			require.NoError(t, err)
			assert.Equal(t, tt.wantStdout, stdout.String())
			assert.Equal(t, tt.wantStderr, stderr.String())
		})
	}
}

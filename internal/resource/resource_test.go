package resource_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/api/types"
	corev1 "go.anx.io/go-anxcloud/pkg/apis/core/v1"

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

// unpagedEndpoint is where the fictional non-paginating object below lives.
const unpagedEndpoint = "/api/test/v1/unpaged.json"

// unpaged stands in for a real Engine object whose endpoint returns everything
// in one response and ignores paging entirely. go-anxcloud signals that with
// PaginationSupportHook, and clouddns zones and records and vsphere templates
// all do it. The library then stops sending the page parameter at all, so every
// request is identical.
type unpaged struct {
	Identifier string `json:"identifier" anxcloud:"identifier"`
}

func (u *unpaged) EndpointURL(context.Context) (*url.URL, error) {
	return url.Parse(unpagedEndpoint)
}

func (u *unpaged) GetIdentifier(context.Context) (string, error) {
	return u.Identifier, nil
}

// HasPagination reads the operation off the context, the way every other
// go-anxcloud object hook does. The library sets it before calling this, so a
// caller asking the question itself has to set it too.
func (u *unpaged) HasPagination(ctx context.Context) (bool, error) {
	if _, err := types.OperationFromContext(ctx); err != nil {
		return false, err
	}

	return false, nil
}

// unpagedSpec registers the non-paginating object the same way a real one would
// be registered.
func unpagedSpec() resource.Spec[unpaged, *unpaged] {
	return resource.Spec[unpaged, *unpaged]{
		Noun:  "unpaged",
		Short: "Work with unpaged things",
		List:  true,
		Get:   true,
		Identify: func(u *unpaged, id string) {
			u.Identifier = id
		},
		Columns: []resource.Column[unpaged]{
			{Name: "identifier", Value: func(u *unpaged) string { return u.Identifier }},
		},
	}
}

// env implements resource.Env against a test server.
type env struct {
	baseURL string
	format  output.Format
	noAPI   bool
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

func (*env) Context(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 10*time.Second)
}

func (*env) Fail(err error) error {
	return err
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
//
// A repeating last response can mask a walk that never terminates, so an --all
// test built on this must either end its responses with an empty page or assert
// the exact number of requests.
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

// terse wraps objects in an envelope carrying no paging metadata at all. Every
// field of it is optional in the Engine's responses, and the client reports a
// missing field and a literal zero identically, so this shape is the one that
// catches a walk relying on either.
func terse(t *testing.T, objects ...corev1.Location) string {
	t.Helper()

	body, err := json.Marshal(map[string]any{"data": objects})
	require.NoError(t, err)

	return string(body)
}

// bare renders objects as a plain JSON array, the third shape the client
// decodes.
func bare(t *testing.T, objects ...corev1.Location) string {
	t.Helper()

	body, err := json.Marshal(objects)
	require.NoError(t, err)

	return string(body)
}

// identifiers reads the identifiers out of JSON command output, in order, so a
// walk can be asserted against exactly rather than by substring.
func identifiers(t *testing.T, stdout string) []string {
	t.Helper()

	var got []struct {
		Identifier string `json:"identifier"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))

	ids := make([]string, 0, len(got))
	for _, g := range got {
		ids = append(ids, g.Identifier)
	}

	return ids
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

// resourceSpec is a Spec over an object whose filter reaches the wire.
// corev1.Resource turns its first tag into a tag_name query parameter, which
// corev1.Location has no equivalent of on a list.
func resourceSpec() resource.Spec[corev1.Resource, *corev1.Resource] {
	return resource.Spec[corev1.Resource, *corev1.Resource]{
		Noun:  "resource",
		Short: "Work with resources",
		List:  true,
		Get:   true,
		Identify: func(r *corev1.Resource, id string) {
			r.Identifier = id
		},
		Filters: func(flags *pflag.FlagSet) func(*corev1.Resource) {
			tag := flags.String("tag", "", "filter by tag")

			return func(r *corev1.Resource) {
				if *tag != "" {
					r.Tags = []string{*tag}
				}
			}
		},
		Columns: []resource.Column[corev1.Resource]{
			{Name: "identifier", Value: func(r *corev1.Resource) string { return r.Identifier }},
		},
	}
}

// TestListPassesPagingFlags pins the paging flags reaching the wire. The
// filter value is checked separately, because corev1.Location does not send
// one on a list and so cannot show a filter arriving.
func TestListPassesPagingFlags(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, onePage(t))

	_, stderrText, err := exec(resource.Command(e, locationSpec()), "list", "--page", "2", "--limit", "5")

	require.NoError(t, err)
	assert.Contains(t, stderrText, "no locations found")
	require.Len(t, *seen, 1)
	assert.Equal(t, "limit=5&page=2", (*seen)[0].query)
}

// TestListPassesFiltersToTheEngine covers the filter mechanism itself, using an
// object that actually turns a field into a query parameter. corev1.Resource
// sends its first tag as tag_name, so a filter that never got applied, or got
// applied to the wrong request, is visible on the wire.
func TestListPassesFiltersToTheEngine(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, `{"data":{"page":1,"total_pages":1,"total_items":0,"limit":50,"data":[]}}`)

	_, _, err := exec(resource.Command(e, resourceSpec()), "list", "--tag", "production")

	require.NoError(t, err)
	require.Len(t, *seen, 1)

	values, err := url.ParseQuery((*seen)[0].query)
	require.NoError(t, err)
	assert.Equal(t, "production", values.Get("tag_name"))
}

// TestListAppliesFiltersToEveryPage guards the filter surviving a walk: the
// filter object is reused across pages, so a page fetched without it would
// return unfiltered results while looking fine.
func TestListAppliesFiltersToEveryPage(t *testing.T) {
	t.Parallel()

	var tags []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tags = append(tags, r.URL.Query().Get("tag_name"))

		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		require.NoError(t, err)

		body := `{"data":{"page":1,"limit":1,"data":[{"identifier":"r-1"}]}}`
		if page > 2 {
			body = `{"data":{"page":1,"limit":1,"data":[]}}`
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	e := &env{baseURL: srv.URL, format: output.FormatTable}

	_, _, err := exec(resource.Command(e, resourceSpec()), "list", "--all", "--limit", "1", "--tag", "production")

	require.NoError(t, err)
	assert.Equal(t, []string{"production", "production", "production"}, tags)
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

	const lastPage = 4

	tests := []struct {
		name      string
		startPage int
		wantPages []int
		wantIDs   []string
	}{
		{
			name:      "from the first page",
			startPage: 1,
			wantPages: []int{1, 2, 3, 4, 5},
			wantIDs:   []string{"id-p1", "id-p2", "id-p3", "id-p4"},
		},
		{
			name:      "from a later page",
			startPage: 2,
			wantPages: []int{2, 3, 4, 5},
			wantIDs:   []string{"id-p2", "id-p3", "id-p4"},
		},
		{
			name:      "from the last page",
			startPage: lastPage,
			wantPages: []int{4, 5},
			wantIDs:   []string{"id-p4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var seen []int

			// One object per page with --limit 1, so every page is full until
			// the results run out and the walk sees an empty page.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				page, err := strconv.Atoi(r.URL.Query().Get("page"))
				require.NoError(t, err)

				seen = append(seen, page)

				var objects []corev1.Location

				if page <= lastPage {
					objects = []corev1.Location{{Identifier: fmt.Sprintf("id-p%d", page)}}
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(paged(t, page, lastPage, 1, objects...)))
			}))
			t.Cleanup(srv.Close)

			e := &env{baseURL: srv.URL, format: output.FormatJSON}

			stdout, _, err := exec(resource.Command(e, locationSpec()),
				"list", "--all", "--limit", "1", "--page", fmt.Sprint(tt.startPage))

			require.NoError(t, err)
			assert.Equal(t, tt.wantPages, seen)
			assert.Equal(t, tt.wantIDs, identifiers(t, stdout))
		})
	}
}

// TestListAllStopsOnAnEmptyPage covers an Engine that reports no total. An
// empty page is the only signal that says "no more results" without relying on
// an optional field, so it has to be the one the walk trusts.
func TestListAllStopsOnAnEmptyPage(t *testing.T) {
	t.Parallel()

	e, seen := serve(t,
		paged(t, 1, 0, 2, corev1.Location{Identifier: "id-1"}, corev1.Location{Identifier: "id-2"}),
		paged(t, 2, 0, 2, corev1.Location{Identifier: "id-3"}),
		paged(t, 3, 0, 2),
	)
	e.format = output.FormatJSON

	stdout, _, err := exec(resource.Command(e, locationSpec()), "list", "--all", "--limit", "2")

	require.NoError(t, err)
	assert.Len(t, *seen, 3)

	// Exact rather than Contains, so a page appended twice cannot pass.
	assert.Equal(t, []string{"id-1", "id-2", "id-3"}, identifiers(t, stdout))
}

// TestListAllWalksEveryResponseShape drives --all over every envelope the
// client can decode, including the ones that carry no page number and no
// applied page size. Both of those arrive as a zero indistinguishable from a
// literal one, so a walk that reads either field truncates here.
func TestListAllWalksEveryResponseShape(t *testing.T) {
	t.Parallel()

	const lastPage = 4

	tests := []struct {
		name string
		body func(t *testing.T, page int, objects ...corev1.Location) string
	}{
		{
			name: "full metadata",
			body: func(t *testing.T, page int, objects ...corev1.Location) string {
				t.Helper()

				return paged(t, page, 0, 1, objects...)
			},
		},
		{
			name: "no page or limit reported",
			body: func(t *testing.T, _ int, objects ...corev1.Location) string {
				t.Helper()

				return terse(t, objects...)
			},
		},
		{
			name: "bare array",
			body: func(t *testing.T, _ int, objects ...corev1.Location) string {
				t.Helper()

				return bare(t, objects...)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, start := range []int{1, 2} {
				var seen []int

				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					page, err := strconv.Atoi(r.URL.Query().Get("page"))
					require.NoError(t, err)

					seen = append(seen, page)

					objects := []corev1.Location{{Identifier: fmt.Sprintf("id-p%d", page)}}
					if page > lastPage {
						objects = nil
					}

					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(tt.body(t, page, objects...)))
				}))

				e := &env{baseURL: srv.URL, format: output.FormatJSON}

				stdout, _, err := exec(resource.Command(e, locationSpec()),
					"list", "--all", "--limit", "1", "--page", fmt.Sprint(start))
				srv.Close()

				require.NoError(t, err)

				wantPages := make([]int, 0, lastPage)
				wantIDs := make([]string, 0, lastPage)

				for p := start; p <= lastPage; p++ {
					wantPages = append(wantPages, p)
					wantIDs = append(wantIDs, fmt.Sprintf("id-p%d", p))
				}

				// One extra request to see the empty page that ends the walk.
				wantPages = append(wantPages, lastPage+1)

				assert.Equal(t, wantPages, seen, "requested pages starting at %d", start)
				assert.Equal(t, wantIDs, identifiers(t, stdout), "returned objects starting at %d", start)
			}
		})
	}
}

// TestListAllOutlivesAWrongTotalPages covers an Engine whose reported
// total_pages understates what it actually serves, which happens when the count
// is computed from the requested limit while the Engine caps the page size
// lower. Trusting the count ends the walk on a full page and drops the rest of
// the results without saying so.
func TestListAllOutlivesAWrongTotalPages(t *testing.T) {
	t.Parallel()

	const lastPage = 5

	var seen []int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		require.NoError(t, err)

		seen = append(seen, page)

		var objects []corev1.Location

		if page <= lastPage {
			objects = []corev1.Location{{Identifier: fmt.Sprintf("id-p%d", page)}}
		}

		// The Engine claims two pages but keeps serving results past them.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(paged(t, page, 2, 1, objects...)))
	}))
	t.Cleanup(srv.Close)

	e := &env{baseURL: srv.URL, format: output.FormatJSON}

	stdout, _, err := exec(resource.Command(e, locationSpec()), "list", "--all", "--limit", "1")

	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6}, seen)
	assert.Equal(t, []string{"id-p1", "id-p2", "id-p3", "id-p4", "id-p5"}, identifiers(t, stdout))
}

// TestListAllKeepsResultsWhenTheEngineRejectsThePageAfterTheLast covers an
// Engine that answers a page past the end with 404 rather than an empty list.
// The walk always asks one page beyond the results, so that answer arrives on
// every complete walk and must not throw away everything already collected.
func TestListAllKeepsResultsWhenTheEngineRejectsThePageAfterTheLast(t *testing.T) {
	t.Parallel()

	const lastPage = 3

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		require.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")

		if page > lastPage {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":404,"message":"page not found"}}`))

			return
		}

		_, _ = w.Write([]byte(paged(t, page, 0, 1,
			corev1.Location{Identifier: fmt.Sprintf("id-p%d", page)})))
	}))
	t.Cleanup(srv.Close)

	e := &env{baseURL: srv.URL, format: output.FormatJSON}

	stdout, _, err := exec(resource.Command(e, locationSpec()), "list", "--all", "--limit", "1")

	require.NoError(t, err)
	assert.Equal(t, []string{"id-p1", "id-p2", "id-p3"}, identifiers(t, stdout))
}

// TestListReportsANotFoundOnTheFirstPage separates the case above from a
// genuine miss: if the very first page is rejected, nothing was collected and
// the user needs to hear about it.
func TestListReportsANotFoundOnTheFirstPage(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"nope"}}`))
	}))
	t.Cleanup(srv.Close)

	e := &env{baseURL: srv.URL, format: output.FormatTable}

	_, _, err := exec(resource.Command(e, locationSpec()), "list", "--all")

	require.Error(t, err)
	assert.Equal(t, errmap.ExitNotFound, errmap.ExitCode(err))
}

// TestListAllOnANonPaginatingResourceFetchesOnce covers a resource whose
// endpoint returns everything at once. Asking for every page is already
// satisfied by the first response, so the walk must not keep asking, and it must
// not fail.
func TestListAllOnANonPaginatingResourceFetchesOnce(t *testing.T) {
	t.Parallel()

	requests := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"identifier":"u-1"},{"identifier":"u-2"}]}`))
	}))
	t.Cleanup(srv.Close)

	e := &env{baseURL: srv.URL, format: output.FormatJSON}

	stdout, _, err := exec(resource.Command(e, unpagedSpec()), "list", "--all", "--limit", "2")

	require.NoError(t, err)
	assert.Equal(t, 1, requests, "the endpoint returns everything in one response")
	assert.Equal(t, []string{"u-1", "u-2"}, identifiers(t, stdout))
}

// TestListPageBeyondFirstOnANonPaginatingResourceIsRejected covers the user who
// asks for page two of a resource that has only ever one page. The Engine
// ignores the parameter, so honoring the request silently returns page one and
// the user cannot tell. Saying so is the only honest answer.
func TestListPageBeyondFirstOnANonPaginatingResourceIsRejected(t *testing.T) {
	t.Parallel()

	requests := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"identifier":"u-1"}]}`))
	}))
	t.Cleanup(srv.Close)

	e := &env{baseURL: srv.URL, format: output.FormatTable}

	_, _, err := exec(resource.Command(e, unpagedSpec()), "list", "--page", "2")

	require.Error(t, err)
	assert.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	assert.Contains(t, err.Error(), "does not support paging")
	assert.Zero(t, requests, "nothing to ask for once the request is known to be unanswerable")
}

// TestListAllWalksPagesCappedBelowTheLimit covers an Engine that caps its page
// size below --limit. Comparing the page length against the requested limit
// would read every page as short and silently drop the rest of the results.
func TestListAllWalksPagesCappedBelowTheLimit(t *testing.T) {
	t.Parallel()

	// Two objects per page regardless of the requested limit of 10, three
	// pages in total, with no total_pages to fall back on.
	var seen []int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		require.NoError(t, err)

		seen = append(seen, page)

		var objects []corev1.Location

		if page <= 3 {
			objects = []corev1.Location{
				{Identifier: fmt.Sprintf("id-p%d-a", page)},
				{Identifier: fmt.Sprintf("id-p%d-b", page)},
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(paged(t, page, 0, 2, objects...)))
	}))
	t.Cleanup(srv.Close)

	e := &env{baseURL: srv.URL, format: output.FormatJSON}

	stdout, _, err := exec(resource.Command(e, locationSpec()), "list", "--all", "--limit", "10")

	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3, 4}, seen)
	assert.Equal(t, []string{
		"id-p1-a", "id-p1-b",
		"id-p2-a", "id-p2-b",
		"id-p3-a", "id-p3-b",
	}, identifiers(t, stdout))
}

// TestListAllGivesUpOnAnEndlessEngine is the backstop: an Engine that keeps
// answering with full pages that track the requested page number, forever.
func TestListAllGivesUpOnAnEndlessEngine(t *testing.T) {
	t.Parallel()

	requests := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		require.NoError(t, err)

		requests++

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(paged(t, page, 0, 1,
			corev1.Location{Identifier: fmt.Sprintf("id-p%d", page)})))
	}))
	t.Cleanup(srv.Close)

	e := &env{baseURL: srv.URL, format: output.FormatTable}

	_, _, err := exec(resource.Command(e, locationSpec()), "list", "--all", "--limit", "1")

	require.Error(t, err)
	// The advice has to name the ceiling, because a user already at --limit
	// 1000 cannot act on "raise --limit".
	assert.Contains(t, err.Error(), "gave up after 1000 pages")
	assert.Contains(t, err.Error(), "narrow the results with a filter")
	assert.Contains(t, err.Error(), "up to 1000")
	// maxPages pages of results, plus the one request that would have seen
	// the end if there had been one.
	assert.Equal(t, 1001, requests)
}

// TestListAllReturnsAResultThatFillsExactlyTheBackstop covers the boundary: a
// result set that happens to be exactly as long as the walk is allowed to go is
// a legitimate answer, not a runaway, and returning nothing would discard every
// page already collected.
func TestListAllReturnsAResultThatFillsExactlyTheBackstop(t *testing.T) {
	t.Parallel()

	const lastPage = 1000

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		require.NoError(t, err)

		objects := []corev1.Location{{Identifier: fmt.Sprintf("id-p%d", page)}}
		if page > lastPage {
			objects = nil
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(paged(t, page, 0, 1, objects...)))
	}))
	t.Cleanup(srv.Close)

	e := &env{baseURL: srv.URL, format: output.FormatJSON}

	stdout, _, err := exec(resource.Command(e, locationSpec()), "list", "--all", "--limit", "1")

	require.NoError(t, err)
	assert.Len(t, identifiers(t, stdout), lastPage)
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

// TestFetchPagesWalksUntilAnEmptyPage covers the paging helper the legacy
// commands share. Those clients discard all page metadata, so an empty page is
// the only end-of-results signal. Page three is short but not empty, which the
// walk must not read as the end.
func TestFetchPagesWalksUntilAnEmptyPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		startPage int
		all       bool
		wantPages []int
		wantItems []string
	}{
		{
			name:      "one page without all",
			startPage: 2,
			all:       false,
			wantPages: []int{2},
			wantItems: []string{"p2-a", "p2-b"},
		},
		{
			name:      "every page from the first",
			startPage: 1,
			all:       true,
			wantPages: []int{1, 2, 3, 4},
			wantItems: []string{"p1-a", "p1-b", "p2-a", "p2-b", "p3-a"},
		},
		{
			name:      "every page from a later one",
			startPage: 2,
			all:       true,
			wantPages: []int{2, 3, 4},
			wantItems: []string{"p2-a", "p2-b", "p3-a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var seen []int

			// Two items per page, one on the short third page, none after.
			items, err := resource.FetchPages(tt.startPage, 2, tt.all, func(p int) ([]string, error) {
				seen = append(seen, p)

				switch {
				case p > 3:
					return nil, nil
				case p == 3:
					return []string{fmt.Sprintf("p%d-a", p)}, nil
				default:
					return []string{fmt.Sprintf("p%d-a", p), fmt.Sprintf("p%d-b", p)}, nil
				}
			})

			require.NoError(t, err)
			assert.Equal(t, tt.wantPages, seen)
			assert.Equal(t, tt.wantItems, items)
		})
	}
}

// TestFetchPagesWalksPagesCappedBelowTheLimit is the legacy half of the
// capped-page-size case: stopping on a page merely shorter than --limit would
// truncate here, and --limit defaults to 50 so an Engine capping lower than
// that is the likely case rather than the exotic one.
func TestFetchPagesWalksPagesCappedBelowTheLimit(t *testing.T) {
	t.Parallel()

	var seen []int

	items, err := resource.FetchPages(1, 50, true, func(p int) ([]string, error) {
		seen = append(seen, p)

		if p > 3 {
			return nil, nil
		}

		return []string{fmt.Sprintf("p%d-a", p), fmt.Sprintf("p%d-b", p)}, nil
	})

	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3, 4}, seen)
	assert.Equal(t, []string{"p1-a", "p1-b", "p2-a", "p2-b", "p3-a", "p3-b"}, items)
}

func TestFetchPagesReportsFailure(t *testing.T) {
	t.Parallel()

	_, err := resource.FetchPages(1, 2, true, func(int) ([]string, error) {
		return nil, errors.New("boom")
	})

	require.ErrorContains(t, err, "boom")
}

// TestFetchPagesGivesUpOnAnEndlessEngine is the backstop for a legacy endpoint
// that keeps answering with full pages.
func TestFetchPagesGivesUpOnAnEndlessEngine(t *testing.T) {
	t.Parallel()

	calls := 0

	_, err := resource.FetchPages(1, 1, true, func(int) ([]string, error) {
		calls++

		return []string{"always"}, nil
	})

	require.ErrorContains(t, err, "gave up after 1000 pages")
	require.ErrorContains(t, err, "narrow the results with a filter")
	require.ErrorContains(t, err, "up to 1000")
	assert.Equal(t, 1001, calls)

	// At the ceiling the advice cannot be "raise --limit", so it has to
	// suggest something the user can actually do.
	_, err = resource.FetchPages(1, resource.MaxLimit, true, func(int) ([]string, error) {
		return []string{"always"}, nil
	})

	require.ErrorContains(t, err, "narrow the results with a filter")
	require.NotContains(t, err.Error(), "raise --limit")
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

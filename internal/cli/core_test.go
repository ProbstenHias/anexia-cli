package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
)

const twoLocations = `{"data":{"page":1,"total_pages":1,"total_items":2,"limit":50,"data":[
  {"identifier":"id-1","code":"ANX04","name":"Vienna","country":"AT","city_code":"VIE"},
  {"identifier":"id-2","code":"ANX63","name":"Frankfurt","country":"DE","city_code":"FRA"}
]}}`

const oneLocation = `{"identifier":"id-1","code":"ANX04","name":"Vienna","country":"AT","city_code":"VIE"}`

const noLocations = `{"data":{"page":1,"total_pages":1,"total_items":0,"limit":50,"data":[]}}`

func TestCoreWithoutSubcommandPrintsHelp(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "core")
	require.NoError(t, err)
	require.Contains(t, stdout, "location")
	require.Contains(t, stdout, "resource")
	require.Contains(t, stdout, "tag")
	require.Contains(t, stdout, "service")
}

func TestCoreLocationOnlyHasReadVerbs(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "core", "location")
	require.NoError(t, err)
	require.Contains(t, stdout, "list")
	require.Contains(t, stdout, "get")
	require.NotContains(t, stdout, "create")
	require.NotContains(t, stdout, "delete")
}

func TestCoreLocationListTable(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, twoLocations)

	stdout, stderr, err := run(t, "core", "location", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Equal(t, "/api/core/v1/location.json", last.path)
	require.Equal(t, "limit=50&page=1", last.query)
	require.Equal(t,
		"IDENTIFIER   CODE    NAME        COUNTRY   CITY\n"+
			"id-1         ANX04   Vienna      AT        VIE\n"+
			"id-2         ANX63   Frankfurt   DE        FRA\n",
		stdout)
}

func TestCoreLocationListNoHeaders(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, twoLocations)

	stdout, _, err := run(t, "core", "location", "list", "--no-headers", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.NotContains(t, stdout, "IDENTIFIER")
	require.Contains(t, stdout, "ANX04")
}

func TestCoreLocationListTSV(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, twoLocations)

	stdout, _, err := run(t, "core", "location", "list", "-o", "tsv", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t,
		"identifier\tcode\tname\tcountry\tcity\n"+
			"id-1\tANX04\tVienna\tAT\tVIE\n"+
			"id-2\tANX63\tFrankfurt\tDE\tFRA\n",
		stdout)
}

func TestCoreLocationListJSON(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, twoLocations)

	stdout, _, err := run(t, "core", "location", "list", "-o", "json", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)

	var got []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	require.Len(t, got, 2)
	require.Equal(t, "ANX04", got[0]["code"])
	require.Equal(t, "AT", got[0]["country"])
}

func TestCoreLocationListYAML(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, twoLocations)

	stdout, _, err := run(t, "core", "location", "list", "-o", "yaml", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Contains(t, stdout, "- city_code: VIE")
	require.Contains(t, stdout, "code: ANX04")
}

func TestCoreLocationListPaging(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, twoLocations)

	_, _, err := run(t, "core", "location", "list",
		"--page", "3", "--limit", "7", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "limit=7&page=3", last.query)
}

func TestCoreLocationListEmptyTable(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, noLocations)

	stdout, stderr, err := run(t, "core", "location", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "IDENTIFIER   CODE   NAME   COUNTRY   CITY\n", stdout)
	require.Equal(t, "no locations found\n", stderr)
}

func TestCoreLocationListEmptyJSON(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, noLocations)

	stdout, _, err := run(t, "core", "location", "list", "-o", "json", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "[]\n", stdout)
}

func TestCoreLocationListServerError(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusInternalServerError, `{"error":"boom"}`)

	_, _, err := run(t, "core", "location", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.ErrorContains(t, err, "listing locations")
}

func TestCoreLocationListMissingToken(t *testing.T) {
	isolate(t)

	_, stderr, err := run(t, "core", "location", "list")
	require.ErrorContains(t, err, "not authenticated")
	require.NotContains(t, stderr, "Usage:")
}

func TestCoreLocationListInvalidOutputFormat(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "core", "location", "list", "-o", "xml", "--token", "tok")
	require.ErrorContains(t, err, `invalid output format "xml"`)
}

func TestCoreLocationListInvalidPage(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "core", "location", "list", "--page", "0", "--token", "tok")
	require.ErrorContains(t, err, "--page 0 must be 1 or greater")
}

func TestCoreLocationListInvalidLimit(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "core", "location", "list", "--limit", "1001", "--token", "tok")
	require.ErrorContains(t, err, "--limit 1001 must be between 1 and 1000")
}

func TestCoreLocationListUsesConfigFile(t *testing.T) {
	path := isolate(t)
	srv, _ := server(t, http.StatusOK, twoLocations)

	require.NoError(t, os.WriteFile(path,
		[]byte("token: file-token\napi_base_url: "+srv.URL+"\n"), 0o600))

	stdout, _, err := run(t, "core", "location", "list")
	require.NoError(t, err)
	require.Contains(t, stdout, "ANX04")
}

func TestCoreLocationListRejectsMalformedConfig(t *testing.T) {
	path := isolate(t)
	require.NoError(t, os.WriteFile(path, []byte("nope: 1\n"), 0o600))

	_, _, err := run(t, "core", "location", "list")
	require.ErrorContains(t, err, `unknown config key "nope"`)
	require.ErrorContains(t, err, path)
}

func TestCoreLocationGet(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, oneLocation)

	stdout, _, err := run(t, "core", "location", "get", "id-1", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/core/v1/location.json/id-1", last.path)
	require.Equal(t, http.MethodGet, last.method)
	require.Contains(t, stdout, "ANX04")
}

func TestCoreLocationGetNotFound(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusNotFound, `{"error":"nope"}`)

	_, _, err := run(t, "core", "location", "get", "id-x", "--token", "tok", "--api-base-url", srv.URL)
	require.ErrorContains(t, err, `reading location "id-x"`)
}

func TestCoreResourceListFiltersByTag(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK,
		`{"data":{"page":1,"total_pages":1,"total_items":1,"limit":50,"data":[
		  {"identifier":"r-1","name":"vm-1","resource_type":{"identifier":"t-1","name":"VM"},"created_at":"2026-01-01"}
		]}}`)

	stdout, _, err := run(t, "core", "resource", "list",
		"--tag", "prod", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/core/v1/resource.json", last.path)
	require.Contains(t, last.query, "tag_name=prod")
	require.Contains(t, stdout, "vm-1")
	require.Contains(t, stdout, "VM")
}

func TestCoreResourceHasTagSubcommand(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "core", "resource")
	require.NoError(t, err)
	require.Contains(t, stdout, "tag")
}

func TestCoreResourceTagList(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK,
		`{"identifier":"r-1","name":"vm-1","tags":[{"name":"prod"},{"name":"web"}]}`)

	stdout, _, err := run(t, "core", "resource", "tag", "list", "r-1",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/core/v1/resource.json/r-1", last.path)
	require.Equal(t, "NAME\nprod\nweb\n", stdout)
}

func TestCoreResourceTagListEmpty(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, `{"identifier":"r-1","name":"vm-1","tags":[]}`)

	stdout, stderr, err := run(t, "core", "resource", "tag", "list", "r-1",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "NAME\n", stdout)
	require.Equal(t, "no tags found\n", stderr)
}

func TestCoreResourceTagAdd(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, `{}`)

	_, stderr, err := run(t, "core", "resource", "tag", "add", "r-1", "prod",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, last.method)
	require.Equal(t, "/api/core/v1/resource.json/r-1/tags/prod", last.path)
	require.Equal(t, "tagged resource r-1\n", stderr)
}

func TestCoreResourceTagRemove(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusNoContent, "")

	_, stderr, err := run(t, "core", "resource", "tag", "remove", "r-1", "prod",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, http.MethodDelete, last.method)
	require.Equal(t, "/api/core/v1/resource.json/r-1/tags/prod", last.path)
	require.Equal(t, "untagged resource r-1\n", stderr)
}

// TestCoreResourceTagAddAppliesEveryTag covers the documented multi-tag form.
// Both the README and the help text advertise "add <resource-id> <tag>...", so
// a user passing three tags has to end up with three tags: silently applying
// only the first would look like it worked.
func TestCoreResourceTagAddAppliesEveryTag(t *testing.T) {
	isolate(t)

	var paths []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	_, stderr, err := run(t, "core", "resource", "tag", "add", "r-1", "prod", "staging", "eu",
		"--token", "tok", "--api-base-url", srv.URL)

	require.NoError(t, err)
	require.Equal(t, []string{
		"POST /api/core/v1/resource.json/r-1/tags/prod",
		"POST /api/core/v1/resource.json/r-1/tags/staging",
		"POST /api/core/v1/resource.json/r-1/tags/eu",
	}, paths)
	require.Equal(t, "tagged resource r-1\n", stderr)
}

// TestCoreResourceTagRemoveRemovesEveryTag is the counterpart: dropping tags
// after the first would leave the resource tagged while reporting success.
func TestCoreResourceTagRemoveRemovesEveryTag(t *testing.T) {
	isolate(t)

	var paths []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	_, stderr, err := run(t, "core", "resource", "tag", "remove", "r-1", "prod", "staging",
		"--token", "tok", "--api-base-url", srv.URL)

	require.NoError(t, err)
	require.Equal(t, []string{
		"DELETE /api/core/v1/resource.json/r-1/tags/prod",
		"DELETE /api/core/v1/resource.json/r-1/tags/staging",
	}, paths)
	require.Equal(t, "untagged resource r-1\n", stderr)
}

// TestGenericCommandsRejectHTTP300 covers an upstream boundary bug where the
// generic client treats exactly 300 as success. A read must not render an empty
// object, and a destructive command must not report success when no operation
// happened.
func TestGenericCommandsRejectHTTP300(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "read", args: []string{"core", "location", "get", "id-1"}},
		{name: "write", args: []string{"core", "resource", "tag", "remove", "r-1", "prod"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)

			srv, _ := server(t, http.StatusMultipleChoices, "")
			stdout, stderr, err := run(t, append(slices.Clone(tt.args),
				"--token", "tok", "--api-base-url", srv.URL)...)

			require.Error(t, err, "HTTP 300 is not a successful Engine operation")
			require.Equal(t, errmap.ExitError, errmap.ExitCode(err))
			require.Empty(t, stdout)
			require.NotContains(t, stderr, "untagged resource")
		})
	}
}

// TestCoreResourceGetDecodesTheEngineEnvelope covers the one Engine object in
// the tree with a custom decode path: corev1.Resource flattens its nested tag
// objects into names, and only on a get. A response-shape change there would
// otherwise surface as a runtime bug with a green suite.
func TestCoreResourceGetDecodesTheEngineEnvelope(t *testing.T) {
	isolate(t)

	srv, last := server(t, http.StatusOK, `{
	  "identifier": "r-1",
	  "name": "vm-1",
	  "resource_type": {"identifier": "t-1", "name": "Virtual Machine"},
	  "created_at": "2024-01-02",
	  "tags": [{"name": "prod"}, {"name": "eu"}]
	}`)

	stdout, _, err := run(t, "core", "resource", "get", "r-1", "-o", "json",
		"--token", "tok", "--api-base-url", srv.URL)

	require.NoError(t, err)
	require.Equal(t, "/api/core/v1/resource.json/r-1", last.path)

	var got struct {
		Identifier string   `json:"identifier"`
		Name       string   `json:"name"`
		Tags       []string `json:"tags"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))

	require.Equal(t, "r-1", got.Identifier)
	require.Equal(t, "vm-1", got.Name)
	require.Equal(t, []string{"prod", "eu"}, got.Tags, "nested tag objects must arrive as names")
}

func TestCoreServiceList(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK,
		`{"data":[{"identifier":"s-1","name":"vsphere","title":"vSphere","category":"compute"}]}`)

	stdout, _, err := run(t, "core", "service", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/core/v1/service.json", last.path)
	require.Equal(t,
		"IDENTIFIER   NAME      TITLE     CATEGORY\n"+
			"s-1          vsphere   vSphere   compute\n",
		stdout)
}

// TestCoreServiceListAllWalksPages pins that --all works the same on a command
// written against the legacy client, which reports no page metadata at all.
// TestListAllKeepsResultsWhenAPageAfterTheLastIsRejected covers both halves of
// the CLI against an Engine that answers a page past the end with 404 instead
// of an empty list. Since the walk always asks one page beyond the results,
// such an Engine would otherwise make --all fail on every complete walk and
// throw away everything already collected. The registry-driven and the
// hand-written commands have to agree here, so both are driven the same way.
func TestListAllKeepsResultsWhenAPageAfterTheLastIsRejected(t *testing.T) {
	isolate(t)

	tests := []struct {
		name    string
		args    []string
		body    func(page int) string
		wantIDs []string
	}{
		{
			name: "registry command",
			args: []string{"core", "location", "list"},
			body: func(page int) string {
				return fmt.Sprintf(
					`{"data":{"page":%d,"limit":1,"data":[{"identifier":"l-%d","code":"ANX0%d"}]}}`,
					page, page, page)
			},
			wantIDs: []string{"l-1", "l-2"},
		},
		{
			name: "legacy command",
			args: []string{"core", "service", "list"},
			body: func(page int) string {
				return fmt.Sprintf(`{"data":[{"identifier":"s-%d","name":"svc-%d"}]}`, page, page)
			},
			wantIDs: []string{"s-1", "s-2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lastPage := 2

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				page, err := strconv.Atoi(r.URL.Query().Get("page"))
				require.NoError(t, err)

				w.Header().Set("Content-Type", "application/json")

				if page > lastPage {
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"error":{"code":404,"message":"no such page"}}`))

					return
				}

				_, _ = w.Write([]byte(tt.body(page)))
			}))
			t.Cleanup(srv.Close)

			args := append(slices.Clone(tt.args), "--all", "--limit", "1", "-o", "json",
				"--token", "tok", "--api-base-url", srv.URL)

			stdout, stderr, err := run(t, args...)

			require.NoError(t, err, "the walk must end on the rejected page, not fail")

			var got []struct {
				Identifier string `json:"identifier"`
			}
			require.NoError(t, json.Unmarshal([]byte(stdout), &got))

			ids := make([]string, 0, len(got))
			for _, g := range got {
				ids = append(ids, g.Identifier)
			}

			require.Equal(t, tt.wantIDs, ids)

			// The same 404 could be a deleted parent or a flaky proxy, so both
			// halves have to say the results may be short rather than let
			// partial output look complete.
			require.Contains(t, stderr, "stopped at page 3")
		})
	}
}

func TestCoreServiceListAllWalksPages(t *testing.T) {
	isolate(t)

	var seen []int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		require.NoError(t, err)

		seen = append(seen, page)

		// Two services per page, one on the short third page, none after.
		// The empty fourth page is what ends the walk: a short page is not
		// proof of the end, because the Engine may cap its page size below
		// the requested limit.
		var names []string

		switch {
		case page > 3:
		case page == 3:
			names = []string{"a"}
		default:
			names = []string{"a", "b"}
		}

		services := make([]map[string]string, 0, len(names))
		for _, n := range names {
			services = append(services, map[string]string{
				"identifier": fmt.Sprintf("s-%d-%s", page, n),
				"name":       n,
			})
		}

		body, err := json.Marshal(map[string]any{"data": services})
		require.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	stdout, _, err := run(t, "core", "service", "list", "--all", "--limit", "2", "-o", "json",
		"--token", "tok", "--api-base-url", srv.URL)

	require.NoError(t, err)
	require.Equal(t, []int{1, 2, 3, 4}, seen)

	// Exact and ordered, so a page collected twice cannot pass.
	var got []struct {
		Identifier string `json:"identifier"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))

	ids := make([]string, 0, len(got))
	for _, g := range got {
		ids = append(ids, g.Identifier)
	}

	require.Equal(t, []string{"s-1-a", "s-1-b", "s-2-a", "s-2-b", "s-3-a"}, ids)
}

func TestCoreServiceListEmpty(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, `{"data":[]}`)

	_, stderr, err := run(t, "core", "service", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "no services found\n", stderr)
}

func TestCoreServiceListJSON(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, `{"data":[]}`)

	stdout, _, err := run(t, "core", "service", "list", "-o", "json",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "[]\n", stdout)
}

func TestCoreServiceListInvalidPaging(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "core", "service", "list", "--page", "0", "--token", "tok")
	require.ErrorContains(t, err, "--page 0 must be 1 or greater")

	_, _, err = run(t, "core", "service", "list", "--limit", "0", "--token", "tok")
	require.ErrorContains(t, err, "--limit 0 must be between 1 and 1000")
}

func TestCoreTagList(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK,
		`{"data":[{"name":"prod","identifier":"t-1"},{"name":"web","identifier":"t-2"}]}`)

	stdout, _, err := run(t, "core", "tag", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/core/v1/tags.json", last.path)
	require.Equal(t,
		"NAME   IDENTIFIER\n"+
			"prod   t-1\n"+
			"web    t-2\n",
		stdout)
}

func TestCoreTagListPassesFilters(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, `{"data":[]}`)

	_, _, err := run(t, "core", "tag", "list",
		"--name", "pro", "--service", "s-1", "--organization", "o-1",
		"--order", "name", "--descending",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Contains(t, last.query, "query=pro")
	require.Contains(t, last.query, "service_identifier=s-1")
	require.Contains(t, last.query, "organization_identifier=o-1")
	require.Contains(t, last.query, "order=name")
	require.Contains(t, last.query, "sort_descending=true")
}

// TestCoreTagListFilterValuesReachTheEngineIntact covers filter values that are
// not already URL-safe. A user filtering on a tag whose name has a space is
// ordinary, and a value must arrive as one parameter rather than becoming
// several, whichever client the command happens to use.
func TestCoreTagListFilterValuesReachTheEngineIntact(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "a space", value: "my tag", want: "my tag"},
		{name: "an ampersand", value: "a&b", want: "a&b"},
		{name: "a value that looks like more parameters", value: "x&limit=1&page=9", want: "x&limit=1&page=9"},
		{name: "a plus", value: "a+b", want: "a+b"},
		{name: "a hash", value: "a#b", want: "a#b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)

			srv, last := server(t, http.StatusOK, `{"data":[]}`)

			_, _, err := run(t, "core", "tag", "list", "--name", tt.value,
				"--token", "tok", "--api-base-url", srv.URL)
			require.NoError(t, err)

			// Read the query the way a server does, so an injected extra
			// parameter shows up as a changed value rather than being missed.
			values, err := url.ParseQuery(last.query)
			require.NoError(t, err)

			require.Equal(t, tt.want, values.Get("query"),
				"the filter must arrive as one value, got query %q", last.query)

			// Paging must still be what the CLI asked for, not something the
			// filter value smuggled in.
			require.Equal(t, "1", values.Get("page"))
			require.Equal(t, "50", values.Get("limit"))
		})
	}
}

func TestCoreTagListEmpty(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, `{"data":[]}`)

	_, stderr, err := run(t, "core", "tag", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "no tags found\n", stderr)
}

func TestCoreTagListJSON(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, `{"data":[]}`)

	stdout, _, err := run(t, "core", "tag", "list", "-o", "json",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "[]\n", stdout)
}

func TestCoreTagListInvalidPaging(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "core", "tag", "list", "--page", "0", "--token", "tok")
	require.ErrorContains(t, err, "--page 0 must be 1 or greater")

	_, _, err = run(t, "core", "tag", "list", "--limit", "1001", "--token", "tok")
	require.ErrorContains(t, err, "--limit 1001 must be between 1 and 1000")
}

// TestListAllRejectsAPageThatCannotBeIncremented covers the signed page
// counter boundary on both clients. Wrapping MaxInt to a negative page can send
// a request for a page the user never asked for and breaks end-of-walk checks.
func TestListAllRejectsAPageThatCannotBeIncremented(t *testing.T) {
	tests := []struct {
		args []string
		body string
	}{
		{args: []string{"core", "location", "list"}, body: `{"data":{"page":1,"limit":1,"data":[{"identifier":"l-1"}]}}`},
		{args: []string{"core", "service", "list"}, body: `{"data":[{"identifier":"s-1","name":"svc"}]}`},
	}

	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			isolate(t)

			srv, last := server(t, http.StatusOK, tt.body)
			args := append(slices.Clone(tt.args), "--all", "--page", strconv.Itoa(int(^uint(0)>>1)),
				"--token", "tok", "--api-base-url", srv.URL)

			_, _, err := run(t, args...)

			require.Error(t, err)
			require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
			require.ErrorContains(t, err, "too large for --all")
			require.Empty(t, last.path, "an impossible walk must be rejected before a request")
		})
	}
}

func TestCoreTagGet(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK,
		`{"name":"prod","identifier":"t-1","organisation_assignments":[
		  {"service":{"name":"vsphere","identifier":"s-1"},"customer":{"name":"ACME","identifier":"c-1"}}
		]}`)

	stdout, _, err := run(t, "core", "tag", "get", "t-1", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/core/v1/tags.json/t-1", last.path)
	require.Equal(t,
		"NAME   IDENTIFIER   SERVICE   CUSTOMER\n"+
			"prod   t-1          vsphere   ACME\n",
		stdout)
}

// TestCoreTagGetWithoutAssignments pins that an unassigned tag drops the
// per-organisation columns instead of padding them, which would leave trailing
// empty fields in tsv.
func TestCoreTagGetWithoutAssignments(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, `{"name":"prod","identifier":"t-1"}`)

	stdout, _, err := run(t, "core", "tag", "get", "t-1", "-o", "tsv",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "name\tidentifier\nprod\tt-1\n", stdout)
}

func TestCoreTagGetJSON(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, `{"name":"prod","identifier":"t-1"}`)

	stdout, _, err := run(t, "core", "tag", "get", "t-1", "-o", "json",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	require.Equal(t, "prod", got["name"])
}

func TestCoreTagCreate(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, `{"name":"prod","identifier":"t-1"}`)

	stdout, _, err := run(t, "core", "tag", "create",
		"--name", "prod", "--service", "s-1", "--organization", "o-1",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, last.method)
	require.Contains(t, last.body, `"name":"prod"`)
	require.Contains(t, last.body, `"service_identifier":"s-1"`)
	require.Contains(t, stdout, "prod   t-1")
}

func TestCoreTagCreateRequiresNameAndService(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "core", "tag", "create", "--token", "tok")
	require.ErrorContains(t, err, "--name is required")

	_, _, err = run(t, "core", "tag", "create", "--name", "prod", "--token", "tok")
	require.ErrorContains(t, err, "--service is required")
}

func TestCoreTagCreateJSON(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, `{"name":"prod","identifier":"t-1"}`)

	stdout, _, err := run(t, "core", "tag", "create", "-o", "json",
		"--name", "prod", "--service", "s-1",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	require.Equal(t, "t-1", got["identifier"])
}

func TestCoreTagDeleteConfirmed(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusNoContent, "")

	_, stderr, err := run(t, "core", "tag", "delete", "t-1", "--service", "s-1", "--yes",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, http.MethodDelete, last.method)
	require.Contains(t, last.query, "service_identifier=s-1")
	require.Equal(t, "deleted tag t-1\n", stderr)
}

// TestCoreTagIdentifiersAddressExactlyTheObjectAsked covers the identifier the
// user passes positionally, which the legacy client interpolates into the URL
// path. An identifier carrying a query or fragment character must address the
// object the user named, or nothing: reading or deleting a different object and
// reporting success is the worst outcome available here.
//
// The generic half sends such an identifier escaped and gets an honest miss, so
// this is also the two halves agreeing.
func TestCoreTagIdentifiersAddressExactlyTheObjectAsked(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "get", args: []string{"core", "tag", "get"}},
		{name: "delete", args: []string{"core", "tag", "delete"}},
	}

	for _, tt := range tests {
		for _, id := range []string{"t-1?service_identifier=other", "tag with space"} {
			t.Run(tt.name+" "+id, func(t *testing.T) {
				isolate(t)

				srv, last := server(t, http.StatusOK, `{"data":{"identifier":"t-1","name":"prod"}}`)

				args := append(slices.Clone(tt.args), id,
					"--token", "tok", "--api-base-url", srv.URL)
				if tt.name == "delete" {
					args = append(args, "--service", "s-1", "--yes")
				}

				_, _, err := run(t, args...)
				require.NoError(t, err)

				// The whole identifier belongs in the path, so the Engine can
				// answer for the object the user actually named. last.path is the
				// decoded path, so this compares against the identifier as typed.
				require.Equal(t, "/api/core/v1/tags.json/"+id, last.path)

				// And it must not have leaked into the query, where on a delete it
				// would override the flag the user passed. Asserting the whole
				// query rather than one key, because the smuggled key is whatever
				// the identifier happens to contain.
				values, err := url.ParseQuery(last.query)
				require.NoError(t, err)

				if tt.name == "delete" {
					require.Equal(t, url.Values{"service_identifier": {"s-1"}}, values)
				} else {
					require.Empty(t, last.query)
				}
			})
		}
	}
}

// TestIdentifiersThatAddressNoObjectAreRejected covers arguments that name no
// object at all. An empty identifier addresses the collection rather than a
// member of it, and a relative path segment walks out of the endpoint, so
// sending either lets the Engine act on something the user never named. A
// delete is the case that matters: it reported success.
//
// Every command taking an identifier is covered, on both clients, because this
// has to be a property of the CLI rather than of whichever client a command
// happens to use.
func TestIdentifiersThatAddressNoObjectAreRejected(t *testing.T) {
	commands := [][]string{
		{"core", "location", "get"},
		{"core", "resource", "get"},
		{"core", "tag", "get"},
		{"core", "tag", "delete", "--service", "s-1", "--yes"},
		{"core", "resource", "tag", "list"},
	}

	// Arguments that name nothing: empty, whitespace only, the current and
	// parent directory, and a bare separator.
	ids := []string{"", "   ", ".", "..", "/", "../..", "a/../..", "a/b"}

	for _, cmd := range commands {
		for _, id := range ids {
			name := fmt.Sprintf("%s %q", strings.Join(cmd, " "), id)

			t.Run(name, func(t *testing.T) {
				isolate(t)

				srv, last := server(t, http.StatusOK, `{"data":[]}`)

				args := append(slices.Clone(cmd), id,
					"--token", "tok", "--api-base-url", srv.URL)

				_, _, err := run(t, args...)

				require.Error(t, err, "an identifier naming no object must be refused")
				require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
				require.Empty(t, last.path, "nothing may reach the Engine")
			})
		}
	}
}

// TestRelationTagsThatAddressNoObjectAreRejected is the same rule for the tag
// name in a relation verb. "remove r-1 .." resolved to the resource itself, so
// a command that removes a tag issued a delete against its parent.
func TestRelationTagsThatAddressNoObjectAreRejected(t *testing.T) {
	for _, verb := range []string{"add", "remove"} {
		for _, tag := range []string{"", "..", "/", "a/../..", "prod/staging"} {
			t.Run(fmt.Sprintf("%s %q", verb, tag), func(t *testing.T) {
				isolate(t)

				srv, last := server(t, http.StatusOK, `{}`)

				_, _, err := run(t, "core", "resource", "tag", verb, "r-1", tag,
					"--token", "tok", "--api-base-url", srv.URL)

				require.Error(t, err, "a tag naming nothing must be refused")
				require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
				require.Empty(t, last.path, "nothing may reach the Engine")
			})
		}
	}
}

// TestRelationValuesStayInTheirOwnPathSegment covers the resource identifier
// and tag name reaching the Engine intact. Both go into the URL path, and
// neither may end the segment it belongs to.
func TestRelationValuesStayInTheirOwnPathSegment(t *testing.T) {
	isolate(t)

	srv, last := server(t, http.StatusOK, `{}`)

	_, _, err := run(t, "core", "resource", "tag", "add", "r-1?x=1", "prod#frag",
		"--token", "tok", "--api-base-url", srv.URL)

	require.NoError(t, err)

	// last.path is decoded, so this compares against the values as typed.
	require.Equal(t, "/api/core/v1/resource.json/r-1?x=1/tags/prod#frag", last.path)
	require.Empty(t, last.query)
}

// TestCoreTagDeleteEscapesTheServiceIdentifier is the delete-side counterpart:
// the same legacy client builds this query the same way, so a value needing
// escaping must not be able to add parameters to a destructive request.
func TestCoreTagDeleteEscapesTheServiceIdentifier(t *testing.T) {
	isolate(t)

	srv, last := server(t, http.StatusNoContent, "")

	_, _, err := run(t, "core", "tag", "delete", "t-1", "--service", "s 1&x=2", "--yes",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)

	values, err := url.ParseQuery(last.query)
	require.NoError(t, err)

	require.Equal(t, "s 1&x=2", values.Get("service_identifier"),
		"the identifier must arrive as one value, got query %q", last.query)
	require.Empty(t, values.Get("x"), "a filter value must not become another parameter")
}

func TestCoreTagDeleteRequiresService(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "core", "tag", "delete", "t-1", "--yes", "--token", "tok")
	require.ErrorContains(t, err, "--service is required")
}

func TestCoreTagDeleteWithoutConfirmation(t *testing.T) {
	isolate(t)

	_, stderr, err := run(t, "core", "tag", "delete", "t-1", "--service", "s-1", "--token", "tok")
	require.ErrorContains(t, err, "canceled")
	require.Contains(t, stderr, `delete tag "t-1" [y/N]:`)
}

func TestCoreTagDeleteAcceptedAtPrompt(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusNoContent, "")

	_, _, err := runWithInput(t, "y\n", "core", "tag", "delete", "t-1", "--service", "s-1",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, http.MethodDelete, last.method)
}

func TestCoreTagDeclinedAtPrompt(t *testing.T) {
	isolate(t)

	_, _, err := runWithInput(t, "n\n", "core", "tag", "delete", "t-1", "--service", "s-1", "--token", "tok")
	require.ErrorContains(t, err, "canceled")
}

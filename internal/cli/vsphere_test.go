package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ProbstenHias/anexia-cli/internal/cli"
	"github.com/ProbstenHias/anexia-cli/internal/errmap"
)

func TestVSphereLocationList(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, `{"data":[`+
		`{"id":"l-1","code":"ANX01","name":"Vienna","country_name":"Austria"},`+
		`{"id":"l-2","code":"ANX02","name":"Graz","country":"AT"}]}`)

	// A user sees the ISO country code when the Engine omits its display name.
	stdout, _, err := run(t, "vsphere", "location", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/vsphere/v1/provisioning/location.json", last.path)
	// A user gets the standard identifier header in scripts and table output.
	require.Equal(t, "IDENTIFIER   CODE    NAME     COUNTRY\n"+
		"l-1          ANX01   Vienna   Austria\n"+
		"l-2          ANX02   Graz     AT\n", stdout)
}

func TestVSphereListShortDescriptions(t *testing.T) {
	root := cli.NewRootCommand(cli.Deps{})
	for _, tt := range []struct {
		path  []string
		short string
	}{
		{[]string{"vsphere", "location", "list"}, "List locations"},
		{[]string{"vsphere", "template", "list"}, "List templates"},
		{[]string{"vsphere", "disk-type", "list"}, "List disk types"},
	} {
		t.Run(tt.short, func(t *testing.T) {
			// A user sees the same concise list description across vSphere resources.
			cmd, _, err := root.Find(tt.path)
			require.NoError(t, err)
			require.Equal(t, tt.short, cmd.Short)
		})
	}
}

func TestVSphereLocationListEscapesFilters(t *testing.T) {
	isolate(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A user can filter with punctuation without it becoming another query parameter.
		require.Equal(t, "Vienna & East", r.URL.Query().Get("location_code"))
		require.Equal(t, "org & team", r.URL.Query().Get("organization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(srv.Close)

	_, _, err := run(t, "vsphere", "location", "list", "--code", "Vienna & East", "--organization", "org & team",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
}

func TestVSphereLocationListPassesPaging(t *testing.T) {
	isolate(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A user can select an exact page size and number.
		require.Equal(t, url.Values{"limit": {"7"}, "page": {"3"}}, r.URL.Query())
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(srv.Close)

	_, _, err := run(t, "vsphere", "location", "list", "--page", "3", "--limit", "7", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
}

func TestVSphereTemplateList(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, `[{"id":"t-1","name":"Ubuntu","build":"24.04","bit":"64"}]`)

	stdout, _, err := run(t, "vsphere", "template", "list", "--location", "l-1", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/vsphere/v1/provisioning/templates.json/l-1/templates", last.path)
	query, err := url.ParseQuery(last.query)
	require.NoError(t, err)
	require.Equal(t, url.Values{"limit": {"1000"}, "page": {"1"}}, query)
	require.Equal(t, "IDENTIFIER   NAME     BUILD   BIT\n"+
		"t-1          Ubuntu   24.04   64\n", stdout)
}

func TestVSphereTemplateListPassesType(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, `[]`)

	_, _, err := run(t, "vsphere", "template", "list", "--location", "l-1", "--type", "from_scratch", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/vsphere/v1/provisioning/templates.json/l-1/from_scratch", last.path)
}

func TestVSphereTemplateListAllFetchesOnce(t *testing.T) {
	isolate(t)
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		// A user asking for all templates receives the one complete response without a loop.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"t-1"},{"id":"t-2"}]`))
	}))
	t.Cleanup(srv.Close)

	stdout, _, err := run(t, "vsphere", "template", "list", "--location", "l-1", "--all", "-o", "json", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, 1, requests)
	require.Equal(t, []string{"t-1", "t-2"}, vsphereIdentifiers(t, stdout))
}

func TestVSphereTemplateListRejectsLaterPage(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, `[]`)

	// A user is told that templates have no second page instead of seeing page one twice.
	_, _, err := run(t, "vsphere", "template", "list", "--location", "l-1", "--page", "2", "--token", "tok", "--api-base-url", srv.URL)
	require.ErrorContains(t, err, "does not support paging")
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.Empty(t, last.path)
}

func TestVSphereTemplateListRejectsInvalidType(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "vsphere", "template", "list", "--location", "l-1", "--type", "invalid", "--token", "tok")
	require.ErrorContains(t, err, `--type must be "templates" or "from_scratch"`)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
}

func TestVSphereTemplateGet(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, `[`+
		`{"id":"t-other","name":"Other","build":"23.10","bit":"32"},`+
		`{"id":"t-1","name":"Ubuntu","build":"24.04","bit":"64"}]`)

	// A user can retrieve one template from the scoped template collection.
	stdout, _, err := run(t, "vsphere", "template", "get", "t-1", "--location", "l-1", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/vsphere/v1/provisioning/templates.json/l-1/templates", last.path)
	query, err := url.ParseQuery(last.query)
	require.NoError(t, err)
	require.Equal(t, url.Values{"limit": {"1000"}, "page": {"1"}}, query)
	require.Equal(t, "IDENTIFIER   NAME     BUILD   BIT\n"+
		"t-1          Ubuntu   24.04   64\n", stdout)
}

func TestVSphereTemplateGetMissingFromCollection(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, `[]`)

	// A user sees not found when the requested template is absent from the scoped collection.
	_, _, err := run(t, "vsphere", "template", "get", "t-missing", "--location", "l-1", "--token", "tok", "--api-base-url", srv.URL)
	require.Error(t, err)
	require.Equal(t, errmap.ExitNotFound, errmap.ExitCode(err))
	require.ErrorContains(t, err, `reading template "t-missing":`)
}

func TestVSphereTemplateRejectsInvalidLocation(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"list dot dot", []string{"vsphere", "template", "list", "--location", ".."}},
		{"list whitespace", []string{"vsphere", "template", "list", "--location", "   "}},
		{"list slash", []string{"vsphere", "template", "list", "--location", "a/b"}},
		{"get dot dot", []string{"vsphere", "template", "get", "t-1", "--location", ".."}},
		{"get whitespace", []string{"vsphere", "template", "get", "t-1", "--location", "   "}},
		{"get slash", []string{"vsphere", "template", "get", "t-1", "--location", "a/b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			srv, last := server(t, http.StatusOK, `[]`)

			// A user cannot turn a template location into a different URL path.
			_, _, err := run(t, append(tt.args, "--token", "tok", "--api-base-url", srv.URL)...)
			require.Error(t, err)
			require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
			require.Empty(t, last.path)
		})
	}
}

func TestVSphereTemplateListEscapesLocation(t *testing.T) {
	isolate(t)
	location := "loc ?#% name"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A user can address a template location with URL punctuation as one path segment.
		require.Equal(t, "/api/vsphere/v1/provisioning/templates.json/"+location+"/templates", r.URL.Path)
		require.Equal(t, url.Values{"limit": {"1000"}, "page": {"1"}}, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	_, _, err := run(t, "vsphere", "template", "list", "--location", location, "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
}

func TestVSphereDiskTypeList(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK,
		`[{"id":"d-1","storage_type":"ssd","bandwidth":100,"iops":200,"latency":3}]`)

	stdout, _, err := run(t, "vsphere", "disk-type", "list", "--location", "l-1", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/vsphere/v1/provisioning/disk_type.json/l-1", last.path)
	// A user gets the documented hyphenated storage-type header.
	require.Equal(t, "IDENTIFIER   STORAGE-TYPE   BANDWIDTH   IOPS   LATENCY\n"+
		"d-1          ssd            100         200    3\n", stdout)
}

func TestVSphereDiskTypeListPassesPaging(t *testing.T) {
	isolate(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A user can select an exact page size and number.
		require.Equal(t, "3", r.URL.Query().Get("page"))
		require.Equal(t, "7", r.URL.Query().Get("limit"))
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	_, _, err := run(t, "vsphere", "disk-type", "list", "--location", "l-1", "--page", "3", "--limit", "7", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
}

func TestVSphereDiskTypeListEscapesLocation(t *testing.T) {
	isolate(t)
	location := "loc ?#% name"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A user can address a location containing URL punctuation as one path segment.
		require.Equal(t, "/api/vsphere/v1/provisioning/disk_type.json/"+location, r.URL.Path)
		require.Equal(t, url.Values{"limit": {"7"}, "page": {"3"}}, r.URL.Query())
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	_, _, err := run(t, "vsphere", "disk-type", "list", "--location", location, "--page", "3", "--limit", "7", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
}

func TestVSphereDiskTypeListRejectsInvalidLocation(t *testing.T) {
	for _, location := range []string{".", ".."} {
		t.Run(location, func(t *testing.T) {
			isolate(t)
			srv, last := server(t, http.StatusOK, `[]`)

			// A user cannot turn a location identifier into a relative path.
			_, _, err := run(t, "vsphere", "disk-type", "list", "--location", location, "--token", "tok", "--api-base-url", srv.URL)
			require.Error(t, err)
			require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
			require.Empty(t, last.path)
		})
	}
}

func TestVSphereListsReportEmptyResults(t *testing.T) {
	tests := []struct {
		name string
		args []string
		body string
		want string
	}{
		{"location", []string{"vsphere", "location", "list"}, `{"data":[]}`, "no locations found\n"},
		{"template", []string{"vsphere", "template", "list", "--location", "l-1"}, `[]`, "no templates found\n"},
		{"disk type", []string{"vsphere", "disk-type", "list", "--location", "l-1"}, `[]`, "no disk types found\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			srv, _ := server(t, http.StatusOK, tt.body)
			_, stderr, err := run(t, append(tt.args, "--token", "tok", "--api-base-url", srv.URL)...)
			require.NoError(t, err)
			require.Equal(t, tt.want, stderr)
		})
	}
}

func TestVSphereListsRenderEmptyJSON(t *testing.T) {
	tests := []struct {
		name string
		args []string
		body string
	}{
		{"location", []string{"vsphere", "location", "list"}, `{"data":[]}`},
		{"template", []string{"vsphere", "template", "list", "--location", "l-1"}, `[]`},
		{"disk type", []string{"vsphere", "disk-type", "list", "--location", "l-1"}, `[]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			srv, _ := server(t, http.StatusOK, tt.body)
			stdout, _, err := run(t, append(tt.args, "-o", "json", "--token", "tok", "--api-base-url", srv.URL)...)
			require.NoError(t, err)
			require.Equal(t, "[]\n", stdout)
		})
	}
}

func TestVSphereListsRejectInvalidPaging(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"location", []string{"vsphere", "location", "list"}},
		{"template", []string{"vsphere", "template", "list", "--location", "l-1"}},
		{"disk type", []string{"vsphere", "disk-type", "list", "--location", "l-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			_, _, err := run(t, append(tt.args, "--page", "0", "--token", "tok")...)
			require.ErrorContains(t, err, "--page 0 must be 1 or greater")
			require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
			_, _, err = run(t, append(tt.args, "--limit", "0", "--token", "tok")...)
			require.ErrorContains(t, err, "--limit 0 must be between 1 and 1000")
			require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
		})
	}
}

// TestVSphereTemplateListReportsUnservablePageBeforeAMissingToken pins the
// order the checks run in. Asking for page two of an endpoint that never pages
// is the user's actual mistake; reporting a missing token ahead of it sends
// them after the wrong problem and exits 3 where the contract calls for 2.
func TestVSphereTemplateListReportsUnservablePageBeforeAMissingToken(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "vsphere", "template", "list", "--location", "l-1", "--page", "2")
	require.Error(t, err)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.Contains(t, errmap.Message(err), "does not support paging")
}

func TestVSphereListsRequireLocation(t *testing.T) {
	for _, args := range [][]string{{"vsphere", "template", "list"}, {"vsphere", "disk-type", "list"}} {
		t.Run(args[1], func(t *testing.T) {
			isolate(t)
			_, _, err := run(t, append(args, "--token", "tok")...)
			require.ErrorContains(t, err, "--location is required")
			require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
		})
	}
}

func TestVSphereLegacyListsWalkPages(t *testing.T) {
	tests := []struct {
		name string
		args []string
		body string
		want []string
	}{
		{"location", []string{"vsphere", "location", "list"}, `{"data":[{"id":"l-%d"}]}`, []string{"l-1", "l-2"}},
		{"disk type", []string{"vsphere", "disk-type", "list", "--location", "l-1"}, `[{"id":"d-%d"}]`, []string{"d-1", "d-2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			var seen []int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				page, err := strconv.Atoi(r.URL.Query().Get("page"))
				require.NoError(t, err)
				seen = append(seen, page)
				if page < 3 {
					_, _ = fmt.Fprintf(w, tt.body, page)
					return
				}
				if tt.name == "location" {
					_, _ = w.Write([]byte(`{"data":[]}`))
					return
				}
				_, _ = w.Write([]byte(`[]`))
			}))
			t.Cleanup(srv.Close)

			stdout, _, err := run(t, append(tt.args, "--all", "--limit", "1", "-o", "json", "--token", "tok", "--api-base-url", srv.URL)...)
			require.NoError(t, err)
			require.Equal(t, []int{1, 2, 3}, seen)
			require.Equal(t, tt.want, vsphereIdentifiers(t, stdout))
		})
	}
}

func TestVSphereListNotFound(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		prefix string
	}{
		{"location", []string{"vsphere", "location", "list"}, "listing locations:"},
		{"template", []string{"vsphere", "template", "list", "--location", "l-1"}, "listing templates:"},
		{"disk type", []string{"vsphere", "disk-type", "list", "--location", "l-1"}, "listing disk types:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			srv, _ := server(t, http.StatusNotFound, `{"error":{"code":404,"message":"missing"}}`)

			// A user receives a not-found result with the operation that failed.
			_, _, err := run(t, append(tt.args, "--token", "tok", "--api-base-url", srv.URL)...)
			require.Error(t, err)
			require.Equal(t, errmap.ExitNotFound, errmap.ExitCode(err))
			require.ErrorContains(t, err, tt.prefix)
		})
	}
}

func vsphereIdentifiers(t *testing.T, text string) []string {
	t.Helper()
	var objects []struct {
		Identifier string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &objects))
	identifiers := make([]string, 0, len(objects))
	for _, object := range objects {
		identifiers = append(identifiers, object.Identifier)
	}
	return identifiers
}

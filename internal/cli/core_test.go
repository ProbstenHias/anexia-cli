package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
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
func TestCoreServiceListAllWalksPages(t *testing.T) {
	isolate(t)

	var seen []int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		require.NoError(t, err)

		seen = append(seen, page)

		// Two services per page until page three, which is short and ends
		// the walk.
		names := []string{"a", "b"}
		if page >= 3 {
			names = names[:1]
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

	stdout, _, err := run(t, "core", "service", "list", "--all", "--limit", "2",
		"--token", "tok", "--api-base-url", srv.URL)

	require.NoError(t, err)
	require.Equal(t, []int{1, 2, 3}, seen)

	for _, want := range []string{"s-1-a", "s-1-b", "s-2-a", "s-2-b", "s-3-a"} {
		require.Contains(t, stdout, want)
	}
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

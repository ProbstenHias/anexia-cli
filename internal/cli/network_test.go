package cli_test

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
)

const twoVlans = `{"data":{"page":1,"total_pages":1,"total_items":2,"limit":50,"data":[
  {"identifier":"v-1","name":"VLAN2000","status":"Active","locations":[{"identifier":"l-1","code":"ANX04"}]},
  {"identifier":"v-2","name":"VLAN2001","status":"Pending","locations":[]}
]}}`

const noVlans = `{"data":{"page":1,"total_pages":1,"total_items":0,"limit":50,"data":[]}}`

const oneVlan = `{"identifier":"v-1","name":"VLAN2000","description_customer":"office","role_text":"Customer","status":"Active","vm_provisioning":true,"locations":[{"identifier":"l-1","name":"ANX04"}]}`

const twoPrefixes = `{"data":[
  {"identifier":"p-1","name":"10.0.0.0/24","description_customer":"office"},
  {"identifier":"p-2","name":"10.0.1.0/24","description_customer":"lab"}
]}`

const noPrefixes = `{"data":[]}`

const twoAddresses = `{"data":{"data":[
  {"identifier":"a-1","name":"10.0.0.1","role_text":"Default","description_customer":"gateway"},
  {"identifier":"a-2","name":"10.0.0.2","role_text":"Default","description_customer":"host"}
]}}`

const noAddresses = `{"data":{"data":[]}}`

func TestNetworkWithoutSubcommandPrintsHelp(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "network")
	require.NoError(t, err)
	require.Contains(t, stdout, "vlan")
	require.Contains(t, stdout, "prefix")
	require.Contains(t, stdout, "address")
}

func TestNetworkAddressOnlyHasReadVerbs(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "network", "address")
	require.NoError(t, err)
	require.Contains(t, stdout, "list")
	require.Contains(t, stdout, "get")
	for _, absent := range []string{"create", "update", "delete", "destroy", "reserve"} {
		require.NotContains(t, stdout, absent,
			"address write verbs are still to be declared")
	}
}

// Prefixes are writable in go-anxcloud's legacy ipam client, so the noun offers
// the same five verbs a registry-driven noun does.
func TestNetworkPrefixHasEveryVerb(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "network", "prefix")
	require.NoError(t, err)
	for _, verb := range []string{"list", "get", "create", "update", "delete"} {
		require.Contains(t, stdout, verb)
	}
}

func TestNetworkVlanHasEveryVerb(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "network", "vlan")
	require.NoError(t, err)
	for _, verb := range []string{"list", "get", "create", "update", "delete"} {
		require.Contains(t, stdout, verb)
	}
}

// The VLAN endpoint does not return the same location shape the location
// endpoint does: go-anxcloud's own fixtures for it carry the site code in
// "name" and no "code" at all. Reading only "code" leaves the column blank for
// every VLAN, which is the one thing this column exists to say.
func TestNetworkVlanListRendersTheLocationTheVlanEndpointReturns(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK,
		`{"data":{"page":1,"total_pages":1,"total_items":1,"limit":50,"data":[
		  {"identifier":"v-1","name":"VLAN2000","status":"Active",
		   "locations":[{"identifier":"l-1","name":"ANX04"}]}
		]}}`)

	stdout, _, err := run(t, "network", "vlan", "list", "-o", "tsv", "--no-headers",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "v-1\tVLAN2000\tActive\tANX04\n", stdout)
}

func TestNetworkVlanListTable(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, twoVlans)

	stdout, stderr, err := run(t, "network", "vlan", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Equal(t, "/api/vlan/v1/vlan.json/filtered", last.path)
	require.Equal(t, "limit=50&page=1", last.query)
	require.Equal(t,
		"IDENTIFIER   NAME       STATUS    LOCATION\n"+
			"v-1          VLAN2000   Active    ANX04\n"+
			"v-2          VLAN2001   Pending   \n",
		stdout)
}

func TestNetworkVlanListFiltersByStatusAndLocation(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, twoVlans)

	_, _, err := run(t, "network", "vlan", "list",
		"--status", "Active", "--location", "l-1", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Contains(t, last.query, "status=Active")
	require.Contains(t, last.query, "location=l-1")
}

func TestNetworkVlanListJSON(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, twoVlans)

	stdout, _, err := run(t, "network", "vlan", "list", "-o", "json", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)

	var got []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	require.Len(t, got, 2)
	require.Equal(t, "VLAN2000", got[0]["name"])
}

func TestNetworkVlanListEmpty(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, noVlans)

	stdout, stderr, err := run(t, "network", "vlan", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "IDENTIFIER   NAME   STATUS   LOCATION\n", stdout)
	require.Equal(t, "no vlans found\n", stderr)
}

func TestNetworkVlanGetTable(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK,
		`{"identifier":"v-1","name":"VLAN2000","status":"Active","locations":[{"identifier":"l-1","code":"ANX04"}]}`)

	stdout, _, err := run(t, "network", "vlan", "get", "v-1", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/vlan/v1/vlan.json/v-1", last.path)
	require.Equal(t,
		"IDENTIFIER   NAME       STATUS   LOCATION\n"+
			"v-1          VLAN2000   Active   ANX04\n",
		stdout)
}

// The Engine takes one location on create and go-anxcloud's body hook flattens
// it to a "location" string, so the CLI sends exactly what its fixtures show.
func TestNetworkVlanCreateSendsASingleLocation(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, oneVlan)

	stdout, _, err := run(t, "network", "vlan", "create",
		"--location", "l-1", "--description", "office", "--vm-provisioning",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, last.method)
	require.Equal(t, "/api/vlan/v1/vlan.json", last.path)

	// The whole body is pinned: no "locations" array, and nothing the Engine
	// assigns itself (identifier, name, status) is sent along.
	var sent map[string]any
	require.NoError(t, json.Unmarshal([]byte(last.body), &sent))
	require.Equal(t, map[string]any{
		"location":             "l-1",
		"description_customer": "office",
		"vm_provisioning":      true,
	}, sent)

	// The created VLAN is rendered from the Engine's response with the same
	// columns as get, so a user sees the Engine-assigned name and status.
	require.Equal(t,
		"IDENTIFIER   NAME       STATUS   LOCATION\n"+
			"v-1          VLAN2000   Active   ANX04\n",
		stdout)
}

// -o json on create prints the Engine object as returned, not the column
// projection, so scripts can read the identifier the Engine assigned.
func TestNetworkVlanCreateJSON(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, oneVlan)

	stdout, _, err := run(t, "network", "vlan", "create", "-o", "json",
		"--location", "l-1", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)

	// Leaving --vm-provisioning off means the VLAN is created without it, and
	// the Engine has to be told so explicitly rather than by omission.
	var sent map[string]any
	require.NoError(t, json.Unmarshal([]byte(last.body), &sent))
	require.Equal(t, false, sent["vm_provisioning"])

	var vlan map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &vlan))
	require.Equal(t, "v-1", vlan["identifier"])
	require.Equal(t, "VLAN2000", vlan["name"])
	require.Equal(t, true, vlan["vm_provisioning"])
}

func TestNetworkVlanCreateRequiresALocation(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, oneVlan)

	_, _, err := run(t, "network", "vlan", "create", "--description", "office",
		"--token", "tok", "--api-base-url", srv.URL)
	require.Error(t, err)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.Contains(t, errmap.Message(err), "--location is required")
	require.Empty(t, last.method, "a usage error must not reach the Engine")
}

// The location cannot be changed after creation, so update does not offer it.
func TestNetworkVlanUpdateHasNoLocationFlag(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "network", "vlan", "update", "--help")
	require.NoError(t, err)
	require.NotContains(t, stdout, "--location")
	require.Contains(t, stdout, "--description")
	require.Contains(t, stdout, "--vm-provisioning")
}

func TestNetworkVlanUpdateKeepsUnnamedFields(t *testing.T) {
	isolate(t)

	var sent []request
	// The PUT response differs from the GET so the test can tell which one
	// was rendered: the user has to see the state after the write. The
	// description is not a table column, so the output is checked as JSON.
	srv := recordingServer(t, &sent, oneVlan, strings.Replace(oneVlan, `"office"`, `"lab"`, 1))

	stdout, _, err := run(t, "network", "vlan", "update", "v-1", "--description", "lab", "-o", "json",
		"--token", "tok", "--api-base-url", srv)
	require.NoError(t, err)
	require.Len(t, sent, 2)

	var shown map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &shown))
	require.Equal(t, "lab", shown["description_customer"], "the PUT response must be rendered, not the GET")
	// The columns the write did not carry come back from the Engine and are
	// still shown to the user.
	require.Equal(t, "VLAN2000", shown["name"])
	require.Equal(t, "Active", shown["status"])

	require.Equal(t, http.MethodGet, sent[0].method)
	require.Equal(t, "/api/vlan/v1/vlan.json/v-1", sent[0].path)
	require.Equal(t, http.MethodPut, sent[1].method)
	require.Equal(t, "/api/vlan/v1/vlan.json/v-1", sent[1].path)

	// The Engine's VLAN update accepts exactly description_customer and
	// vm_provisioning (go-anxcloud's legacy UpdateDefinition is the contract).
	// Name, role and status are assigned by the Engine and the location is fixed
	// at creation, so none of them may be echoed back and the whole PUT body is
	// pinned rather than two fields.
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(sent[1].body), &body))
	require.Equal(t, map[string]any{
		"identifier":           "v-1",
		"description_customer": "lab",
		"vm_provisioning":      true,
	}, body)
}

// A user who passes --description "" wants the description gone. go-anxcloud
// drops an empty description from the request body, so the Engine would never
// see the change and the command would report a success that changed nothing.
// The CLI has to refuse as a usage error rather than lie, and nothing may be
// written.
func TestNetworkVlanUpdateRefusesAnEmptyDescription(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, oneVlan, oneVlan)

	_, _, err := run(t, "network", "vlan", "update", "v-1", "--description", "",
		"--token", "tok", "--api-base-url", srv)
	require.Error(t, err)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.Contains(t, errmap.Message(err), "--description")

	for _, r := range sent {
		require.NotEqual(t, http.MethodPut, r.method, "a refused update must not be written")
	}
}

// A failed PUT is reported as the update it was, not as the read before it.
func TestNetworkVlanUpdateReportsAFailedWrite(t *testing.T) {
	isolate(t)

	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"code":500}}`))
			return
		}
		_, _ = w.Write([]byte(oneVlan))
	}))
	t.Cleanup(srv.Close)

	_, _, err := run(t, "network", "vlan", "update", "v-1", "--description", "lab",
		"--token", "tok", "--api-base-url", srv.URL)
	require.Error(t, err)
	require.Equal(t, []string{http.MethodGet, http.MethodPut}, methods, "the read must succeed and the write must be attempted")
	require.Contains(t, errmap.Message(err), `updating vlan "v-1"`)
}

// A bool flag is only "set" when passed, so --vm-provisioning=false must count
// as a change rather than being read as the default and rejected.
func TestNetworkVlanUpdateCanDisableVMProvisioning(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, oneVlan, oneVlan)

	_, _, err := run(t, "network", "vlan", "update", "v-1", "--vm-provisioning=false",
		"--token", "tok", "--api-base-url", srv)
	require.NoError(t, err)
	require.Len(t, sent, 2)

	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(sent[1].body), &body))
	require.Equal(t, false, body["vm_provisioning"])
}

// The mirror image: a VLAN that has provisioning off gets it switched on, and
// the description it already had rides along untouched. Together with the
// disable test this pins that the flag's value is sent, not a constant.
func TestNetworkVlanUpdateCanEnableVMProvisioning(t *testing.T) {
	isolate(t)

	var sent []request
	off := strings.Replace(oneVlan, `"vm_provisioning":true`, `"vm_provisioning":false`, 1)
	require.NotEqual(t, oneVlan, off, "fixture must start with provisioning off")
	srv := recordingServer(t, &sent, off, oneVlan)

	_, _, err := run(t, "network", "vlan", "update", "v-1", "--vm-provisioning",
		"--token", "tok", "--api-base-url", srv)
	require.NoError(t, err)
	require.Len(t, sent, 2)

	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(sent[1].body), &body))
	require.Equal(t, map[string]any{
		"identifier":           "v-1",
		"description_customer": "office",
		"vm_provisioning":      true,
	}, body)
}

func TestNetworkVlanUpdateWithNothingChangedIsRejected(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, oneVlan)

	_, _, err := run(t, "network", "vlan", "update", "v-1", "--token", "tok", "--api-base-url", srv)
	require.Error(t, err)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.Contains(t, errmap.Message(err), "nothing to update")

	for _, r := range sent {
		require.NotEqual(t, http.MethodPut, r.method, "nothing changed, so nothing may be written")
	}
}

func TestNetworkVlanDeleteConfirms(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, `{}`)

	_, stderr, err := runWithInput(t, "y\n", "network", "vlan", "delete", "v-1",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, http.MethodDelete, last.method)
	require.Equal(t, "/api/vlan/v1/vlan.json/v-1", last.path)
	require.Contains(t, stderr, `delete vlan "v-1"`)
	require.Contains(t, stderr, "deleted vlan v-1")
}

func TestNetworkVlanDeleteStopsOnARefusal(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, `{}`)

	_, _, err := runWithInput(t, "n\n", "network", "vlan", "delete", "v-1",
		"--token", "tok", "--api-base-url", srv)
	require.Error(t, err)
	require.Equal(t, errmap.ExitCanceled, errmap.ExitCode(err))
	require.Empty(t, sent, "a refused delete must not reach the Engine")
}

func TestNetworkVlansPluralAlias(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, twoVlans)

	stdout, _, err := run(t, "network", "vlans", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Contains(t, stdout, "VLAN2000")
}

func TestNetworkPrefixListTable(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, twoPrefixes)

	stdout, stderr, err := run(t, "network", "prefix", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Equal(t, "/api/ipam/v1/prefix.json", last.path)
	require.Contains(t, last.query, "page=1")
	require.Contains(t, last.query, "limit=50")
	require.Equal(t,
		"IDENTIFIER   NAME          DESCRIPTION\n"+
			"p-1          10.0.0.0/24   office\n"+
			"p-2          10.0.1.0/24   lab\n",
		stdout)
}

// The prefix client query-escapes the search term itself, so the CLI must pass
// it through untouched or the escaping ends up doubled on the wire.
func TestNetworkPrefixListSearchIsEscapedExactlyOnce(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, twoPrefixes)

	_, _, err := run(t, "network", "prefix", "list",
		"--search", "a b&c", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Contains(t, last.query, "search=a+b%26c")
	require.NotContains(t, last.query, "%2526")
}

func TestNetworkPrefixListEmpty(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, noPrefixes)

	stdout, stderr, err := run(t, "network", "prefix", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "IDENTIFIER   NAME   DESCRIPTION\n", stdout)
	require.Equal(t, "no prefixes found\n", stderr)
}

func TestNetworkPrefixListPaging(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, twoPrefixes)

	_, _, err := run(t, "network", "prefix", "list",
		"--page", "3", "--limit", "7", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	requirePaging(t, last.query, "3", "7")
}

func TestNetworkPrefixGetTable(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK,
		`{"identifier":"p-1","name":"10.0.0.0/24","version":4,"netmask":24,"status":"Active",
		  "locations":[{"identifier":"l-1","code":"ANX04"}]}`)

	stdout, _, err := run(t, "network", "prefix", "get", "p-1", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/ipam/v1/prefix.json/p-1", last.path)
	require.Equal(t,
		"IDENTIFIER   NAME          VERSION   STATUS\n"+
			"p-1          10.0.0.0/24   4         Active\n",
		stdout)
}

func TestNetworkPrefixGetJSONKeepsFullObject(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK,
		`{"identifier":"p-1","name":"10.0.0.0/24","version":4,"netmask":24,"status":"Active",
		  "description_internal":"internal note","router_redundancy":true,"locations":[]}`)

	stdout, _, err := run(t, "network", "prefix", "get", "p-1", "-o", "json", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	require.Equal(t, "internal note", got["description_internal"])
	require.Equal(t, true, got["router_redundancy"])
}

const onePrefix = `{"identifier":"p-1","name":"10.0.0.0/24","description_customer":"office"}`

// The Engine's prefix create takes the location, the IP version, the prefix
// type and the netmask; go-anxcloud's legacy Create struct is the contract, so
// the whole body is pinned and nothing the Engine assigns (identifier, name)
// rides along.
func TestNetworkPrefixCreateSendsTheLegacyCreateBody(t *testing.T) {
	isolate(t)

	var seen []request
	srv := recordingServer(t, &seen, onePrefix)

	stdout, _, err := run(t, "network", "prefix", "create",
		"--location", "l-1", "--version", "4", "--netmask", "24", "--type", "private",
		"--vlan", "v-1", "--description", "office", "--vm-provisioning",
		"--token", "tok", "--api-base-url", srv)
	require.NoError(t, err)
	require.Len(t, seen, 1, "create is one POST, nothing is read before or after")
	last := seen[0]
	require.Equal(t, http.MethodPost, last.method)
	require.Equal(t, "/api/ipam/v1/prefix.json", last.path)

	var sent map[string]any
	require.NoError(t, json.Unmarshal([]byte(last.body), &sent))
	require.Equal(t, map[string]any{
		"location":             "l-1",
		"version":              float64(4),
		"type":                 float64(1),
		"netmask":              float64(24),
		"vlan":                 "v-1",
		"create_empty":         false,
		"description_customer": "office",
		"vm_provisioning":      true,
	}, sent)

	// The created prefix is rendered with the same columns as list, because
	// the Engine answers create with the list summary, not the full object.
	require.Equal(t,
		"IDENTIFIER   NAME          DESCRIPTION\n"+
			"p-1          10.0.0.0/24   office\n",
		stdout)
}

// A public prefix is type 0 on the wire and a new VLAN is requested with a
// flag instead of an identifier; both spellings are the user's, the numbers
// are the Engine's.
func TestNetworkPrefixCreatePublicWithANewVlan(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, onePrefix)

	// /128 is the longest IPv6 prefix there is and must still be accepted;
	// the IPv4 counterpart /32 is covered below.
	stdout, _, err := run(t, "network", "prefix", "create", "-o", "json",
		"--location", "l-1", "--version", "6", "--netmask", "128", "--type", "public",
		"--new-vlan", "--vlan-description", "new office vlan", "--create-empty",
		"--router-redundancy", "--organization", "o-1",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)

	var sent map[string]any
	require.NoError(t, json.Unmarshal([]byte(last.body), &sent))
	require.Equal(t, map[string]any{
		"location":                  "l-1",
		"version":                   float64(6),
		"type":                      float64(0),
		"netmask":                   float64(128),
		"new_vlan":                  true,
		"create_empty":              true,
		"router_redundancy":         true,
		"description_vlan_customer": "new office vlan",
		"organization":              "o-1",
	}, sent)

	// -o json prints the Engine's object, whole, so scripts see every field
	// the Engine answered with and not a projection of it.
	require.JSONEq(t, onePrefix, stdout)
}

// Both ends of the netmask range are inclusive: /0 is the whole address space
// and /32 a single IPv4 address, and each goes through as typed.
func TestNetworkPrefixCreateAcceptsBothNetmaskBounds(t *testing.T) {
	for _, netmask := range []string{"0", "32"} {
		t.Run("netmask "+netmask, func(t *testing.T) {
			isolate(t)
			srv, last := server(t, http.StatusOK, onePrefix)

			_, _, err := run(t, "network", "prefix", "create",
				"--location", "l-1", "--version", "4", "--netmask", netmask, "--type", "private",
				"--token", "tok", "--api-base-url", srv.URL)
			require.NoError(t, err)
			require.Equal(t, http.MethodPost, last.method)

			var body map[string]any
			require.NoError(t, json.Unmarshal([]byte(last.body), &body))
			want, err := strconv.Atoi(netmask)
			require.NoError(t, err)
			require.EqualValues(t, want, body["netmask"])
		})
	}
}

// Every rejected flag combination is rejected before anything is sent.
func TestNetworkPrefixCreateRejectsBadFlags(t *testing.T) {
	base := []string{"--location", "l-1", "--version", "4", "--netmask", "24", "--type", "private", "--vlan", "v-1"}
	without := func(flags ...string) []string {
		out := make([]string, 0, len(base))
		for i := 0; i < len(base); i += 2 {
			if !slices.Contains(flags, base[i]) {
				out = append(out, base[i], base[i+1])
			}
		}
		return out
	}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing location", args: without("--location"), want: "--location is required"},
		{name: "missing version", args: without("--version"), want: "--version is required"},
		{name: "missing netmask", args: without("--netmask"), want: "--netmask is required"},
		{name: "missing type", args: without("--type"), want: "--type is required"},
		{name: "unknown version", args: append(without("--version"), "--version", "5"), want: "--version 5 must be 4 or 6"},
		{name: "unknown type", args: append(without("--type"), "--type", "shared"), want: `--type "shared" must be public or private`},
		{name: "netmask below zero", args: append(without("--netmask"), "--netmask", "-1"), want: "--netmask"},
		// A netmask longer than the address has no meaning, and the bound
		// follows the version: 33 is fine for IPv6 and wrong for IPv4.
		{name: "netmask past ipv4", args: append(without("--netmask"), "--netmask", "33"), want: "--netmask 33 must be between 0 and 32 for IPv4"},
		{name: "netmask past ipv6", args: append(without("--version", "--netmask"), "--version", "6", "--netmask", "129"), want: "--netmask 129 must be between 0 and 128 for IPv6"},
		{name: "both an existing and a new vlan", args: append(slices.Clone(base), "--new-vlan"), want: "--vlan and --new-vlan"},
		// The VLAN identifier rides in the body, but a value with a path
		// separator cannot name a VLAN and is refused like any identifier.
		{name: "vlan with a slash", args: append(without("--vlan"), "--vlan", "v/1"), want: `vlan "v/1" does not name a vlan`},
		// A description for the new VLAN without asking for a new VLAN has
		// nothing to describe; the Engine would ignore or reject it.
		{name: "vlan description without a new vlan", args: append(slices.Clone(base), "--vlan-description", "lab"), want: "--vlan-description requires --new-vlan"},
		// Naming the flag is the mistake, not the value: an empty description
		// is still a description for a VLAN that will not exist.
		{name: "empty vlan description without a new vlan", args: append(slices.Clone(base), "--vlan-description", ""), want: "--vlan-description requires --new-vlan"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			srv, last := server(t, http.StatusOK, onePrefix)

			args := append([]string{"network", "prefix", "create"}, tt.args...)
			args = append(args, "--token", "tok", "--api-base-url", srv.URL)
			_, _, err := run(t, args...)
			require.Error(t, err)
			require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
			require.Contains(t, errmap.Message(err), tt.want)
			require.Empty(t, last.method, "a usage error must not reach the Engine")
		})
	}
}

// The name is the CIDR the Engine assigns and the location, version, type and
// netmask are fixed at creation, so update offers exactly the description.
func TestNetworkPrefixUpdateOffersOnlyTheDescription(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "network", "prefix", "update", "--help")
	require.NoError(t, err)

	// The local flag block is everything between "Flags:" and "Global Flags:",
	// so any create-only flag leaking into update shows up here by name.
	_, after, found := strings.Cut(stdout, "Flags:\n")
	require.True(t, found)
	local, _, _ := strings.Cut(after, "Global Flags:")
	var names []string
	for _, m := range regexp.MustCompile(`(?m)^\s+(?:-\w, )?--([\w-]+)`).FindAllStringSubmatch(local, -1) {
		names = append(names, m[1])
	}
	require.Equal(t, []string{"description", "help"}, names)
}

// go-anxcloud's legacy prefix Update drops every empty field from the body, so
// the Engine only ever sees the fields the user named and keeps the rest. That
// is the same promise the registry's read-then-write update makes, without a
// read: the PUT body is pinned to exactly the one field.
func TestNetworkPrefixUpdateSendsOnlyTheNamedField(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, strings.Replace(onePrefix, `"office"`, `"lab"`, 1))

	stdout, _, err := run(t, "network", "prefix", "update", "p-1", "--description", "lab",
		"--token", "tok", "--api-base-url", srv)
	require.NoError(t, err)
	require.Len(t, sent, 1)
	require.Equal(t, http.MethodPut, sent[0].method)
	require.Equal(t, "/api/ipam/v1/prefix.json/p-1", sent[0].path)

	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(sent[0].body), &body))
	require.Equal(t, map[string]any{"description_customer": "lab"}, body)

	// The Engine's answer is rendered, so the user sees the state after the
	// write and not what they typed.
	require.Equal(t,
		"IDENTIFIER   NAME          DESCRIPTION\n"+
			"p-1          10.0.0.0/24   lab\n",
		stdout)
}

// A user who passes --description "" wants the description gone. go-anxcloud
// drops an empty description from the request body, so the Engine would never
// see the change and the command would report a success that changed nothing.
func TestNetworkPrefixUpdateRefusesAnEmptyDescription(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, onePrefix)

	_, _, err := run(t, "network", "prefix", "update", "p-1", "--description", "",
		"--token", "tok", "--api-base-url", srv)
	require.Error(t, err)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.Contains(t, errmap.Message(err), "--description")
	require.Empty(t, sent, "a refused update must not be written")
}

func TestNetworkPrefixUpdateWithNothingChangedIsRejected(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, onePrefix)

	_, _, err := run(t, "network", "prefix", "update", "p-1", "--token", "tok", "--api-base-url", srv)
	require.Error(t, err)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.Contains(t, errmap.Message(err), "nothing to update")
	require.Empty(t, sent, "nothing changed, so nothing may be written")
}

func TestNetworkPrefixUpdateReportsAFailedWrite(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusInternalServerError, `{"error":{"code":500}}`)

	_, _, err := run(t, "network", "prefix", "update", "p-1", "--description", "lab",
		"--token", "tok", "--api-base-url", srv.URL)
	require.Error(t, err)
	require.Contains(t, errmap.Message(err), `updating prefix "p-1"`)
}

// The write verbs put the identifier in the URL path the same way get does, so
// they share get's two guarantees: a value that cannot stay in one path segment
// is refused before anything is sent, and one that could end the path early is
// escaped so it addresses the named prefix and leaks nothing into the query.
// On update and delete the alternative is writing to or deleting the wrong
// object.
func TestNetworkPrefixWriteVerbsGuardTheIdentifier(t *testing.T) {
	verbs := []struct {
		name   string
		args   func(id string) []string
		method string
	}{
		{
			name:   "update",
			args:   func(id string) []string { return []string{"network", "prefix", "update", id, "--description", "lab"} },
			method: http.MethodPut,
		},
		{
			name:   "delete",
			args:   func(id string) []string { return []string{"network", "prefix", "delete", id, "--yes"} },
			method: http.MethodDelete,
		},
	}

	for _, verb := range verbs {
		t.Run(verb.name+" refuses an identifier with a slash", func(t *testing.T) {
			isolate(t)

			var sent []request
			srv := recordingServer(t, &sent, onePrefix)

			args := append(verb.args("p/1"), "--token", "tok", "--api-base-url", srv)
			_, _, err := run(t, args...)
			require.Error(t, err)
			require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
			require.Contains(t, errmap.Message(err), `prefix "p/1" does not name a prefix`)
			require.Empty(t, sent, "an identifier that cannot be a path segment must not reach the Engine")
		})

		t.Run(verb.name+" escapes the identifier", func(t *testing.T) {
			isolate(t)

			var sent []request
			srv := recordingServer(t, &sent, onePrefix)

			args := append(verb.args("p 1?x=y"), "--token", "tok", "--api-base-url", srv)
			_, _, err := run(t, args...)
			require.NoError(t, err)
			require.Len(t, sent, 1)
			require.Equal(t, verb.method, sent[0].method)
			require.Equal(t, "/api/ipam/v1/prefix.json/p 1?x=y", sent[0].path)
			require.Empty(t, sent[0].query, "the identifier must not leak into the query string")
		})
	}
}

func TestNetworkPrefixDeleteConfirms(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, `{}`)

	stdout, stderr, err := runWithInput(t, "y\n", "network", "prefix", "delete", "p-1",
		"--token", "tok", "--api-base-url", srv)
	require.NoError(t, err)
	require.Empty(t, stdout, "notes belong on stderr, stdout carries data only")
	require.Len(t, sent, 1, "delete is one DELETE, nothing is read before or after")
	require.Equal(t, http.MethodDelete, sent[0].method)
	require.Equal(t, "/api/ipam/v1/prefix.json/p-1", sent[0].path)
	require.Contains(t, stderr, `delete prefix "p-1"`)
	require.Contains(t, stderr, "deleted prefix p-1")
}

func TestNetworkPrefixDeleteWithYesSkipsThePrompt(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, `{}`)

	_, stderr, err := run(t, "network", "prefix", "destroy", "p-1", "--yes",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, http.MethodDelete, last.method)
	// The prompt ends in "[y/N]"; with --yes no question is asked at all.
	require.NotContains(t, stderr, "[y/N]")
	require.Contains(t, stderr, "deleted prefix p-1")
}

func TestNetworkPrefixDeleteStopsOnARefusal(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, `{}`)

	_, _, err := runWithInput(t, "n\n", "network", "prefix", "delete", "p-1",
		"--token", "tok", "--api-base-url", srv)
	require.Error(t, err)
	require.Equal(t, errmap.ExitCanceled, errmap.ExitCode(err))
	require.Empty(t, sent, "a refused delete must not reach the Engine")
}

func TestNetworkPrefixDeleteReportsTheFailure(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusNotFound, `{"error":{"code":404}}`)

	_, _, err := run(t, "network", "prefix", "delete", "p-1", "--yes",
		"--token", "tok", "--api-base-url", srv.URL)
	require.Error(t, err)
	require.Equal(t, errmap.ExitNotFound, errmap.ExitCode(err))
	require.Contains(t, errmap.Message(err), `deleting prefix "p-1"`)
}

func TestNetworkPrefixesPluralAlias(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, twoPrefixes)

	stdout, _, err := run(t, "network", "prefixes", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Contains(t, stdout, "10.0.0.0/24")
}

func TestNetworkAddressListTable(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, twoAddresses)

	stdout, stderr, err := run(t, "network", "address", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Equal(t, "/api/ipam/v1/address.json", last.path)
	require.Equal(t,
		"IDENTIFIER   NAME       ROLE      DESCRIPTION\n"+
			"a-1          10.0.0.1   Default   gateway\n"+
			"a-2          10.0.0.2   Default   host\n",
		stdout)
}

// Field filters are served by a different endpoint than free-text search, so
// setting one has to switch the request rather than append a parameter.
func TestNetworkAddressListWithFiltersUsesFilteredEndpoint(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, twoAddresses)

	_, _, err := run(t, "network", "address", "list",
		"--prefix", "p-1", "--vlan", "v-1", "--version", "4",
		"--role", "Default", "--status", "Active", "--location", "l-1",
		"--organization", "o-1",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/ipam/v1/address/filtered.json", last.path)
	require.Contains(t, last.query, "prefix=p-1")
	require.Contains(t, last.query, "vlan=v-1")
	require.Contains(t, last.query, "version=4")
	require.Contains(t, last.query, "role_text=Default")
	require.Contains(t, last.query, "status=Active")
	require.Contains(t, last.query, "location=l-1")
	require.Contains(t, last.query, "organization_identifier=o-1")
}

func TestNetworkAddressListUnsetFiltersAreNotSent(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, twoAddresses)

	_, _, err := run(t, "network", "address", "list",
		"--prefix", "p-1", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Contains(t, last.query, "prefix=p-1")
	require.NotContains(t, last.query, "version=")
	require.NotContains(t, last.query, "role_text=")
	require.NotContains(t, last.query, "vlan=")
}

// A rejected filter combination has to be rejected before anything is sent.
// Each case points at a fake server and asserts it was never called, so a
// regression that lets the value through cannot pass by reaching some other
// endpoint and failing there for an unrelated reason.
func TestNetworkAddressListRejectsBadFilters(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "search cannot be combined with a field filter",
			args: []string{"--search", "10.0.0.1", "--prefix", "p-1"},
			want: "--search",
		},
		{
			name: "an unknown IP version",
			args: []string{"--version", "5"},
			want: "--version",
		},
		{
			// Version 0 is as wrong as version 5: there is no IP version 0.
			// Reading it as "unset" would quietly widen the request to every
			// address instead of saying the value cannot be served.
			name: "an explicitly passed version zero",
			args: []string{"--version", "0"},
			want: "--version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			srv, last := server(t, http.StatusOK, twoAddresses)

			args := append([]string{"network", "address", "list"}, tt.args...)
			args = append(args, "--token", "tok", "--api-base-url", srv.URL)

			_, _, err := run(t, args...)
			require.Error(t, err)
			require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
			require.ErrorContains(t, err, tt.want)
			require.Empty(t, last.path, "the request must not reach the Engine at all")
		})
	}
}

// The address client query-escapes its search term itself, exactly like the
// prefix one, so escaping here too would double it on the wire.
func TestNetworkAddressListSearchIsEscapedExactlyOnce(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, twoAddresses)

	_, _, err := run(t, "network", "address", "list",
		"--search", "a b&c", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Contains(t, last.query, "search=a+b%26c")
	require.NotContains(t, last.query, "%2526")
}

func TestNetworkAddressListEmpty(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, noAddresses)

	stdout, stderr, err := run(t, "network", "address", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "IDENTIFIER   NAME   ROLE   DESCRIPTION\n", stdout)
	require.Equal(t, "no addresses found\n", stderr)
}

func TestNetworkAddressGetTable(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK,
		`{"identifier":"a-1","name":"10.0.0.1","version":4,"status":"Active","vlan":"v-1","prefix":"p-1"}`)

	stdout, _, err := run(t, "network", "address", "get", "a-1", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/ipam/v1/address.json/a-1", last.path)
	require.Equal(t,
		"IDENTIFIER   NAME       VERSION   STATUS\n"+
			"a-1          10.0.0.1   4         Active\n",
		stdout)
}

func TestNetworkAddressGetTSV(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK,
		`{"identifier":"a-1","name":"10.0.0.1","version":4,"status":"Active","vlan":"v-1","prefix":"p-1"}`)

	stdout, _, err := run(t, "network", "address", "get", "a-1", "-o", "tsv", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t,
		"identifier\tname\tversion\tstatus\n"+
			"a-1\t10.0.0.1\t4\tActive\n",
		stdout)
}

// A field the Engine omits decodes as a zero value. For the IP version that
// zero is not a real version, so showing it as "0" states something false.
// Both nouns render the same field and have to agree on what absent looks
// like.
func TestNetworkGetRendersAnAbsentVersionAsBlank(t *testing.T) {
	tests := []struct {
		name string
		args []string
		body string
		want string
	}{
		{
			name: "prefix",
			args: []string{"network", "prefix", "get", "p-1"},
			body: `{"identifier":"p-1","name":"10.0.0.0/24","status":"Active"}`,
			want: "identifier\tname\tversion\tstatus\n" +
				"p-1\t10.0.0.0/24\t\tActive\n",
		},
		{
			name: "address",
			args: []string{"network", "address", "get", "a-1"},
			body: `{"identifier":"a-1","name":"10.0.0.1","status":"Active"}`,
			want: "identifier\tname\tversion\tstatus\n" +
				"a-1\t10.0.0.1\t\tActive\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			srv, _ := server(t, http.StatusOK, tt.body)

			args := append(slices.Clone(tt.args), "-o", "tsv", "--token", "tok", "--api-base-url", srv.URL)

			stdout, _, err := run(t, args...)
			require.NoError(t, err)
			require.Equal(t, tt.want, stdout)
		})
	}
}

// Identifiers reach the legacy clients through a URL path those clients build
// without escaping, so a character that ends the path early would address a
// different object and report success. core tag pins the same property.
func TestNetworkGetEscapesTheIdentifier(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "prefix",
			args: []string{"network", "prefix", "get", "p 1?x=y"},
			want: "/api/ipam/v1/prefix.json/p 1?x=y",
		},
		{
			name: "address",
			args: []string{"network", "address", "get", "a 1?x=y"},
			want: "/api/ipam/v1/address.json/a 1?x=y",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			srv, last := server(t, http.StatusOK, `{"identifier":"x"}`)

			args := append(slices.Clone(tt.args), "--token", "tok", "--api-base-url", srv.URL)

			_, _, err := run(t, args...)
			require.NoError(t, err)
			require.Equal(t, tt.want, last.path)
			require.Empty(t, last.query, "the identifier must not leak into the query string")
		})
	}
}

// The paging flags have to reach the address endpoints. Only --all is
// exercised elsewhere, and it always starts at page one, so an explicit --page
// or --limit could stop being forwarded without any test noticing.
func TestNetworkAddressListPaging(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, twoAddresses)

	_, _, err := run(t, "network", "address", "list",
		"--page", "3", "--limit", "7", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	requirePaging(t, last.query, "3", "7")
}

// Both hand-written nouns have to call ValidatePaging before they build a
// request. Dropping that call is invisible in the happy path: the value simply
// travels to the Engine and comes back as whatever the Engine makes of it, so
// each case asserts the server was never reached.
func TestNetworkListRejectsOutOfRangePaging(t *testing.T) {
	nouns := []struct {
		noun string
		body string
	}{
		{noun: "prefix", body: twoPrefixes},
		{noun: "address", body: twoAddresses},
	}

	pagings := []struct {
		name string
		args []string
		want string
	}{
		{name: "page below one", args: []string{"--page", "0"}, want: "--page"},
		{name: "limit below one", args: []string{"--limit", "0"}, want: "--limit"},
		{name: "limit above the maximum", args: []string{"--limit", "1001"}, want: "--limit"},
		{
			name: "a page too large to walk from",
			args: []string{"--all", "--page", strconv.Itoa(math.MaxInt)},
			want: "--page",
		},
	}

	for _, n := range nouns {
		for _, p := range pagings {
			t.Run(n.noun+" "+p.name, func(t *testing.T) {
				isolate(t)
				srv, last := server(t, http.StatusOK, n.body)

				args := append([]string{"network", n.noun, "list"}, p.args...)
				args = append(args, "--token", "tok", "--api-base-url", srv.URL)

				_, _, err := run(t, args...)
				require.Error(t, err)
				require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
				require.ErrorContains(t, err, p.want)
				require.Empty(t, last.path, "the request must not reach the Engine at all")
			})
		}
	}
}

// requirePaging pins the paging parameters as parsed values. Substring
// matching would accept page=30 for page=3, and the legacy ipam clients also
// send an always-present empty search=, so the raw query cannot be compared
// whole either.
func requirePaging(t *testing.T, query, page, limit string) {
	t.Helper()

	values, err := url.ParseQuery(query)
	require.NoError(t, err)
	require.Equal(t, []string{page}, values["page"])
	require.Equal(t, []string{limit}, values["limit"])
}

// Structured output renders the decoded object, not the table's column subset.
// Conformance only checks that every format is accepted, so a regression to
// tabular rendering here would otherwise go unseen.
func TestNetworkAddressGetJSONKeepsFullObject(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK,
		`{"identifier":"a-1","name":"10.0.0.1","version":4,"status":"Active",
		  "description_internal":"internal note","rdns_name":"host.example.com"}`)

	stdout, _, err := run(t, "network", "address", "get", "a-1", "-o", "json", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	require.Equal(t, "internal note", got["description_internal"])
	require.Equal(t, "host.example.com", got["rdns_name"])
}

func TestNetworkAddressesPluralAlias(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, twoAddresses)

	stdout, _, err := run(t, "network", "addresses", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Contains(t, stdout, "10.0.0.1")
}

// TestNetworkListAllCarriesFiltersAcrossPages pins that the parameters that
// pick what is listed survive a multi-page walk. Both nouns build their
// request per page, so a filter left out of the second request would silently
// widen the result set halfway through instead of failing.
func TestNetworkListAllCarriesFiltersAcrossPages(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPath string
		wantQry  string
		body     func(page int) string
		empty    string
	}{
		{
			name:     "prefix keeps its search term",
			args:     []string{"network", "prefix", "list", "--search", "10.0."},
			wantPath: "/api/ipam/v1/prefix.json",
			wantQry:  "search=10.0.",
			body: func(page int) string {
				return fmt.Sprintf(`{"data":[{"identifier":"p-%d","name":"10.0.%d.0/24"}]}`, page, page)
			},
			empty: noPrefixes,
		},
		{
			name:     "address keeps its filtered endpoint",
			args:     []string{"network", "address", "list", "--prefix", "p-1"},
			wantPath: "/api/ipam/v1/address/filtered.json",
			wantQry:  "prefix=p-1",
			body: func(page int) string {
				return fmt.Sprintf(`{"data":{"data":[{"identifier":"a-%d","name":"10.0.0.%d"}]}}`, page, page)
			},
			empty: noAddresses,
		},
		{
			// The address noun reaches two endpoints, so its search term has
			// to survive the walk on the unfiltered one as well.
			name:     "address keeps its search term",
			args:     []string{"network", "address", "list", "--search", "10.0."},
			wantPath: "/api/ipam/v1/address.json",
			wantQry:  "search=10.0.",
			body: func(page int) string {
				return fmt.Sprintf(`{"data":{"data":[{"identifier":"a-%d","name":"10.0.0.%d"}]}}`, page, page)
			},
			empty: noAddresses,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)

			const lastPage = 2

			var seen []string

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				page, err := strconv.Atoi(r.URL.Query().Get("page"))
				require.NoError(t, err)

				seen = append(seen, r.URL.Path+"?"+r.URL.RawQuery)

				w.Header().Set("Content-Type", "application/json")

				if page > lastPage {
					_, _ = w.Write([]byte(tt.empty))

					return
				}

				_, _ = w.Write([]byte(tt.body(page)))
			}))
			t.Cleanup(srv.Close)

			args := append(slices.Clone(tt.args), "--all", "--limit", "1", "-o", "json",
				"--token", "tok", "--api-base-url", srv.URL)

			stdout, _, err := run(t, args...)
			require.NoError(t, err)

			var got []struct {
				Identifier string `json:"identifier"`
			}
			require.NoError(t, json.Unmarshal([]byte(stdout), &got))
			require.Len(t, got, lastPage, "the walk must collect every page")

			require.Len(t, seen, lastPage+1, "the walk ends on the first empty page")

			for _, req := range seen {
				require.Contains(t, req, tt.wantPath)
				require.Contains(t, req, tt.wantQry)
			}
		})
	}
}

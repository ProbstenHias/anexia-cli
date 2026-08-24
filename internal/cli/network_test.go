package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
)

const twoVlans = `{"data":{"page":1,"total_pages":1,"total_items":2,"limit":50,"data":[
  {"identifier":"v-1","name":"VLAN2000","status":"Active","locations":[{"identifier":"l-1","code":"ANX04"}]},
  {"identifier":"v-2","name":"VLAN2001","status":"Pending","locations":[]}
]}}`

const noVlans = `{"data":{"page":1,"total_pages":1,"total_items":0,"limit":50,"data":[]}}`

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

func TestNetworkNounsOnlyHaveReadVerbs(t *testing.T) {
	isolate(t)

	for _, noun := range []string{"vlan", "prefix", "address"} {
		stdout, _, err := run(t, "network", noun)
		require.NoError(t, err)
		require.Contains(t, stdout, "list")
		require.Contains(t, stdout, "get")
		for _, absent := range []string{"create", "update", "delete", "destroy", "reserve"} {
			require.NotContains(t, stdout, absent,
				"write verbs wait for the resource registry to grow them")
		}
	}
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
	require.Contains(t, last.query, "page=3")
	require.Contains(t, last.query, "limit=7")
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

func TestNetworkAddressListRejectsSearchWithFilters(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "network", "address", "list",
		"--search", "10.0.0.1", "--prefix", "p-1", "--token", "tok")
	require.Error(t, err)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.ErrorContains(t, err, "--search")
}

func TestNetworkAddressListRejectsUnknownVersion(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "network", "address", "list",
		"--version", "5", "--token", "tok")
	require.Error(t, err)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.ErrorContains(t, err, "--version")
}

// An explicitly passed --version 0 is as wrong as --version 5: there is no IP
// version 0. Reading it as "unset" would quietly widen the request to every
// address instead of telling the user the value cannot be served.
func TestNetworkAddressListRejectsExplicitZeroVersion(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "network", "address", "list",
		"--version", "0", "--token", "tok")
	require.Error(t, err)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.ErrorContains(t, err, "--version")
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
	require.Contains(t, last.query, "page=3")
	require.Contains(t, last.query, "limit=7")
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

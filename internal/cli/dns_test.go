package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
)

const twoZones = `{"results":[
  {"name":"example.com","master":true,"dnssec_mode":"managed","admin_email":"admin@example.com","ttl":3600},
  {"name":"example.org","master":false,"dnssec_mode":"unvalidated","admin_email":"root@example.org","ttl":900}
]}`

const oneZone = `{"name":"example.com","master":true,"dnssec_mode":"managed","admin_email":"admin@example.com","ttl":3600,"refresh":3600,"retry":900,"expire":604800}`

const noZones = `{"results":[]}`

const twoRecords = `[
  {"identifier":"rec-1","name":"www","type":"A","rdata":"10.0.0.1","ttl":3600},
  {"identifier":"rec-2","name":"mail","type":"MX","rdata":"10 mail.example.com","ttl":900}
]`

// createdRecordZone is what the Engine answers a record create with: the whole
// zone, whose current revision holds the record just made. go-anxcloud reads
// the assigned identifier back out of it.
const createdRecordZone = `{"name":"example.com","current_revision":"rev-1","revisions":[
  {"identifier":"rev-1","records":[{"identifier":"rec-9","name":"www","type":"A","rdata":"10.0.0.1","ttl":3600}]}
]}`

// updatedRecordZone is the same shape for an update, carrying the record the
// update targeted rather than a newly created one.
const updatedRecordZone = `{"name":"example.com","current_revision":"rev-1","revisions":[
  {"identifier":"rev-1","records":[{"identifier":"rec-1","name":"www","type":"A","rdata":"10.0.0.9","ttl":3600}]}
]}`

func TestDNSWithoutSubcommandPrintsHelp(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "dns")
	require.NoError(t, err)
	require.Contains(t, stdout, "zone")
	require.Contains(t, stdout, "record")
}

func TestDNSZoneHasEveryVerbIncludingTheDocumentOnes(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "dns", "zone")
	require.NoError(t, err)

	for _, verb := range []string{"list", "get", "create", "update", "delete", "import", "apply"} {
		require.Contains(t, stdout, verb)
	}
}

// TestDNSRecordHasNoGet pins the one verb deliberately missing: go-anxcloud
// answers a record get with ErrOperationNotSupported, and a verb that always
// fails is worse than a missing one.
func TestDNSRecordHasNoGet(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "dns", "record")
	require.NoError(t, err)

	require.Contains(t, stdout, "list")
	require.Contains(t, stdout, "create")
	require.Contains(t, stdout, "update")
	require.Contains(t, stdout, "delete")

	for _, line := range strings.Split(stdout, "\n") {
		require.False(t, strings.HasPrefix(strings.TrimSpace(line), "get "),
			"dns record must not offer get, the Engine has no endpoint for one")
	}
}

func TestDNSZoneListTable(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, twoZones)

	stdout, stderr, err := run(t, "dns", "zone", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Equal(t, "/api/clouddns/v1/zone.json", last.path)
	require.Contains(t, stdout, "NAME")
	require.Contains(t, stdout, "example.com")
	require.Contains(t, stdout, "managed")
	require.Contains(t, stdout, "example.org")
}

func TestDNSZoneListJSON(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, twoZones)

	stdout, _, err := run(t, "dns", "zone", "list", "-o", "json", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)

	var zones []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &zones))
	require.Len(t, zones, 2)
	require.Equal(t, "example.com", zones[0]["name"])
}

func TestDNSZoneListEmptyNotesOnStderr(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, noZones)

	stdout, stderr, err := run(t, "dns", "zone", "list", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Contains(t, stderr, "no zones found")
	require.Contains(t, stdout, "NAME")
}

// TestDNSZoneListRejectsALaterPage is the paging contract for an endpoint
// go-anxcloud marks as not paginating: the page parameter is dropped, so every
// request returns the same full set and a later page cannot be served at all.
// Answering with page one would silently return the wrong thing.
func TestDNSZoneListRejectsALaterPage(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, twoZones)

	_, _, err := run(t, "dns", "zone", "list", "--page", "2", "--token", "tok", "--api-base-url", srv.URL)
	require.Error(t, err)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.Contains(t, errmap.Message(err), "does not support paging")
}

func TestDNSZoneGet(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, oneZone)

	stdout, _, err := run(t, "dns", "zone", "get", "example.com", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/clouddns/v1/zone.json/example.com", last.path)
	require.Contains(t, stdout, "example.com")
	require.Contains(t, stdout, "admin@example.com")
}

func TestDNSZoneGetNotFound(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusNotFound, `{"error":{"code":404,"message":"no such zone"}}`)

	_, _, err := run(t, "dns", "zone", "get", "missing.example", "--token", "tok", "--api-base-url", srv.URL)
	require.Error(t, err)
	require.Equal(t, errmap.ExitNotFound, errmap.ExitCode(err))
	require.Contains(t, errmap.Message(err), `reading zone "missing.example"`)
}

// TestDNSZoneCreateSendsTheSOADefaults checks that a zone created without the
// optional SOA fields carries usable values rather than Go's zeros, which
// would make a zone that refreshes and expires immediately.
func TestDNSZoneCreateSendsTheSOADefaults(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, oneZone)

	_, _, err := run(t, "dns", "zone", "create",
		"--name", "example.com", "--admin-email", "admin@example.com",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, last.method)

	var sent map[string]any
	require.NoError(t, json.Unmarshal([]byte(last.body), &sent))

	// The Engine names the zone "zone_name" on create, which the object's own
	// body hook handles.
	require.Equal(t, "example.com", sent["zone_name"])
	require.Equal(t, "admin@example.com", sent["admin_email"])
	require.InEpsilon(t, 3600.0, sent["refresh"], 0)
	require.InEpsilon(t, 900.0, sent["retry"], 0)
	require.InEpsilon(t, 604800.0, sent["expire"], 0)
	require.InEpsilon(t, 3600.0, sent["ttl"], 0)
	require.Equal(t, true, sent["master"])
	require.Equal(t, "unvalidated", sent["dnssec_mode"])
}

func TestDNSZoneCreateRequiresNameAndAdminEmail(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, oneZone)

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "no name", args: []string{"--admin-email", "a@example.com"}, want: "--name is required"},
		{name: "no email", args: []string{"--name", "example.com"}, want: "--admin-email is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"dns", "zone", "create"}, tc.args...)
			args = append(args, "--token", "tok", "--api-base-url", srv.URL)

			_, _, err := run(t, args...)
			require.Error(t, err)
			require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
			require.Contains(t, errmap.Message(err), tc.want)
		})
	}
}

// TestDNSZoneUpdateHasNoNameFlag pins a deliberate omission. The Engine's zone
// update carries the name only in the request body, with no old name anywhere
// in the request, so a --name would send a different zone_name and nothing in
// the library says whether that renames the zone or writes a different one.
// Renaming is not offered rather than guessed at on someone's DNS.
func TestDNSZoneUpdateHasNoNameFlag(t *testing.T) {
	isolate(t)

	stdout, _, err := run(t, "dns", "zone", "update", "--help")
	require.NoError(t, err)
	require.NotContains(t, stdout, "--name ")
	require.Contains(t, stdout, "--admin-email")
}

// TestDNSZoneUpdateKeepsUnnamedFields is what makes naming only the change
// safe: the command reads the zone first, so a field the user did not mention
// goes back exactly as the Engine had it.
func TestDNSZoneUpdateKeepsUnnamedFields(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, oneZone, oneZone)

	_, _, err := run(t, "dns", "zone", "update", "example.com",
		"--admin-email", "new@example.com", "--token", "tok", "--api-base-url", srv)
	require.NoError(t, err)
	require.Len(t, sent, 2)

	require.Equal(t, http.MethodGet, sent[0].method)
	require.Equal(t, "/api/clouddns/v1/zone.json/example.com", sent[0].path)
	require.Equal(t, http.MethodPut, sent[1].method)

	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(sent[1].body), &body))

	require.Equal(t, "new@example.com", body["admin_email"], "the named field must change")
	require.Equal(t, "managed", body["dnssec_mode"], "an unnamed field must survive the round trip")
	require.InEpsilon(t, 604800.0, body["expire"], 0, "an unnamed field must survive the round trip")
}

func TestDNSZoneUpdateWithNothingChangedIsRejected(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, oneZone)

	_, _, err := run(t, "dns", "zone", "update", "example.com", "--token", "tok", "--api-base-url", srv)
	require.Error(t, err)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.Contains(t, errmap.Message(err), "nothing to update")

	for _, r := range sent {
		require.NotEqual(t, http.MethodPut, r.method, "nothing changed, so nothing may be written")
	}
}

func TestDNSZoneDeleteConfirms(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, oneZone)

	_, stderr, err := runWithInput(t, "y\n", "dns", "zone", "delete", "example.com",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, http.MethodDelete, last.method)
	require.Contains(t, stderr, `delete zone "example.com"`)
	require.Contains(t, stderr, "deleted zone example.com")
}

func TestDNSZoneDeleteStopsOnARefusal(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, oneZone)

	_, _, err := runWithInput(t, "n\n", "dns", "zone", "delete", "example.com",
		"--token", "tok", "--api-base-url", srv)
	require.Error(t, err)
	require.Equal(t, errmap.ExitCanceled, errmap.ExitCode(err))
	require.Empty(t, sent, "a refused delete must not reach the Engine")
}

func TestDNSRecordListAddressesTheZone(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, twoRecords)

	stdout, _, err := run(t, "dns", "record", "list", "--zone", "example.com",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/clouddns/v1/zone.json/example.com/records", last.path)
	require.Contains(t, stdout, "rec-1")
	require.Contains(t, stdout, "10.0.0.1")
	require.Contains(t, stdout, "mail")
}

func TestDNSRecordListFilters(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, twoRecords)

	_, _, err := run(t, "dns", "record", "list", "--zone", "example.com",
		"--name", "www", "--type", "A", "--rdata", "10.0.0.1",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Contains(t, last.query, "name=www")
	require.Contains(t, last.query, "type=A")
	// The Engine spells the record-data filter "data", not "rdata", so the
	// flag is named after the field and the library maps it.
	require.Contains(t, last.query, "data=10.0.0.1")
}

// TestDNSRecordVerbsRequireAZone covers the scope rule: without a zone there is
// no collection to address, so every verb refuses before making a request
// rather than asking the Engine what it makes of an empty one.
func TestDNSRecordVerbsRequireAZone(t *testing.T) {
	isolate(t)

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "list", args: []string{"dns", "record", "list"}},
		{name: "create", args: []string{"dns", "record", "create", "--name", "www", "--type", "A", "--rdata", "10.0.0.1"}},
		{name: "update", args: []string{"dns", "record", "update", "rec-1", "--rdata", "10.0.0.2"}},
		{name: "delete", args: []string{"dns", "record", "delete", "rec-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sent []request
			srv := recordingServer(t, &sent, twoRecords)

			args := append(slices.Clone(tc.args), "--token", "tok", "--api-base-url", srv, "--yes")

			_, _, err := run(t, args...)
			require.Error(t, err)
			require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
			require.Contains(t, errmap.Message(err), "--zone is required")
			require.Empty(t, sent, "%s: a missing zone must not reach the Engine", tc.name)
		})
	}
}

// TestDNSRecordListReportsAMissingZoneBeforeAMissingToken pins the order the
// checks run in. A missing scope is the user's actual mistake; reporting a
// missing token ahead of it sends them after the wrong problem and exits 3
// where the contract calls for 2.
func TestDNSRecordListReportsAMissingZoneBeforeAMissingToken(t *testing.T) {
	isolate(t)

	_, _, err := run(t, "dns", "record", "list")
	require.Error(t, err)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.Contains(t, errmap.Message(err), "--zone is required")
}

// TestDNSRecordEscapesTheZoneName is the reason --zone is escaped by hand even
// though the generic client escapes what it appends itself. go-anxcloud
// interpolates the zone into a path string it then parses, so an unescaped
// question mark ends the path and the rest becomes query parameters, addressing
// a different collection than the user named.
func TestDNSRecordEscapesTheZoneName(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, twoRecords)

	_, _, err := run(t, "dns", "record", "list", "--zone", "ex?ample.com",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, "/api/clouddns/v1/zone.json/ex?ample.com/records", last.path,
		"the whole zone name must stay in one path segment")
}

func TestDNSRecordRejectsAZoneNamingNothing(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, twoRecords)

	_, _, err := run(t, "dns", "record", "list", "--zone", "..", "--token", "tok", "--api-base-url", srv)
	require.Error(t, err)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.Empty(t, sent)
}

func TestDNSRecordCreate(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, createdRecordZone)

	stdout, _, err := run(t, "dns", "record", "create", "--zone", "example.com",
		"--name", "www", "--type", "A", "--rdata", "10.0.0.1", "--ttl", "3600",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, last.method)
	require.Equal(t, "/api/clouddns/v1/zone.json/example.com/records", last.path)

	var sent map[string]any
	require.NoError(t, json.Unmarshal([]byte(last.body), &sent))
	require.Equal(t, "www", sent["name"])
	require.Equal(t, "A", sent["type"])
	require.Equal(t, "10.0.0.1", sent["rdata"])

	// The identifier the Engine assigned comes back out of the zone it
	// answered with, so the user sees the record they just made.
	require.Contains(t, stdout, "rec-9")
}

// TestDNSRecordCreateSendsADefaultTTL guards a trap in how a create is read
// back. go-anxcloud finds the new record in the zone the Engine answers with by
// matching name, type, rdata and TTL. Sending no TTL lets the Engine fill in
// the zone's default, which then fails to match what was sent, and a create
// that actually worked is reported as a record that cannot be found.
func TestDNSRecordCreateSendsADefaultTTL(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, createdRecordZone)

	_, _, err := run(t, "dns", "record", "create", "--zone", "example.com",
		"--name", "www", "--type", "A", "--rdata", "10.0.0.1",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)

	var sent map[string]any
	require.NoError(t, json.Unmarshal([]byte(last.body), &sent))
	require.InEpsilon(t, 3600.0, sent["ttl"], 0, "a create with no TTL must still name one")
}

// TestDNSRecordWriteRefusesAZeroTTL is the explicit half of the same trap. The
// Engine reads a zero TTL as "use the zone default" and substitutes it, which
// the client then cannot match against what it sent, so a write that succeeded
// comes back as a record that cannot be found. Refusing keeps the report honest.
func TestDNSRecordWriteRefusesAZeroTTL(t *testing.T) {
	isolate(t)

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "create", args: []string{"dns", "record", "create", "--zone", "example.com", "--name", "www", "--type", "A", "--rdata", "10.0.0.1", "--ttl", "0"}},
		{name: "update", args: []string{"dns", "record", "update", "rec-1", "--zone", "example.com", "--ttl", "0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sent []request
			srv := recordingServer(t, &sent, twoRecords, createdRecordZone)

			args := append(slices.Clone(tc.args), "--token", "tok", "--api-base-url", srv)

			_, _, err := run(t, args...)
			require.Error(t, err)
			require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
			require.Contains(t, errmap.Message(err), "--ttl must be greater than zero")

			for _, r := range sent {
				require.NotEqual(t, http.MethodPost, r.method, "%s: nothing may be written", tc.name)
				require.NotEqual(t, http.MethodPut, r.method, "%s: nothing may be written", tc.name)
			}
		})
	}
}

func TestDNSRecordCreateRequiresItsPayload(t *testing.T) {
	isolate(t)
	srv, _ := server(t, http.StatusOK, createdRecordZone)

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "no name", args: []string{"--type", "A", "--rdata", "10.0.0.1"}, want: "--name is required"},
		{name: "no type", args: []string{"--name", "www", "--rdata", "10.0.0.1"}, want: "--type is required"},
		{name: "no rdata", args: []string{"--name", "www", "--type", "A"}, want: "--rdata is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"dns", "record", "create", "--zone", "example.com"}, tc.args...)
			args = append(args, "--token", "tok", "--api-base-url", srv.URL)

			_, _, err := run(t, args...)
			require.Error(t, err)
			require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
			require.Contains(t, errmap.Message(err), tc.want)
		})
	}
}

// TestDNSRecordUpdateReadsTheZoneFirst is the hand-written half of the record
// commands. go-anxcloud refuses a record get, so the only read available is the
// zone's listing; without it the user would have to restate every field on
// every change.
func TestDNSRecordUpdateReadsTheZoneFirst(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, twoRecords, updatedRecordZone)

	_, _, err := run(t, "dns", "record", "update", "rec-1", "--zone", "example.com",
		"--rdata", "10.0.0.9", "--token", "tok", "--api-base-url", srv)
	require.NoError(t, err)
	require.Len(t, sent, 2)

	require.Equal(t, http.MethodGet, sent[0].method)
	require.Equal(t, "/api/clouddns/v1/zone.json/example.com/records", sent[0].path)

	require.Equal(t, http.MethodPut, sent[1].method)
	require.Equal(t, "/api/clouddns/v1/zone.json/example.com/records/rec-1", sent[1].path)

	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(sent[1].body), &body))

	require.Equal(t, "10.0.0.9", body["rdata"], "the named field must change")
	require.Equal(t, "www", body["name"], "an unnamed field must survive the round trip")
	require.Equal(t, "A", body["type"], "an unnamed field must survive the round trip")
}

// TestDNSRecordUpdateKeepsTheEscapedZone covers the trap in doing an update as
// a list followed by a write. Decoding the list overwrites the record's zone
// with a value read back out of the request URL, and that value arrives
// decoded, so handing it straight to the update rebuilds a different path than
// the one the user named. The validated zone has to go back on in between.
func TestDNSRecordUpdateKeepsTheEscapedZone(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, twoRecords, updatedRecordZone)

	_, _, err := run(t, "dns", "record", "update", "rec-1", "--zone", "ex?ample.com",
		"--rdata", "10.0.0.9", "--token", "tok", "--api-base-url", srv)
	require.NoError(t, err)
	require.Len(t, sent, 2)

	require.Equal(t, "/api/clouddns/v1/zone.json/ex?ample.com/records/rec-1", sent[1].path,
		"the write must address the same zone the read did")
}

// TestDNSRecordUpdateLeavesTXTDataAlone covers the quoting round trip. The
// Engine lists TXT data enclosed in quotes and wants it back without them,
// because go-anxcloud adds a pair of its own when it looks for the record in
// the response. Passing the listed value straight through writes doubled quotes
// and then reports the record as not found, so an update meaning to change only
// the TTL would corrupt the data and look like it failed.
func TestDNSRecordUpdateLeavesTXTDataAlone(t *testing.T) {
	isolate(t)

	const listed = `[{"identifier":"rec-t","name":"_acme","type":"TXT","rdata":"\"token\"","ttl":3600}]`
	const updated = `{"name":"example.com","current_revision":"rev-1","revisions":[
	  {"identifier":"rev-1","records":[{"identifier":"rec-t","name":"_acme","type":"TXT","rdata":"\"token\"","ttl":900}]}
	]}`

	var sent []request
	srv := recordingServer(t, &sent, listed, updated)

	_, _, err := run(t, "dns", "record", "update", "rec-t", "--zone", "example.com",
		"--ttl", "900", "--token", "tok", "--api-base-url", srv)
	require.NoError(t, err)
	require.Len(t, sent, 2)

	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(sent[1].body), &body))

	require.Equal(t, "token", body["rdata"], "TXT data must go back the way the Engine wants it")
	require.InEpsilon(t, 900.0, body["ttl"], 0)
}

func TestDNSRecordUpdateNotInZoneExitsNotFound(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, twoRecords)

	_, _, err := run(t, "dns", "record", "update", "rec-missing", "--zone", "example.com",
		"--rdata", "10.0.0.9", "--token", "tok", "--api-base-url", srv)
	require.Error(t, err)
	require.Equal(t, errmap.ExitNotFound, errmap.ExitCode(err))
	require.Contains(t, errmap.Message(err), `reading record "rec-missing"`)

	for _, r := range sent {
		require.NotEqual(t, http.MethodPut, r.method, "a record that is not there must not be written")
	}
}

func TestDNSRecordUpdateWithNothingChangedIsRejected(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, twoRecords)

	_, _, err := run(t, "dns", "record", "update", "rec-1", "--zone", "example.com",
		"--token", "tok", "--api-base-url", srv)
	require.Error(t, err)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.Contains(t, errmap.Message(err), "nothing to update")
}

func TestDNSRecordDeleteConfirms(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, `{"identifier":"rec-1","name":"www"}`)

	_, stderr, err := runWithInput(t, "y\n", "dns", "record", "delete", "rec-1",
		"--zone", "example.com", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, http.MethodDelete, last.method)
	require.Equal(t, "/api/clouddns/v1/zone.json/example.com/records/rec-1", last.path)
	require.Contains(t, stderr, "deleted record rec-1")
}

func TestDNSZoneImportSendsTheFile(t *testing.T) {
	isolate(t)
	// The legacy client types a revision identifier as a real UUID, so the
	// fixture has to be one.
	srv, last := server(t, http.StatusOK,
		`{"identifier":"22222222-2222-2222-2222-222222222222","serial":7,"state":"active","records":[]}`)

	path := writeFile(t, "zone.db", "www IN A 10.0.0.1\n")

	stdout, _, err := run(t, "dns", "zone", "import", "example.com", "--file", path, "--yes",
		"--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, last.method)
	require.Equal(t, "/api/clouddns/v1/zone.json/example.com/import", last.path)

	var sent map[string]any
	require.NoError(t, json.Unmarshal([]byte(last.body), &sent))
	require.Equal(t, "www IN A 10.0.0.1\n", sent["zoneData"])

	require.Contains(t, stdout, "22222222-2222-2222-2222-222222222222")
	require.Contains(t, stdout, "active")
}

func TestDNSZoneImportConfirms(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, "{}")

	path := writeFile(t, "zone.db", "www IN A 10.0.0.1\n")

	_, _, err := runWithInput(t, "n\n", "dns", "zone", "import", "example.com", "--file", path,
		"--token", "tok", "--api-base-url", srv)
	require.Error(t, err)
	require.Equal(t, errmap.ExitCanceled, errmap.ExitCode(err))
	require.Empty(t, sent, "a refused import must not reach the Engine")
}

// TestDNSZoneDocumentFromStdinNeedsAssumeYes covers the one place where a
// confirmation and a document collide. Both would read the same stream:
// confirming first eats the document's first line, reading first leaves the
// prompt at EOF. Saying so beats either failure.
func TestDNSZoneDocumentFromStdinNeedsAssumeYes(t *testing.T) {
	isolate(t)

	for _, verb := range []string{"import", "apply"} {
		t.Run(verb, func(t *testing.T) {
			var sent []request
			srv := recordingServer(t, &sent, "{}")

			_, _, err := runWithInput(t, `{"create":[],"delete":[]}`,
				"dns", "zone", verb, "example.com", "--file", "-",
				"--token", "tok", "--api-base-url", srv)
			require.Error(t, err)
			require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
			require.Contains(t, errmap.Message(err), "--yes")
			require.Empty(t, sent)
		})
	}
}

func TestDNSZoneApplyReadsStdinWithAssumeYes(t *testing.T) {
	isolate(t)
	srv, last := server(t, http.StatusOK, `[{"identifier":"11111111-1111-1111-1111-111111111111","name":"www","Type":"A","rdata":"10.0.0.1","ttl":3600}]`)

	changeset := `{"create":[{"name":"www","type":"A","rdata":"10.0.0.1","region":"","ttl":3600}],"delete":[]}`

	stdout, _, err := runWithInput(t, changeset, "dns", "zone", "apply", "example.com",
		"--file", "-", "--yes", "--token", "tok", "--api-base-url", srv.URL)
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, last.method)
	require.Equal(t, "/api/clouddns/v1/zone.json/example.com/changeset", last.path)
	require.JSONEq(t, changeset, last.body)
	require.Contains(t, stdout, "www")
	require.Contains(t, stdout, "10.0.0.1")
}

func TestDNSZoneApplyRejectsAMalformedChangeset(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, "{}")

	path := writeFile(t, "changeset.json", "not json")

	_, _, err := run(t, "dns", "zone", "apply", "example.com", "--file", path, "--yes",
		"--token", "tok", "--api-base-url", srv)
	require.Error(t, err)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.Contains(t, errmap.Message(err), "reading changeset")
	require.Empty(t, sent)
}

// TestDNSZoneApplyRejectsAnUnknownKey is why the changeset is decoded strictly.
// A misspelled "delete" would otherwise be dropped, the Engine would accept the
// empty changeset it left behind, and the CLI would report success while the
// records the user meant to remove were still there.
func TestDNSZoneApplyRejectsAnUnknownKey(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, "[]")

	path := writeFile(t, "changeset.json", `{"create":[],"delet":[{"name":"www","type":"A","rdata":"10.0.0.1","region":"","ttl":3600}]}`)

	_, _, err := run(t, "dns", "zone", "apply", "example.com", "--file", path, "--yes",
		"--token", "tok", "--api-base-url", srv)
	require.Error(t, err)
	require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	require.Contains(t, errmap.Message(err), "delet")
	require.Empty(t, sent, "a changeset that does not say what the user meant must not be sent")
}

func TestDNSZoneDocumentVerbsRequireAFile(t *testing.T) {
	isolate(t)

	for _, verb := range []string{"import", "apply"} {
		t.Run(verb, func(t *testing.T) {
			var sent []request
			srv := recordingServer(t, &sent, "{}")

			_, _, err := run(t, "dns", "zone", verb, "example.com", "--yes",
				"--token", "tok", "--api-base-url", srv)
			require.Error(t, err)
			require.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
			require.Contains(t, errmap.Message(err), "--file is required")
			require.Empty(t, sent)
		})
	}
}

func TestDNSZoneDocumentVerbsReportAMissingFile(t *testing.T) {
	isolate(t)

	var sent []request
	srv := recordingServer(t, &sent, "{}")

	missing := filepath.Join(t.TempDir(), "absent")

	_, _, err := run(t, "dns", "zone", "import", "example.com", "--file", missing, "--yes",
		"--token", "tok", "--api-base-url", srv)
	require.Error(t, err)
	require.Contains(t, errmap.Message(err), "reading "+missing)
	require.Empty(t, sent, "a file that is not there must not reach the Engine")
}

// writeFile puts content in a temporary file and returns its path.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// recordingServer answers with each response in order, keeping the last one
// once they run out, and appends every request to seen.
//
// The shared server helper keeps only the last request, which cannot show a
// read-then-write pair, and that pair is exactly what the update commands are
// about.
func recordingServer(t *testing.T, seen *[]request, responses ...string) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		*seen = append(*seen, request{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			body:   string(raw),
		})

		i := min(len(*seen), len(responses)) - 1

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responses[i]))
	}))
	t.Cleanup(srv.Close)

	return srv.URL
}

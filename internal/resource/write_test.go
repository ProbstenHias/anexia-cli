package resource_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clouddnsv1 "go.anx.io/go-anxcloud/pkg/apis/clouddns/v1"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/output"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// The write verbs are exercised against clouddnsv1.Zone rather than the
// corev1.Location the read verbs use, because Location is read-only in
// go-anxcloud itself: its EndpointURL answers a create or a delete with
// ErrOperationNotSupported, so a write test built on it would only ever observe
// the library refusing. Zone is the object with the full lifecycle, and running
// against a real one means a change to its hooks or endpoints fails here rather
// than passing against a stand-in.
const zoneEndpoint = "/api/clouddns/v1/zone.json"

// zoneSpec is the "dns zone" spec reduced to what the registry needs to build
// every verb.
func zoneSpec() resource.Spec[clouddnsv1.Zone, *clouddnsv1.Zone] {
	return resource.Spec[clouddnsv1.Zone, *clouddnsv1.Zone]{
		Noun:   "zone",
		Short:  "Work with zones",
		List:   true,
		Get:    true,
		Delete: true,
		Identify: func(z *clouddnsv1.Zone, id string) {
			z.Name = id
		},
		CreatePayload: func(flags *pflag.FlagSet) func(*clouddnsv1.Zone) error {
			name := flags.String("name", "", "zone name")
			email := flags.String("admin-email", "", "admin email")

			return func(z *clouddnsv1.Zone) error {
				if *name == "" {
					return errmap.Usagef("--name is required")
				}

				z.Name = *name
				z.AdminEmail = *email

				return nil
			}
		},
		UpdatePayload: func(flags *pflag.FlagSet) func(*clouddnsv1.Zone) error {
			email := flags.String("admin-email", "", "admin email")

			return func(z *clouddnsv1.Zone) error {
				if !flags.Changed("admin-email") {
					return errmap.Usagef("nothing to update: pass at least one field to change")
				}

				z.AdminEmail = *email

				return nil
			}
		},
		Columns: []resource.Column[clouddnsv1.Zone]{
			{Name: "name", Value: func(z *clouddnsv1.Zone) string { return z.Name }},
			{Name: "admin-email", Value: func(z *clouddnsv1.Zone) string { return z.AdminEmail }},
		},
	}
}

func TestCommandRegistersWriteVerbs(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "{}")
	cmd := resource.Command(e, zoneSpec())

	names := make([]string, 0, len(cmd.Commands()))
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}

	assert.ElementsMatch(t, []string{"list", "get", "create", "update", "delete"}, names)
}

// TestCommandOmitsWriteVerbsWhenUnset is the counterpart: a resource the Engine
// will not let you write must not offer the verbs at all, rather than offering
// them and failing.
func TestCommandOmitsWriteVerbsWhenUnset(t *testing.T) {
	t.Parallel()

	e, _ := serve(t, "{}")

	spec := zoneSpec()
	spec.CreatePayload = nil
	spec.UpdatePayload = nil
	spec.Delete = false

	names := make([]string, 0)
	for _, c := range resource.Command(e, spec).Commands() {
		names = append(names, c.Name())
	}

	assert.ElementsMatch(t, []string{"list", "get"}, names)
}

func TestCreatePostsThePayload(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, `{"name":"example.com","admin_email":"admin@example.com"}`)

	stdout, _, err := exec(resource.Command(e, zoneSpec()),
		"create", "--name", "example.com", "--admin-email", "admin@example.com")

	require.NoError(t, err)
	require.Len(t, *seen, 1)
	assert.Equal(t, http.MethodPost, (*seen)[0].method)
	assert.Equal(t, zoneEndpoint, (*seen)[0].path)

	var sent map[string]any
	require.NoError(t, json.Unmarshal([]byte((*seen)[0].body), &sent))

	// The Engine names a zone "zone_name" on create, which the object's own
	// body hook takes care of. Asserting it here is what proves the registry
	// runs the hook rather than encoding the struct itself.
	assert.Equal(t, "example.com", sent["zone_name"])
	assert.Equal(t, "admin@example.com", sent["admin_email"])
	assert.Contains(t, stdout, "example.com")
}

// TestCreateReportsAMissingRequiredFieldAsUsage checks that the payload hook's
// own validation runs before anything is sent, so a missing field costs no
// request and exits 2 rather than arriving as an Engine rejection.
func TestCreateReportsAMissingRequiredFieldAsUsage(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, "{}")

	_, _, err := exec(resource.Command(e, zoneSpec()), "create")

	require.Error(t, err)
	assert.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	assert.Empty(t, *seen, "a rejected payload must not reach the Engine")
}

// TestCreateNamesTheObjectItFailedOn covers the one verb with no identifier to
// quote: the Engine has not assigned one yet, so the message has to fall back
// to a field the user typed or it says nothing useful at all.
func TestCreateNamesTheObjectItFailedOn(t *testing.T) {
	t.Parallel()

	e := &env{baseURL: notFoundServer(t), format: output.FormatTable}

	_, _, err := exec(resource.Command(e, zoneSpec()), "create", "--name", "example.com")

	require.Error(t, err)
	assert.Equal(t, errmap.ExitNotFound, errmap.ExitCode(err))
	assert.Contains(t, errmap.Message(err), `creating zone "example.com"`)
}

// TestUpdateReadsBeforeWriting pins the documented shape of update: a Get, then
// a PUT carrying the object that came back with the changed field applied. The
// point is the second half: a field the user did not name keeps the value the
// Engine had, which is what makes naming only the change safe.
func TestUpdateReadsBeforeWriting(t *testing.T) {
	t.Parallel()

	e, seen := serve(t,
		`{"name":"example.com","admin_email":"old@example.com","ttl":3600,"dnssec_mode":"managed"}`,
		`{"name":"example.com","admin_email":"new@example.com","ttl":3600,"dnssec_mode":"managed"}`,
	)

	_, _, err := exec(resource.Command(e, zoneSpec()),
		"update", "example.com", "--admin-email", "new@example.com")

	require.NoError(t, err)
	require.Len(t, *seen, 2)

	assert.Equal(t, http.MethodGet, (*seen)[0].method)
	assert.Equal(t, zoneEndpoint+"/example.com", (*seen)[0].path)

	assert.Equal(t, http.MethodPut, (*seen)[1].method)

	var sent map[string]any
	require.NoError(t, json.Unmarshal([]byte((*seen)[1].body), &sent))

	assert.Equal(t, "new@example.com", sent["admin_email"], "the changed field must be replaced")
	assert.InEpsilon(t, 3600.0, sent["ttl"], 0, "an unnamed field must keep what the Engine returned")
	assert.Equal(t, "managed", sent["dnssec_mode"], "an unnamed field must keep what the Engine returned")
}

// TestUpdateWithNothingChangedIsRejected keeps an update that changes nothing
// from being written. The Engine would accept it, and CloudDNS versions a zone's
// contents, so success would mean a revision nobody asked for.
func TestUpdateWithNothingChangedIsRejected(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, `{"name":"example.com","admin_email":"old@example.com"}`)

	_, _, err := exec(resource.Command(e, zoneSpec()), "update", "example.com")

	require.Error(t, err)
	assert.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
	assert.Contains(t, errmap.Message(err), "nothing to update")

	// The read runs first, so a request is expected. What must not happen is
	// the write.
	for _, r := range *seen {
		assert.NotEqual(t, http.MethodPut, r.method, "nothing changed, so nothing may be written")
	}
}

func TestUpdateReportsANotFoundFromTheRead(t *testing.T) {
	t.Parallel()

	e := &env{baseURL: notFoundServer(t), format: output.FormatTable}

	_, _, err := exec(resource.Command(e, zoneSpec()),
		"update", "missing.example", "--admin-email", "new@example.com")

	require.Error(t, err)
	assert.Equal(t, errmap.ExitNotFound, errmap.ExitCode(err))
	assert.Contains(t, errmap.Message(err), `reading zone "missing.example"`)
}

func TestDeleteConfirmsBeforeDestroying(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, "{}")

	cmd := resource.Command(e, zoneSpec())
	cmd.SetIn(strings.NewReader("y\n"))

	_, stderr, err := exec(cmd, "delete", "example.com")

	require.NoError(t, err)
	require.Len(t, *seen, 1)
	assert.Equal(t, http.MethodDelete, (*seen)[0].method)
	assert.Equal(t, zoneEndpoint+"/example.com", (*seen)[0].path)
	assert.Contains(t, stderr, `delete zone "example.com"`)
	assert.Contains(t, stderr, "deleted zone example.com")
}

func TestDeleteStopsOnARefusal(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, "{}")

	cmd := resource.Command(e, zoneSpec())
	cmd.SetIn(strings.NewReader("n\n"))

	_, _, err := exec(cmd, "delete", "example.com")

	require.Error(t, err)
	assert.Equal(t, errmap.ExitCanceled, errmap.ExitCode(err))
	assert.Empty(t, *seen, "a refused delete must not reach the Engine")
}

// TestDeleteSaysWhenThereIsNobodyToAsk separates a refusal from an unattended
// run. Both stop, but a scheduled job needs to be told to pass --yes rather
// than reading that it canceled something it never declined.
func TestDeleteSaysWhenThereIsNobodyToAsk(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, "{}")

	cmd := resource.Command(e, zoneSpec())
	cmd.SetIn(strings.NewReader(""))

	_, _, err := exec(cmd, "delete", "example.com")

	require.Error(t, err)
	assert.Equal(t, errmap.ExitCanceled, errmap.ExitCode(err))
	assert.Contains(t, errmap.Message(err), "--yes")
	assert.Empty(t, *seen)
}

func TestDeleteSkipsThePromptWithAssumeYes(t *testing.T) {
	t.Parallel()

	e, seen := serve(t, "{}")
	e.assumeYes = true

	_, stderr, err := exec(resource.Command(e, zoneSpec()), "delete", "example.com")

	require.NoError(t, err)
	require.Len(t, *seen, 1)
	assert.NotContains(t, stderr, "[y/N]")
}

func TestDeleteReportsANotFound(t *testing.T) {
	t.Parallel()

	e := &env{baseURL: notFoundServer(t), format: output.FormatTable, assumeYes: true}

	_, _, err := exec(resource.Command(e, zoneSpec()), "delete", "missing.example")

	require.Error(t, err)
	assert.Equal(t, errmap.ExitNotFound, errmap.ExitCode(err))
	assert.Contains(t, errmap.Message(err), `deleting zone "missing.example"`)
}

// TestWriteVerbsRefuseAnIdentifierThatNamesNothing covers the verbs that put a
// positional straight into a URL path. A relative segment is normalized away by
// the client, so "delete .." would address the collection itself.
func TestWriteVerbsRefuseAnIdentifierThatNamesNothing(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		verb string
		args []string
	}{
		{verb: "update", args: []string{"update", "..", "--admin-email", "a@example.com"}},
		{verb: "delete", args: []string{"delete", ".."}},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			t.Parallel()

			e, seen := serve(t, "{}")
			e.assumeYes = true

			_, _, err := exec(resource.Command(e, zoneSpec()), tc.args...)

			require.Error(t, err)
			assert.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
			assert.Empty(t, *seen, "%s: a value naming nothing must not reach the Engine", tc.verb)
		})
	}
}

// notFoundServer answers everything with a 404, for the exit-code checks.
func notFoundServer(t *testing.T) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"nope"}}`))
	}))
	t.Cleanup(srv.Close)

	return srv.URL
}

// recordSpec is the "dns record" spec, the resource that actually needs a
// scope: a record is only addressable inside a zone, and go-anxcloud puts that
// zone in the URL path. It carries no get or update, because the library
// answers a record get with ErrOperationNotSupported.
func recordSpec() resource.Spec[clouddnsv1.Record, *clouddnsv1.Record] {
	return resource.Spec[clouddnsv1.Record, *clouddnsv1.Record]{
		Noun:   "record",
		Short:  "Work with records",
		List:   true,
		Delete: true,
		Identify: func(r *clouddnsv1.Record, id string) {
			r.Identifier = id
		},
		Scope: func(flags *pflag.FlagSet) func(*clouddnsv1.Record) error {
			zone := flags.String("zone", "", "zone the record belongs to")

			return func(r *clouddnsv1.Record) error {
				if *zone == "" {
					return errmap.Usagef("--zone is required")
				}

				r.ZoneName = *zone

				return nil
			}
		},
		CreatePayload: func(flags *pflag.FlagSet) func(*clouddnsv1.Record) error {
			name := flags.String("name", "", "record name")

			return func(r *clouddnsv1.Record) error {
				if *name == "" {
					return errmap.Usagef("--name is required")
				}

				r.Name = *name

				return nil
			}
		},
		Columns: []resource.Column[clouddnsv1.Record]{
			{Name: "identifier", Value: func(r *clouddnsv1.Record) string { return r.Identifier }},
			{Name: "name", Value: func(r *clouddnsv1.Record) string { return r.Name }},
		},
	}
}

// createdRecordZone is what the Engine answers a record create with: the whole
// zone, whose current revision contains the record that was just made. That is
// where go-anxcloud reads back the identifier the Engine assigned, so a fixture
// that returned the record alone would never exercise the real path.
const createdRecordZone = `{
	"name": "example.com",
	"current_revision": "rev-1",
	"revisions": [{
		"identifier": "rev-1",
		"records": [{"identifier": "rec-1", "name": "www", "rdata": "", "type": "", "ttl": 0}]
	}]
}`

// TestScopeAppliesToEveryVerb is the rule that matters about a scope: it is not
// a list filter, so a verb that quietly omitted it would address a different
// collection than the user named. Each verb is driven and the scope's effect
// observed on the wire rather than on the object.
func TestScopeAppliesToEveryVerb(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		verb     string
		args     []string
		response string
	}{
		// A list decodes an array where the single-object verbs decode one
		// object, so each verb is answered with the shape it expects.
		{verb: "list", args: []string{"list"}, response: `[{"identifier":"rec-1","name":"www"}]`},
		// A record create answers with the whole zone, and go-anxcloud
		// finds the new record in the zone's current revision to learn the
		// identifier the Engine assigned it.
		{verb: "create", args: []string{"create", "--name", "www"}, response: createdRecordZone},
		{verb: "delete", args: []string{"delete", "rec-1"}, response: `{"identifier":"rec-1","name":"www"}`},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			t.Parallel()

			e, seen := serve(t, tc.response)
			e.assumeYes = true

			_, _, err := exec(resource.Command(e, recordSpec()),
				append(tc.args, "--zone", "example.com")...)

			require.NoError(t, err)
			require.NotEmpty(t, *seen)

			last := (*seen)[len(*seen)-1]
			assert.Contains(t, last.path, "/zone.json/example.com/records",
				"%s: the scope must address the zone the user named", tc.verb)
		})
	}
}

// TestScopeIsRequiredOnEveryVerb is the other half: a missing scope is a usage
// mistake reported before any request, not a request to whatever the Engine
// makes of an empty one.
func TestScopeIsRequiredOnEveryVerb(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		verb string
		args []string
	}{
		{verb: "list", args: []string{"list"}},
		{verb: "create", args: []string{"create", "--name", "www"}},
		{verb: "delete", args: []string{"delete", "rec-1"}},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			t.Parallel()

			e, seen := serve(t, `[]`)
			e.assumeYes = true

			_, _, err := exec(resource.Command(e, recordSpec()), tc.args...)

			require.Error(t, err)
			assert.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
			assert.Contains(t, errmap.Message(err), "--zone is required")
			assert.Empty(t, *seen, "%s: a missing scope must not reach the Engine", tc.verb)
		})
	}
}

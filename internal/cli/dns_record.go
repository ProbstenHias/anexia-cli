package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/api/types"
	clouddnsv1 "go.anx.io/go-anxcloud/pkg/apis/clouddns/v1"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/output"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// newDNSRecordCommand builds "dns record".
//
// A record is a resource rather than a sub-noun of the zone: it has its own
// identifier and its own lifecycle, where a sub-noun is for a collection with
// no identity of its own. So the zone it lives in is a scope flag, the way
// "core tag delete" takes --service, and the positional argument stays the
// thing being addressed.
//
// There is no get: go-anxcloud answers one with api.ErrOperationNotSupported,
// and a verb that always fails is worse than a missing one.
func newDNSRecordCommand(opts *globalOptions) *cobra.Command {
	cmd := resource.Command(opts, resource.Spec[clouddnsv1.Record, *clouddnsv1.Record]{
		Noun:   "record",
		Short:  "Work with CloudDNS records",
		List:   true,
		Delete: true,
		Identify: func(r *clouddnsv1.Record, id string) {
			r.Identifier = id
		},
		Scope:         recordZoneFlag,
		CreatePayload: recordCreateFlags,
		Filters:       recordFilters,
		Columns:       recordColumns(),
	})

	cmd.AddCommand(newDNSRecordUpdateCommand(opts))

	return cmd
}

func recordColumns() []resource.Column[clouddnsv1.Record] {
	return []resource.Column[clouddnsv1.Record]{
		{Name: "identifier", Value: func(r *clouddnsv1.Record) string { return r.Identifier }},
		{Name: "name", Value: func(r *clouddnsv1.Record) string { return r.Name }},
		{Name: "type", Value: func(r *clouddnsv1.Record) string { return r.Type }},
		{Name: "rdata", Value: func(r *clouddnsv1.Record) string { return r.RData }},
		{Name: "ttl", Value: func(r *clouddnsv1.Record) string { return strconv.Itoa(r.TTL) }},
	}
}

// recordZoneFlag declares --zone and writes it into the record, which is what
// makes every record verb addressable at all.
//
// The value is escaped because go-anxcloud interpolates it into a path string
// it then parses, so an unescaped question mark ends the path and turns the
// rest into query parameters, addressing a different collection than the user
// named. That is the same hazard the legacy clients have, in an object the
// generic client otherwise escapes for itself.
func recordZoneFlag(flags *pflag.FlagSet) func(*clouddnsv1.Record) error {
	zone := flags.String("zone", "", "name of the zone the record belongs to")

	return func(r *clouddnsv1.Record) error {
		scope, err := recordZone(*zone)
		if err != nil {
			return err
		}

		r.ZoneName = scope

		return nil
	}
}

// recordZone validates a zone name and escapes it for its own path segment.
func recordZone(zone string) (string, error) {
	if zone == "" {
		return "", errmap.Usagef("--zone is required")
	}

	if err := resource.ValidateIdentifier("zone", zone); err != nil {
		return "", err
	}

	return pathValue(zone), nil
}

// recordFilters declares the three fields the Engine's record list accepts as
// query parameters, which go-anxcloud reads off the same object's fields.
func recordFilters(flags *pflag.FlagSet) func(*clouddnsv1.Record) {
	name := flags.String("name", "", "only list records with this name")
	recordType := flags.String("type", "", "only list records of this type")
	rdata := flags.String("rdata", "", "only list records with this record data")

	return func(r *clouddnsv1.Record) {
		r.Name = *name
		r.Type = *recordType
		r.RData = *rdata
	}
}

// recordFields registers the flags shared by record create and update.
type recordFields struct {
	name       *string
	recordType *string
	rdata      *string
	region     *string
	ttl        *int
	comment    *string
}

func registerRecordFields(flags *pflag.FlagSet) recordFields {
	return recordFields{
		name:       flags.String("name", "", `record name, "@" for the domain root`),
		recordType: flags.String("type", "", "record type, such as A or CNAME"),
		rdata:      flags.String("rdata", "", "record data"),
		region:     flags.String("region", "", "region the record applies to"),
		// Defaulted rather than left at zero because of how a create is
		// read back: go-anxcloud finds the new record in the returned zone
		// by matching name, type, rdata and TTL. Sending no TTL lets the
		// Engine fill in the zone's default, which then does not match what
		// was sent, and a create that worked is reported as a record that
		// cannot be found. Naming the value keeps both ends agreeing.
		ttl:     flags.Int("ttl", defaultTTL, "time to live in seconds"),
		comment: flags.String("comment", "", "comment stored with the record"),
	}
}

func recordCreateFlags(flags *pflag.FlagSet) func(*clouddnsv1.Record) error {
	fields := registerRecordFields(flags)

	return func(r *clouddnsv1.Record) error {
		// go-anxcloud refuses an empty name outright, because the Engine
		// spells the domain root "@" rather than leaving the name blank.
		// Saying so here beats a library error the user cannot act on.
		if *fields.name == "" {
			return errmap.Usagef(`--name is required, use "@" for the domain root`)
		}

		if *fields.recordType == "" {
			return errmap.Usagef("--type is required")
		}

		if *fields.rdata == "" {
			return errmap.Usagef("--rdata is required")
		}

		if err := checkTTL(*fields.ttl); err != nil {
			return err
		}

		r.Name = *fields.name
		r.Type = *fields.recordType
		r.RData = *fields.rdata
		r.Region = *fields.region
		r.TTL = *fields.ttl

		if flags.Changed("comment") {
			r.Comment = fields.comment
		}

		return nil
	}
}

// newDNSRecordUpdateCommand builds "dns record update <id>".
//
// This is the one record verb the registry cannot build, because an update
// there is a Get followed by an Update and go-anxcloud answers a record Get
// with api.ErrOperationNotSupported. Listing the zone and picking the record
// out is the only read available, and without a read the user would have to
// restate every field on every change.
func newDNSRecordUpdateCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a record",
		Args:  cobra.ExactArgs(1),
	}

	flags := cmd.Flags()
	zone := flags.String("zone", "", "name of the zone the record belongs to")
	fields := registerRecordFields(flags)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := resource.ValidateIdentifier("record", args[0]); err != nil {
			return err
		}

		scope, err := recordZone(*zone)
		if err != nil {
			return err
		}

		w, err := opts.Writer(cmd.OutOrStdout())
		if err != nil {
			return err
		}

		a, err := opts.API(cmd.Flags())
		if err != nil {
			return err
		}

		ctx, cancel := opts.Context(cmd.Context())
		defer cancel()

		found, err := findRecord(ctx, a, scope, args[0])
		if err != nil {
			return opts.Fail(fmt.Errorf("reading record %q: %w", args[0], err))
		}

		if err := applyRecordChanges(flags, fields, found); err != nil {
			return err
		}

		if err := a.Update(ctx, found); err != nil {
			return opts.Fail(fmt.Errorf("updating record %q: %w", args[0], err))
		}

		return renderRecord(w, found)
	}

	return cmd
}

// findRecord reads one record out of its zone's listing, which is the only way
// to read a single record: the Engine has no endpoint for one.
//
// The listing is unpaginated, so a single request returns the whole zone.
func findRecord(ctx context.Context, a api.API, scope, identifier string) (*clouddnsv1.Record, error) {
	var info types.PageInfo
	if err := a.List(ctx, &clouddnsv1.Record{ZoneName: scope}, api.Paged(1, resource.MaxLimit, &info)); err != nil {
		return nil, err
	}

	var records []clouddnsv1.Record

	info.Next(&records)

	if err := info.Error(); err != nil {
		return nil, err
	}

	for i := range records {
		if records[i].Identifier != identifier {
			continue
		}

		found := records[i]

		// Decoding a list response overwrites ZoneName with the value read
		// back out of the request URL, which arrives unescaped. Handing
		// that to the update would rebuild a different path than the one
		// the user named, so the validated scope goes back on.
		found.ZoneName = scope

		// The Engine returns TXT data enclosed in quotes and expects it
		// back without them: go-anxcloud adds a pair of its own when it
		// looks for the record in the update's response. Passing the listed
		// value straight through would send doubled quotes, so the record
		// would be written with data the user never asked for and then
		// reported as not found. Undoing the Engine's quoting here is what
		// makes an update that only touches the TTL leave the data alone.
		found.RData = unquoteTXT(found.Type, found.RData)

		return &found, nil
	}

	// Wrapped as a not-found so a missing record exits 4 like every other
	// missing object, rather than as an unclassified failure.
	return nil, fmt.Errorf("%w: no record with that identifier in zone %q", api.ErrNotFound, scope)
}

// checkTTL refuses a TTL of zero on a record write.
//
// The Engine reads zero as "use the zone's default" and substitutes it, but
// go-anxcloud finds the written record in the response by matching the TTL it
// sent. The substituted value never matches, so a write that succeeded is
// reported as a record that cannot be found, leaving the user thinking nothing
// happened when something did. Refusing up front is the only way to keep the
// report honest.
func checkTTL(ttl int) error {
	if ttl <= 0 {
		return errmap.Usagef("--ttl must be greater than zero: the Engine replaces a zero with the zone default, which the client then cannot match")
	}

	return nil
}

// unquoteTXT strips the quoting the Engine puts around TXT record data, for
// the one caller that has to hand a listed record back to an update.
//
// Only TXT is quoted, and only strconv-style quoting is undone: a value that
// does not parse is returned unchanged, because a TXT record can hold anything
// and guessing at it would be worse than leaving it alone.
func unquoteTXT(recordType, rdata string) string {
	if recordType != "TXT" {
		return rdata
	}

	unquoted, err := strconv.Unquote(rdata)
	if err != nil {
		return rdata
	}

	return unquoted
}

// applyRecordChanges writes the flags the user set onto the record read from
// the Engine, leaving everything else as it came back.
func applyRecordChanges(flags *pflag.FlagSet, fields recordFields, r *clouddnsv1.Record) error {
	changed := 0

	for name, apply := range map[string]func(){
		"name":    func() { r.Name = *fields.name },
		"type":    func() { r.Type = *fields.recordType },
		"rdata":   func() { r.RData = *fields.rdata },
		"region":  func() { r.Region = *fields.region },
		"ttl":     func() { r.TTL = *fields.ttl },
		"comment": func() { r.Comment = fields.comment },
	} {
		if flags.Changed(name) {
			apply()

			changed++
		}
	}

	if changed == 0 {
		return errmap.Usagef("nothing to update: pass at least one field to change")
	}

	return checkTTL(r.TTL)
}

// renderRecord writes one record in whichever format was requested.
func renderRecord(w *output.Writer, r *clouddnsv1.Record) error {
	if w.Format().Structured() {
		return w.Object(r)
	}

	columns := recordColumns()

	headers := make([]string, len(columns))
	row := make([]string, len(columns))

	for i, c := range columns {
		headers[i] = c.Name
		row[i] = c.Value(r)
	}

	return w.Table(headers, [][]string{row})
}

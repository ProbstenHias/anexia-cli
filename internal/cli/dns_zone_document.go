package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/spf13/cobra"
	"go.anx.io/go-anxcloud/pkg/clouddns/zone"

	"github.com/ProbstenHias/anexia-cli/internal/confirm"
	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// stdinFile is the --file value meaning "read the document from stdin".
const stdinFile = "-"

// readDocument reads the document a zone operation applies, from a file or
// from stdin.
//
// Both operations take a document rather than a set of fields: a BIND zone file
// for import, a create/delete changeset for apply. Spelling a changeset out as
// repeated flags would need a small language to parse, document and escape,
// where a file hands the Engine's own format straight through.
func readDocument(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "" {
		return nil, errmap.Usagef("--file is required")
	}

	if path == stdinFile {
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, fmt.Errorf("reading standard input: %w", err)
		}

		return data, nil
	}

	data, err := os.ReadFile(path) //nolint:gosec // G304: reading the file the user named is the point of --file
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	return data, nil
}

// confirmDocument asks before an operation that replaces or removes records.
//
// A document read from stdin cannot coexist with a prompt, because both would
// read the same stream: confirm first and the document's first line is taken as
// the answer, read first and the prompt sees EOF and reports an unattended run.
// So --file - requires --yes, and says so rather than failing obscurely.
func confirmDocument(cmd *cobra.Command, opts *globalOptions, path, question string) error {
	if path == stdinFile && !opts.AssumeYes() {
		return errmap.Usagef("--file %s reads the document from standard input, so the confirmation cannot be read from there too: pass --yes", stdinFile)
	}

	return confirm.Prompt(cmd.InOrStdin(), cmd.ErrOrStderr(), question, opts.AssumeYes())
}

// newDNSZoneImportCommand builds "dns zone import <name>", which replaces a
// zone's contents with a BIND zone file. import rather than create or update
// because it is neither: the zone already exists and what arrives is a
// document, not a set of fields.
func newDNSZoneImportCommand(opts *globalOptions) *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "import <name>",
		Short: "Import a zone file into a zone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := resource.ValidateIdentifier("zone", args[0]); err != nil {
				return err
			}

			w, err := opts.Writer(cmd.OutOrStdout())
			if err != nil {
				return err
			}

			data, err := readDocument(cmd, file)
			if err != nil {
				return err
			}

			// Replaces every record in the zone, so it confirms for the
			// same reason delete does.
			if err := confirmDocument(cmd, opts, file, fmt.Sprintf("replace the contents of zone %q", args[0])); err != nil {
				return err
			}

			c, err := opts.Client(cmd.Flags())
			if err != nil {
				return err
			}

			ctx, cancel := opts.Context(cmd.Context())
			defer cancel()

			revision, err := zone.NewAPI(c).Import(ctx, pathValue(args[0]), zone.Import{ZoneData: string(data)})
			if err != nil {
				return opts.Fail(fmt.Errorf("importing zone %q: %w", args[0], err))
			}

			if w.Format().Structured() {
				return w.Object(revision)
			}

			return w.Table([]string{"identifier", "serial", "state", "records"}, [][]string{{
				revision.Identifier.String(),
				strconv.Itoa(revision.Serial),
				revision.State,
				strconv.Itoa(len(revision.Records)),
			}})
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "path to a BIND zone file, or - for standard input")

	return cmd
}

// newDNSZoneApplyCommand builds "dns zone apply <name>", which applies a
// changeset of records to create and records to delete in one request.
func newDNSZoneApplyCommand(opts *globalOptions) *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "apply <name>",
		Short: "Apply a record changeset to a zone",
		Long: `Apply a record changeset to a zone.

The changeset is JSON in the Engine's own shape, with a list of records to
create and a list to delete:

  {
    "create": [{"name": "www", "type": "A", "rdata": "10.0.0.1", "region": "", "ttl": 3600}],
    "delete": [{"name": "old", "type": "A", "rdata": "10.0.0.2", "region": "", "ttl": 3600}]
  }`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := resource.ValidateIdentifier("zone", args[0]); err != nil {
				return err
			}

			w, err := opts.Writer(cmd.OutOrStdout())
			if err != nil {
				return err
			}

			data, err := readDocument(cmd, file)
			if err != nil {
				return err
			}

			// Strict, so a misspelled key is reported rather than dropped.
			// Silently ignoring one turns "delet" into an empty changeset,
			// which the Engine accepts and the CLI reports as success while
			// the records the user meant to remove are still there.
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.DisallowUnknownFields()

			var changeset zone.ChangeSet
			if err := dec.Decode(&changeset); err != nil {
				return errmap.Usagef("reading changeset: %v", err)
			}

			// The delete half removes records, so this confirms even when
			// the changeset only creates: what it contains is the user's
			// claim until the Engine has acted on it.
			if err := confirmDocument(cmd, opts, file, fmt.Sprintf("apply %d creates and %d deletes to zone %q",
				len(changeset.Create), len(changeset.Delete), args[0])); err != nil {
				return err
			}

			c, err := opts.Client(cmd.Flags())
			if err != nil {
				return err
			}

			ctx, cancel := opts.Context(cmd.Context())
			defer cancel()

			applied, err := zone.NewAPI(c).Apply(ctx, pathValue(args[0]), changeset)
			if err != nil {
				return opts.Fail(fmt.Errorf("applying changeset to zone %q: %w", args[0], err))
			}

			return resource.RenderList(cmd, w, "records", applied,
				[]string{"identifier", "name", "type", "rdata", "ttl"},
				func(r *zone.Record) []string {
					ttl := ""
					if r.TTL != nil {
						ttl = strconv.Itoa(*r.TTL)
					}

					return []string{r.Identifier.String(), r.Name, r.Type, r.RData, ttl}
				},
			)
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "path to a JSON changeset, or - for standard input")

	return cmd
}

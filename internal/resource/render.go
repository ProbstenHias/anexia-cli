package resource

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ProbstenHias/anexia-cli/internal/output"
)

// render writes a list result. Structured formats get the raw objects so no
// field is lost; tabular formats get the spec's column projection.
func render[O any, PO Pointer[O]](cmd *cobra.Command, w *output.Writer, spec Spec[O, PO], items []O) error {
	if w.Format().Structured() {
		if items == nil {
			items = []O{}
		}

		return w.Object(items)
	}

	headers, rows := project(spec, items)

	if err := w.Table(headers, rows); err != nil {
		return err
	}

	// The note goes to stderr so piped stdout stays machine-readable.
	if len(items) == 0 {
		_, err := fmt.Fprintf(cmd.ErrOrStderr(), "no %s found\n", spec.plural())

		return err
	}

	return nil
}

// renderOne writes a single-object result.
func renderOne[O any, PO Pointer[O]](w *output.Writer, spec Spec[O, PO], item *O) error {
	if w.Format().Structured() {
		return w.Object(item)
	}

	headers, rows := project(spec, []O{*item})

	return w.Table(headers, rows)
}

// project turns objects into header and row strings using the spec's columns.
func project[O any, PO Pointer[O]](spec Spec[O, PO], items []O) (headers []string, rows [][]string) {
	headers = make([]string, len(spec.Columns))
	for i, c := range spec.Columns {
		headers[i] = c.Name
	}

	rows = make([][]string, 0, len(items))

	for i := range items {
		row := make([]string, len(spec.Columns))
		for j, c := range spec.Columns {
			row[j] = c.Value(&items[i])
		}

		rows = append(rows, row)
	}

	return headers, rows
}

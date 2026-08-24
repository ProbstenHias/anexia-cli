package resource

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ProbstenHias/anexia-cli/internal/output"
)

// render writes a list result. Structured formats get the raw objects so no
// field is lost; tabular formats get the spec's column projection.
func render[O any, PO Pointer[O]](cmd *cobra.Command, w *output.Writer, spec Spec[O, PO], items []O) error {
	headers := make([]string, len(spec.Columns))
	for i, c := range spec.Columns {
		headers[i] = c.Name
	}

	return RenderList(cmd, w, spec.plural(), items, headers, func(item *O) []string {
		row := make([]string, len(spec.Columns))
		for i, c := range spec.Columns {
			row[i] = c.Value(item)
		}

		return row
	})
}

// RenderList writes a list result in whichever format was requested, with the
// empty-result note on stderr so piped stdout stays machine-readable. Commands
// written against the legacy client call this directly, which is what keeps
// their output indistinguishable from a Spec-driven one.
func RenderList[T any](
	cmd *cobra.Command,
	w *output.Writer,
	plural string,
	items []T,
	headers []string,
	row func(*T) []string,
) error {
	if w.Format().Structured() {
		if items == nil {
			items = []T{}
		}

		return w.Object(items)
	}

	rows := make([][]string, 0, len(items))
	for i := range items {
		rows = append(rows, row(&items[i]))
	}

	if err := w.Table(headers, rows); err != nil {
		return err
	}

	if len(items) == 0 {
		_, err := fmt.Fprintf(cmd.ErrOrStderr(), "no %s found\n", plural)

		return err
	}

	return nil
}

// renderOne writes a single-object result.
func renderOne[O any, PO Pointer[O]](w *output.Writer, spec Spec[O, PO], item *O) error {
	if w.Format().Structured() {
		return w.Object(item)
	}

	headers := make([]string, len(spec.Columns))
	row := make([]string, len(spec.Columns))

	for i, c := range spec.Columns {
		headers[i] = c.Name
		row[i] = c.Value(item)
	}

	return w.Table(headers, [][]string{row})
}

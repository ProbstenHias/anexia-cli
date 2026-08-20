// Package output renders command results as aligned tables or as JSON.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Format identifies a supported rendering of command output.
type Format string

// The supported output formats.
const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
)

// ParseFormat converts a user-supplied string into a Format.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatTable:
		return FormatTable, nil
	case FormatJSON:
		return FormatJSON, nil
	default:
		return "", fmt.Errorf("invalid output format %q: must be one of %s, %s", s, FormatTable, FormatJSON)
	}
}

// Writer renders values to an underlying stream in a fixed Format.
type Writer struct {
	out    io.Writer
	format Format
}

// NewWriter returns a Writer that renders to out using format f.
func NewWriter(out io.Writer, f Format) *Writer {
	return &Writer{out: out, format: f}
}

// Format reports the format this Writer renders.
func (w *Writer) Format() Format {
	return w.format
}

// Table writes rows as a borderless, tab-aligned table with uppercased headers.
func (w *Writer) Table(headers []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w.out, 0, 0, 3, ' ', 0)

	upper := make([]string, len(headers))
	for i, h := range headers {
		upper[i] = strings.ToUpper(h)
	}

	if _, err := fmt.Fprintln(tw, strings.Join(upper, "\t")); err != nil {
		return err
	}

	for _, row := range rows {
		if _, err := fmt.Fprintln(tw, strings.Join(row, "\t")); err != nil {
			return err
		}
	}

	return tw.Flush()
}

// JSON writes v as indented JSON followed by a newline.
func (w *Writer) JSON(v any) error {
	enc := json.NewEncoder(w.out)
	enc.SetIndent("", "  ")

	return enc.Encode(v)
}

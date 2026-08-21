// Package output renders command results as aligned tables, JSON, YAML, or TSV.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// Format identifies a supported rendering of command output.
type Format string

// The supported output formats.
const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
	FormatTSV   Format = "tsv"
)

// Formats lists every supported format in help order.
var Formats = []Format{FormatTable, FormatJSON, FormatYAML, FormatTSV}

// FormatNames returns the supported formats as a comma-separated string.
func FormatNames() string {
	names := make([]string, len(Formats))
	for i, f := range Formats {
		names[i] = string(f)
	}

	return strings.Join(names, ", ")
}

// ParseFormat converts a user-supplied string into a Format.
func ParseFormat(s string) (Format, error) {
	for _, f := range Formats {
		if Format(s) == f {
			return f, nil
		}
	}

	return "", fmt.Errorf("invalid output format %q: must be one of %s", s, FormatNames())
}

// Structured reports whether the format renders whole objects rather than a
// column projection, meaning commands should hand it the raw API payload.
func (f Format) Structured() bool {
	return f == FormatJSON || f == FormatYAML
}

// Writer renders values to an underlying stream in a fixed Format.
type Writer struct {
	out       io.Writer
	format    Format
	noHeaders bool
}

// NewWriter returns a Writer that renders to out using format f.
func NewWriter(out io.Writer, f Format) *Writer {
	return &Writer{out: out, format: f}
}

// SetNoHeaders suppresses the header row of tabular formats.
func (w *Writer) SetNoHeaders(v bool) {
	w.noHeaders = v
}

// Format reports the format this Writer renders.
func (w *Writer) Format() Format {
	return w.format
}

// Table writes rows in the writer's tabular format: aligned columns with
// uppercased headers for table, raw tab-separated fields for tsv.
func (w *Writer) Table(headers []string, rows [][]string) error {
	if w.format == FormatTSV {
		return w.tsv(headers, rows)
	}

	tw := tabwriter.NewWriter(w.out, 0, 0, 3, ' ', 0)

	if !w.noHeaders {
		upper := make([]string, len(headers))
		for i, h := range headers {
			upper[i] = strings.ToUpper(h)
		}

		if _, err := fmt.Fprintln(tw, strings.Join(upper, "\t")); err != nil {
			return err
		}
	}

	for _, row := range rows {
		if _, err := fmt.Fprintln(tw, strings.Join(row, "\t")); err != nil {
			return err
		}
	}

	return tw.Flush()
}

// tsv writes unaligned tab-separated fields, suited to cut and awk.
func (w *Writer) tsv(headers []string, rows [][]string) error {
	if !w.noHeaders {
		if _, err := fmt.Fprintln(w.out, strings.Join(headers, "\t")); err != nil {
			return err
		}
	}

	for _, row := range rows {
		if _, err := fmt.Fprintln(w.out, strings.Join(row, "\t")); err != nil {
			return err
		}
	}

	return nil
}

// Object writes v in the writer's structured format. Callers must only use it
// when Format().Structured() reports true.
func (w *Writer) Object(v any) error {
	if w.format == FormatYAML {
		return w.yaml(v)
	}

	return w.JSON(v)
}

// JSON writes v as indented JSON followed by a newline.
func (w *Writer) JSON(v any) error {
	enc := json.NewEncoder(w.out)
	enc.SetIndent("", "  ")

	return enc.Encode(v)
}

// yaml writes v as YAML, routed through JSON so the API structs' json tags
// decide the key names.
func (w *Writer) yaml(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encoding yaml: %w", err)
	}

	var intermediate any
	if err := yaml.Unmarshal(raw, &intermediate); err != nil {
		return fmt.Errorf("encoding yaml: %w", err)
	}

	enc := yaml.NewEncoder(w.out)
	enc.SetIndent(2)

	if err := enc.Encode(intermediate); err != nil {
		return fmt.Errorf("encoding yaml: %w", err)
	}

	return enc.Close()
}

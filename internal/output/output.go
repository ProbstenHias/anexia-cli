// Package output renders command results as aligned tables, JSON, YAML, or TSV.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
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

		if _, err := fmt.Fprintln(tw, flatten(upper)); err != nil {
			return err
		}
	}

	for _, row := range rows {
		if _, err := fmt.Fprintln(tw, flatten(row)); err != nil {
			return err
		}
	}

	return tw.Flush()
}

// tsv writes unaligned tab-separated fields, suited to cut and awk.
func (w *Writer) tsv(headers []string, rows [][]string) error {
	if !w.noHeaders {
		if _, err := fmt.Fprintln(w.out, flatten(headers)); err != nil {
			return err
		}
	}

	for _, row := range rows {
		if _, err := fmt.Fprintln(w.out, flatten(row)); err != nil {
			return err
		}
	}

	return nil
}

// flatten joins fields with tabs after replacing every byte that ends a cell
// or a line with a space. The Engine does not forbid any of them in a name, and
// one arriving unescaped would add columns and rows to the output, silently
// shifting every field after it for tools reading the stream.
//
// The set is tabwriter's, not just the obvious one: it ends a cell on a tab or
// a vertical tab, and a line on a newline or a form feed.
func flatten(fields []string) string {
	clean := make([]string, len(fields))
	for i, f := range fields {
		clean[i] = strings.Map(func(r rune) rune {
			switch r {
			case '\t', '\v', '\n', '\f', '\r':
				return ' '
			default:
				return r
			}
		}, f)
	}

	return strings.Join(clean, "\t")
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

	// UseNumber keeps every number as the digits the Engine sent. Decoding
	// into the default float64 would round anything past 2^53, and object
	// attributes are arbitrary Engine JSON that may carry such values.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var intermediate any
	if err := dec.Decode(&intermediate); err != nil {
		return fmt.Errorf("encoding yaml: %w", err)
	}

	enc := yaml.NewEncoder(w.out)
	enc.SetIndent(2)

	if err := enc.Encode(numbersAsScalars(intermediate)); err != nil {
		return fmt.Errorf("encoding yaml: %w", err)
	}

	return enc.Close()
}

// numbersAsScalars rewrites the json.Number values UseNumber produced into
// something the YAML encoder emits unquoted. yaml.v3 has no json.Number
// knowledge and would render one as a struct, so integers become int64 and
// everything else keeps its original digits behind a plain resolver-safe node.
func numbersAsScalars(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = numbersAsScalars(val)
		}

		return t
	case []any:
		for i, val := range t {
			t[i] = numbersAsScalars(val)
		}

		return t
	case json.Number:
		return numberScalar(t)
	default:
		return v
	}
}

// numberScalar renders one JSON number so a YAML reader sees a number.
//
// An integer that fits becomes an int64 and yaml.v3 handles it. Everything else
// has to be emitted as a scalar to keep its digits, and there the spelling
// matters: YAML 1.1, which is what most readers outside Go implement, only
// resolves an exponent form as a float when it carries a decimal point and a
// signed exponent, so JSON's own "1e5" would arrive as a string. Reformatting
// through a float fixes the spelling, and anything too large for that keeps its
// digits verbatim, which is the one case where precision beats typing.
func numberScalar(n json.Number) any {
	text := n.String()

	if i, err := strconv.ParseInt(text, 10, 64); err == nil {
		return i
	}

	return &yaml.Node{Kind: yaml.ScalarNode, Value: exponentForm(text)}
}

// exponentForm spells a number the way a YAML 1.1 resolver recognizes it, which
// needs a decimal point in the mantissa as well as a signed exponent: a reader
// handed "1e5" produces the string, handed "1.0e+5" produces the number.
//
// The digits are edited rather than reformatted through a float, because a
// mantissa longer than float64 carries, or an exponent outside its range, would
// otherwise be rounded, truncated or flushed to zero. Losing digits to make a
// number readable is a worse trade than emitting one a reader treats as text.
func exponentForm(text string) string {
	mantissa, exponent, found := strings.Cut(strings.ToLower(text), "e")
	if !found {
		return text
	}

	if !strings.Contains(mantissa, ".") {
		mantissa += ".0"
	}

	if !strings.HasPrefix(exponent, "+") && !strings.HasPrefix(exponent, "-") {
		exponent = "+" + exponent
	}

	return mantissa + "e" + exponent
}

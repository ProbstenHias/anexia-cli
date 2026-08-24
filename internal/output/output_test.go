package output_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ProbstenHias/anexia-cli/internal/output"
)

func TestParseFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    output.Format
		wantErr string
	}{
		{name: "table", in: "table", want: output.FormatTable},
		{name: "json", in: "json", want: output.FormatJSON},
		{name: "yaml", in: "yaml", want: output.FormatYAML},
		{name: "tsv", in: "tsv", want: output.FormatTSV},
		{name: "invalid", in: "xml", wantErr: `invalid output format "xml": must be one of table, json, yaml, tsv`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := output.ParseFormat(tt.in)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestFormatStructured(t *testing.T) {
	t.Parallel()

	require.True(t, output.FormatJSON.Structured())
	require.True(t, output.FormatYAML.Structured())
	require.False(t, output.FormatTable.Structured())
	require.False(t, output.FormatTSV.Structured())
}

func TestWriterTable(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := output.NewWriter(&buf, output.FormatTable)

	require.NoError(t, w.Table(
		[]string{"code", "name"},
		[][]string{{"ANX04", "Vienna"}, {"ANX99", "A much longer name"}},
	))

	require.Equal(t, "CODE    NAME\nANX04   Vienna\nANX99   A much longer name\n", buf.String())
}

func TestWriterTableHeadersOnly(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := output.NewWriter(&buf, output.FormatTable)

	require.NoError(t, w.Table([]string{"code", "name"}, nil))
	require.Equal(t, "CODE   NAME\n", buf.String())
}

func TestWriterTableNoHeaders(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := output.NewWriter(&buf, output.FormatTable)
	w.SetNoHeaders(true)

	require.NoError(t, w.Table([]string{"code", "name"}, [][]string{{"ANX04", "Vienna"}}))
	require.Equal(t, "ANX04   Vienna\n", buf.String())
}

func TestWriterTSV(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := output.NewWriter(&buf, output.FormatTSV)

	require.NoError(t, w.Table(
		[]string{"code", "name"},
		[][]string{{"ANX04", "Vienna"}, {"ANX99", "A much longer name"}},
	))

	require.Equal(t, "code\tname\nANX04\tVienna\nANX99\tA much longer name\n", buf.String())
}

func TestWriterTSVNoHeaders(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := output.NewWriter(&buf, output.FormatTSV)
	w.SetNoHeaders(true)

	require.NoError(t, w.Table([]string{"code"}, [][]string{{"ANX04"}}))
	require.Equal(t, "ANX04\n", buf.String())
}

func TestWriterJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := output.NewWriter(&buf, output.FormatJSON)

	require.NoError(t, w.JSON([]string{"a", "b"}))
	require.Equal(t, "[\n  \"a\",\n  \"b\"\n]\n", buf.String())
}

func TestWriterObjectJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := output.NewWriter(&buf, output.FormatJSON)

	require.NoError(t, w.Object(map[string]string{"code": "ANX04"}))
	require.Equal(t, "{\n  \"code\": \"ANX04\"\n}\n", buf.String())
}

func TestWriterObjectYAML(t *testing.T) {
	t.Parallel()

	type location struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}

	var buf bytes.Buffer
	w := output.NewWriter(&buf, output.FormatYAML)

	require.NoError(t, w.Object([]location{{Code: "ANX04", Name: "Vienna"}}))
	require.Equal(t, "- code: ANX04\n  name: Vienna\n", buf.String())
}

func TestWriterObjectYAMLUsesJSONTags(t *testing.T) {
	t.Parallel()

	type location struct {
		CountryCode string `json:"country"`
	}

	var buf bytes.Buffer
	w := output.NewWriter(&buf, output.FormatYAML)

	require.NoError(t, w.Object(location{CountryCode: "AT"}))
	require.Equal(t, "country: AT\n", buf.String())
}

// TestWriterObjectYAMLKeepsLargeNumbersExact pins that -o yaml reports the
// same number the Engine sent. Anything routed through a float loses precision
// above 2^53, and core resource attributes are arbitrary Engine JSON, so a
// large identifier or byte count arriving there must survive the format
// change: a user comparing -o json against -o yaml has to see one value.
func TestWriterObjectYAMLKeepsLargeNumbersExact(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := output.NewWriter(&buf, output.FormatYAML)

	// json.RawMessage is how the API structs carry Engine payloads the CLI
	// does not model, so this is the realistic shape rather than a contrived
	// one.
	payload := map[string]any{
		"attributes": json.RawMessage(`{"bytes":9007199254740993,"huge":12345678901234567890123}`),
	}

	require.NoError(t, w.Object(payload))
	require.Equal(t, "attributes:\n  bytes: 9007199254740993\n  huge: 12345678901234567890123\n", buf.String())
}

// TestWriterObjectYAMLKeepsNumbersNumeric checks that a number stays a number
// for the reader, not just that its digits survive. YAML 1.1, which is what
// PyYAML and most non-Go readers implement, only resolves an exponent form as a
// float when it carries both a decimal point and a signed exponent, so emitting the
// JSON spelling verbatim can turn a number into a string on the way out.
func TestWriterObjectYAMLKeepsNumbersNumeric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		json string
		want string
	}{
		{name: "exponent without a sign", json: `1e5`, want: "1.0e+5"},
		{name: "exponent with a fraction", json: `1.5e10`, want: "1.5e+10"},
		{name: "negative exponent", json: `1e-7`, want: "1.0e-7"},
		{name: "plain integer", json: `42`, want: "42"},
		{name: "negative zero", json: `-0`, want: "-0"},
		{name: "plain float", json: `0.1`, want: "0.1"},
		{name: "beyond int64", json: `12345678901234567890123`, want: "12345678901234567890123"},

		// Spelling a number for the reader must not cost digits. Routing
		// these through float64 rounds the first, truncates the second and
		// underflows the third to zero, which is worse than the string a
		// YAML 1.1 reader would have produced.
		{name: "exponent past float64 precision", json: `9007199254740993e0`, want: "9007199254740993.0e+0"},
		{name: "exponent with a long mantissa", json: `1.23456789012345678901e10`, want: "1.23456789012345678901e+10"},
		{name: "exponent below float64 range", json: `1e-400`, want: "1.0e-400"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			w := output.NewWriter(&buf, output.FormatYAML)

			require.NoError(t, w.Object(map[string]any{
				"value": json.RawMessage(tt.json),
			}))
			require.Equal(t, "value: "+tt.want+"\n", buf.String())
		})
	}
}

// TestWriterKeepsOneRecordPerLine pins that a control byte inside a cell cannot
// invent columns or rows, in either column format. The point of tsv is feeding
// cut and awk, and of table is being readable, so a value the Engine allows
// must not shift every later column of that record.
//
// Both formats are driven for every byte, because table and tsv split on
// different sets: tabwriter ends a cell on a tab or a vertical tab, and a line
// on a newline or a form feed.
func TestWriterKeepsOneRecordPerLine(t *testing.T) {
	t.Parallel()

	for _, format := range []output.Format{output.FormatTable, output.FormatTSV} {
		for _, bad := range []string{"\t", "\n", "\r", "\v", "\f"} {
			var buf bytes.Buffer
			w := output.NewWriter(&buf, format)

			require.NoError(t, w.Table(
				[]string{"name", "identifier"},
				[][]string{{"a" + bad + "b", "t-1"}},
			))

			lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
			require.Len(t, lines, 2,
				"%s with %q: one header line and one record line, got %q", format, bad, buf.String())

			record := lines[1]

			// The record must still end with the last column, so nothing has
			// shifted or been pushed onto another line.
			require.True(t, strings.HasSuffix(record, "t-1"),
				"%s with %q: the identifier column must still end the record, got %q", format, bad, record)

			if format == output.FormatTSV {
				fields := strings.Split(record, "\t")
				require.Len(t, fields, 2,
					"%s with %q: a record must keep exactly as many fields as there are columns", format, bad)

				// The byte must not survive inside the cell either, where a
				// reader splitting on it would see a field that is not there.
				require.NotContains(t, fields[0], bad,
					"%s: %q must not reach the output inside a cell", format, bad)
			}
		}
	}
}

func TestWriterObjectYAMLRejectsUnencodable(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := output.NewWriter(&buf, output.FormatYAML)

	require.ErrorContains(t, w.Object(func() {}), "encoding yaml")
}

func TestWriterFormat(t *testing.T) {
	t.Parallel()

	require.Equal(t, output.FormatJSON, output.NewWriter(&bytes.Buffer{}, output.FormatJSON).Format())
}

func TestFormatNames(t *testing.T) {
	t.Parallel()

	require.Equal(t, "table, json, yaml, tsv", output.FormatNames())
}

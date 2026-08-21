package output_test

import (
	"bytes"
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

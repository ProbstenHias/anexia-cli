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
		{name: "invalid", in: "yaml", wantErr: `invalid output format "yaml": must be one of table, json`},
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

func TestWriterJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := output.NewWriter(&buf, output.FormatJSON)

	require.NoError(t, w.JSON([]string{"a", "b"}))
	require.Equal(t, "[\n  \"a\",\n  \"b\"\n]\n", buf.String())
}

func TestWriterFormat(t *testing.T) {
	t.Parallel()

	require.Equal(t, output.FormatJSON, output.NewWriter(&bytes.Buffer{}, output.FormatJSON).Format())
}

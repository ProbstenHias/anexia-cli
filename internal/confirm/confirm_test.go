package confirm_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ProbstenHias/anexia-cli/internal/confirm"
	"github.com/ProbstenHias/anexia-cli/internal/errmap"
)

func TestPromptAssumeYesSkipsPrompt(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	require.NoError(t, confirm.Prompt(nil, &out, "delete vlan \"v-1\"", true))
	assert.Empty(t, out.String())
}

func TestPromptAcceptsAnswers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		answer string
	}{
		{"lower y", "y\n"},
		{"upper y", "Y\n"},
		{"yes", "yes\n"},
		{"padded yes", "  YES  \n"},
		{"no trailing newline", "y"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			require.NoError(t, confirm.Prompt(strings.NewReader(tt.answer), &out, "delete tag", false))
			assert.Equal(t, "delete tag [y/N]: ", out.String())
		})
	}
}

func TestPromptRejectsAnswers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		answer string
	}{
		{"empty", "\n"},
		{"no", "n\n"},
		{"nonsense", "maybe\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			err := confirm.Prompt(strings.NewReader(tt.answer), &out, "delete tag", false)

			require.ErrorIs(t, err, errmap.ErrCanceled)
			assert.Equal(t, "canceled: delete tag", err.Error())
		})
	}
}

// TestPromptWithoutInputAsksForYesFlag covers both ways there can be nobody to
// answer: no reader at all, and a reader that is already at EOF, which is what
// an unattended run actually hands over. Neither is a refusal, so the error has
// to point at --yes rather than claim the user said no.
func TestPromptWithoutInputAsksForYesFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   io.Reader
	}{
		{"no reader", nil},
		{"reader at eof", strings.NewReader("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			err := confirm.Prompt(tt.in, &out, "delete tag", false)

			require.ErrorIs(t, err, errmap.ErrCanceled)
			assert.Contains(t, err.Error(), "pass --yes")
		})
	}
}

// failingReader reports a failure that is not io.EOF.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("terminal closed")
}

func TestPromptReportsReadFailure(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := confirm.Prompt(failingReader{}, &out, "delete tag", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading confirmation: terminal closed")
}

// failingWriter rejects every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("stderr closed")
}

func TestPromptReportsWriteFailure(t *testing.T) {
	t.Parallel()

	err := confirm.Prompt(strings.NewReader("y\n"), failingWriter{}, "delete tag", false)

	require.Error(t, err)
	assert.Equal(t, "stderr closed", err.Error())
}

package anx_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ProbstenHias/anexia-cli/internal/anx"
)

func TestNewClientRequiresToken(t *testing.T) {
	t.Parallel()

	_, err := anx.NewClient(anx.Options{})
	require.ErrorIs(t, err, anx.ErrNoToken)
	require.EqualError(t, err, "no API token: pass --token, set ANEXIA_TOKEN, or run 'anexia config set token <value>'")
}

func TestNewClientDefaultBaseURL(t *testing.T) {
	t.Parallel()

	c, err := anx.NewClient(anx.Options{Token: "tok"})
	require.NoError(t, err)
	require.Equal(t, "https://engine.anexia-it.com", c.BaseURL())
}

func TestNewClientBaseURLOverride(t *testing.T) {
	t.Parallel()

	c, err := anx.NewClient(anx.Options{Token: "tok", BaseURL: "http://127.0.0.1:8080"})
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:8080", c.BaseURL())
}

func TestNewClientInvalidBaseURL(t *testing.T) {
	t.Parallel()

	_, err := anx.NewClient(anx.Options{Token: "tok", BaseURL: "not-a-url"})
	require.ErrorContains(t, err, "not-a-url")
}

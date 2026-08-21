package anx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "go.anx.io/go-anxcloud/pkg/apis/core/v1"

	"github.com/ProbstenHias/anexia-cli/internal/anx"
)

func TestNewClientRequiresToken(t *testing.T) {
	t.Parallel()

	_, err := anx.NewClient(anx.Options{})
	require.ErrorIs(t, err, anx.ErrNoToken)
	require.EqualError(t, err, "not authenticated: pass --token, set ANEXIA_TOKEN, or run 'anexia config set token <value>'")
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

func TestNewAPIRequiresToken(t *testing.T) {
	t.Parallel()

	_, err := anx.NewAPI(anx.Options{})
	require.ErrorIs(t, err, anx.ErrNoToken)
}

func TestNewAPIInvalidBaseURL(t *testing.T) {
	t.Parallel()

	_, err := anx.NewAPI(anx.Options{Token: "tok", BaseURL: "not-a-url"})
	require.ErrorContains(t, err, "not-a-url")
}

func TestNewAPIUsesBaseURLAndToken(t *testing.T) {
	t.Parallel()

	var gotAuth, gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"identifier":"id-1","code":"ANX04"}`))
	}))
	defer srv.Close()

	a, err := anx.NewAPI(anx.Options{Token: "tok", BaseURL: srv.URL})
	require.NoError(t, err)

	location := corev1.Location{Identifier: "id-1"}
	require.NoError(t, a.Get(context.Background(), &location))

	require.Equal(t, "Token tok", gotAuth)
	require.Equal(t, "/api/core/v1/location.json/id-1", gotPath)
	require.Equal(t, "ANX04", location.Code)
}

package anx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "go.anx.io/go-anxcloud/pkg/apis/core/v1"
	"go.anx.io/go-anxcloud/pkg/client"
	"go.anx.io/go-anxcloud/pkg/core/service"

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

// TestNewClientSendsTheTokenAndReachesTheBaseURL is the legacy-client mirror of
// the check above. The two clients are built from the same options but by
// different library constructors, so both need proof the token reaches the wire.
func TestNewClientSendsTheTokenAndReachesTheBaseURL(t *testing.T) {
	t.Parallel()

	var gotAuth, gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"identifier":"s-1","name":"svc"}]}`))
	}))
	defer srv.Close()

	c, err := anx.NewClient(anx.Options{Token: "tok", BaseURL: srv.URL})
	require.NoError(t, err)

	found, err := service.NewAPI(c).List(context.Background(), 1, 1)
	require.NoError(t, err)

	require.Equal(t, "Token tok", gotAuth)
	require.Equal(t, "/api/core/v1/service.json", gotPath)
	require.Len(t, found, 1)
	require.Equal(t, "svc", found[0].Name)
}

// TestNewClientClassifiesAStatusWithAnUnparseableBody covers the transport that
// backfills an error body: the legacy leaf clients only report 5xx themselves
// and rely on the library parsing an error body for everything else, so a
// bodyless or non-JSON 4xx would otherwise arrive with no status attached.
func TestNewClientClassifiesAStatusWithAnUnparseableBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "empty body", status: http.StatusForbidden, body: ""},
		{name: "html body", status: http.StatusForbidden, body: "<html><body>Forbidden</body></html>"},

		// Valid JSON is not enough: the library decodes the body into its
		// own struct, so any shape that parses but does not fit still takes
		// the discard path. A string error field and a non-numeric code are
		// both ordinary API gateway shapes.
		{name: "error is a string", status: http.StatusForbidden, body: `{"error":"Forbidden"}`},
		{name: "code is a string", status: http.StatusForbidden, body: `{"error":{"code":"FORBIDDEN","message":"no"}}`},
		{name: "body is an array", status: http.StatusForbidden, body: `["Forbidden"]`},
		{name: "body is a bare string", status: http.StatusForbidden, body: `"Forbidden"`},

		// The library treats anything outside 2xx as an error, so a redirect
		// to a proxy login page needs the same repair.
		{name: "redirect with html body", status: http.StatusMultipleChoices, body: "<html>login</html>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			c, err := anx.NewClient(anx.Options{Token: "tok", BaseURL: srv.URL})
			require.NoError(t, err)

			_, err = service.NewAPI(c).List(context.Background(), 1, 1)
			require.Error(t, err)

			var responseErr *client.ResponseError
			require.ErrorAs(t, err, &responseErr, "the status must survive as a ResponseError")
			require.NotNil(t, responseErr.Response)
			require.Equal(t, tt.status, responseErr.Response.StatusCode)
		})
	}
}

// TestNewClientKeepsTheEngineWording checks the other side of the transport:
// when the Engine does send a usable error body, its message must reach the
// user rather than being replaced by the generic status text.
func TestNewClientKeepsTheEngineWording(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"service not found"}}`))
	}))
	defer srv.Close()

	c, err := anx.NewClient(anx.Options{Token: "tok", BaseURL: srv.URL})
	require.NoError(t, err)

	_, err = service.NewAPI(c).List(context.Background(), 1, 1)
	require.Error(t, err)

	var responseErr *client.ResponseError
	require.ErrorAs(t, err, &responseErr)
	require.Equal(t, "service not found", responseErr.ErrorData.Message)
}

// TestNewClientLeavesSuccessfulResponsesAlone guards the transport against
// touching the responses it has no business rewriting.
func TestNewClientLeavesSuccessfulResponsesAlone(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"identifier":"s-1","name":"svc"}]}`))
	}))
	defer srv.Close()

	c, err := anx.NewClient(anx.Options{Token: "tok", BaseURL: srv.URL})
	require.NoError(t, err)

	found, err := service.NewAPI(c).List(context.Background(), 1, 1)
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, "svc", found[0].Name)
}

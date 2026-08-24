package anx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.anx.io/go-anxcloud/pkg/api"
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

// TestNewClientDoesNotFollowAnErrorRedirect covers a proxy redirecting an API
// request to an HTML login page. Following it loses the original status and the
// legacy client reports an HTML decode error instead of the redirect response.
func TestNewClientDoesNotFollowAnErrorRedirect(t *testing.T) {
	t.Parallel()

	loginRequests := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			loginRequests++
			_, _ = w.Write([]byte("<html>login</html>"))

			return
		}

		http.Redirect(w, r, "/login", http.StatusFound)
	}))
	defer srv.Close()

	c, err := anx.NewClient(anx.Options{Token: "tok", BaseURL: srv.URL})
	require.NoError(t, err)

	_, err = service.NewAPI(c).List(context.Background(), 1, 1)
	require.Error(t, err)

	var responseErr *client.ResponseError
	require.ErrorAs(t, err, &responseErr, "the redirect status must survive as a ResponseError")
	require.Equal(t, http.StatusFound, responseErr.Response.StatusCode)
	require.Zero(t, loginRequests, "an API client must not follow a redirect to a login page")
}

// TestNewAPIDoesNotFollowAnErrorRedirect is the generic-client counterpart.
// Following a proxy redirect would send the Engine token to the login endpoint
// and hide the original status behind an HTML decode error.
func TestNewAPIDoesNotFollowAnErrorRedirect(t *testing.T) {
	t.Parallel()

	loginRequests := 0
	loginAuth := ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			loginRequests++
			loginAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte("<html>login</html>"))

			return
		}

		http.Redirect(w, r, "/login", http.StatusFound)
	}))
	defer srv.Close()

	a, err := anx.NewAPI(anx.Options{Token: "secret-token", BaseURL: srv.URL})
	require.NoError(t, err)

	err = a.Get(context.Background(), &corev1.Location{Identifier: "id-1"})
	require.Error(t, err)

	var httpErr api.HTTPError
	require.ErrorAs(t, err, &httpErr, "the redirect status must survive as an HTTPError")
	require.Equal(t, http.StatusFound, httpErr.StatusCode())
	require.Zero(t, loginRequests, "the API client must not follow the redirect")
	require.Empty(t, loginAuth, "the Engine token must never reach the login endpoint")
}

// TestClientsClassifyARedirectBeforeBodyEOF covers a proxy that sends redirect
// headers and never finishes the body. The status is already sufficient; both
// clients must return it rather than wait for the command timeout.
func TestClientsClassifyARedirectBeforeBodyEOF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(t *testing.T, baseURL string) error
	}{
		{
			name: "legacy",
			call: func(t *testing.T, baseURL string) error {
				t.Helper()
				c, err := anx.NewClient(anx.Options{Token: "tok", BaseURL: baseURL})
				require.NoError(t, err)

				_, err = service.NewAPI(c).List(context.Background(), 1, 1)

				return err
			},
		},
		{
			name: "generic",
			call: func(t *testing.T, baseURL string) error {
				t.Helper()
				a, err := anx.NewAPI(anx.Options{Token: "tok", BaseURL: baseURL})
				require.NoError(t, err)

				return a.Get(context.Background(), &corev1.Location{Identifier: "id-1"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			release := make(chan struct{})

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusFound)
				flusher, ok := w.(http.Flusher)
				require.True(t, ok)
				flusher.Flush()
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer func() {
				close(release)
				srv.Close()
			}()

			finished := make(chan error, 1)
			go func() { finished <- tt.call(t, srv.URL) }()

			select {
			case err := <-finished:
				require.Error(t, err)
			case <-time.After(500 * time.Millisecond):
				t.Fatal("client waited for EOF after receiving redirect headers")
			}
		})
	}
}

// TestClientsClassifyFailureHeadersWithoutWaitingForEOF covers a proxy that
// sends a known failure status and then never starts or closes the body. The
// status must not turn into the command timeout.
func TestClientsClassifyFailureHeadersWithoutWaitingForEOF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(t *testing.T, baseURL string) error
	}{
		{
			name: "legacy",
			call: func(t *testing.T, baseURL string) error {
				t.Helper()
				c, err := anx.NewClient(anx.Options{Token: "tok", BaseURL: baseURL})
				require.NoError(t, err)

				_, err = service.NewAPI(c).List(context.Background(), 1, 1)

				return err
			},
		},
		{
			name: "generic",
			call: func(t *testing.T, baseURL string) error {
				t.Helper()
				a, err := anx.NewAPI(anx.Options{Token: "tok", BaseURL: baseURL})
				require.NoError(t, err)

				return a.Get(context.Background(), &corev1.Location{Identifier: "id-1"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			release := make(chan struct{})

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				flusher, ok := w.(http.Flusher)
				require.True(t, ok)
				flusher.Flush()
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer func() {
				close(release)
				srv.Close()
			}()

			finished := make(chan error, 1)
			go func() { finished <- tt.call(t, srv.URL) }()

			select {
			case err := <-finished:
				require.Error(t, err)
			case <-time.After(500 * time.Millisecond):
				t.Fatal("client waited for EOF after receiving failure headers")
			}
		})
	}
}

// TestNewClientKeepsTheEngineWording checks the other side of the transport:
// when the Engine does send a usable error body, its message must reach the
// user rather than being replaced by the generic status text.
func TestNewClientKeepsTheEngineWording(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "engine error body",
			body: `{"error":{"code":404,"message":"service not found"}}`,
		},
		{
			// A proxy that appends to the body it forwards. The library
			// decodes with a streaming decoder, which stops at the end of the
			// first value, so it still reads the Engine's message here and
			// the transport must not decide otherwise.
			name: "engine error body with trailing bytes",
			body: `{"error":{"code":404,"message":"service not found"}}` + "\n<!-- proxy -->",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			c, err := anx.NewClient(anx.Options{Token: "tok", BaseURL: srv.URL})
			require.NoError(t, err)

			_, err = service.NewAPI(c).List(context.Background(), 1, 1)
			require.Error(t, err)

			var responseErr *client.ResponseError
			require.ErrorAs(t, err, &responseErr)
			require.Equal(t, "service not found", responseErr.ErrorData.Message)
		})
	}
}

// TestNewClientClassifiesACompleteErrorBodyBeforeEOF covers a chunked proxy
// that writes one complete Engine error and then keeps the stream open. The
// status and message are already available; waiting for EOF turns a not-found
// into the command timeout instead.
func TestNewClientClassifiesACompleteErrorBodyBeforeEOF(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"service not found"}}`))
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	c, err := anx.NewClient(anx.Options{Token: "tok", BaseURL: srv.URL})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err = service.NewAPI(c).List(ctx, 1, 1)
	require.Error(t, err)

	var responseErr *client.ResponseError
	require.ErrorAs(t, err, &responseErr, "a complete Engine error must classify before the stream closes")
	require.Equal(t, http.StatusNotFound, responseErr.Response.StatusCode)
	require.Equal(t, "service not found", responseErr.ErrorData.Message)
}

// TestNewClientLeavesSuccessfulResponsesAlone guards the transport against
// touching the responses it has no business rewriting.
//
// The second body matters: its error field holds a string, which is the shape
// the transport refuses to decode. If the status check ever stopped separating
// success from failure, that body would be replaced and the list would come
// back silently empty instead of erroring.
func TestNewClientLeavesSuccessfulResponsesAlone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "object body", body: `{"data":[{"identifier":"s-1","name":"svc"}]}`},
		{name: "body the transport could not decode", body: `{"error":"none","data":[{"identifier":"s-1","name":"svc"}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			c, err := anx.NewClient(anx.Options{Token: "tok", BaseURL: srv.URL})
			require.NoError(t, err)

			found, err := service.NewAPI(c).List(context.Background(), 1, 1)
			require.NoError(t, err)
			require.Len(t, found, 1)
			require.Equal(t, "svc", found[0].Name)
		})
	}
}

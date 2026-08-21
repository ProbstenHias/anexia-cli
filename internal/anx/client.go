// Package anx builds go-anxcloud clients from already-resolved settings.
//
// go-anxcloud ships two clients. The generic one in pkg/api works with any
// object in pkg/apis and is the one upstream is migrating to. The legacy
// per-area clients under pkg/ still cover APIs the generic client has no
// objects for, so the CLI needs both. Commands ask for whichever one their
// API area requires; both are built from the same resolved options.
package anx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/client"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
)

// ErrNoToken is returned when no API token could be resolved from any source.
// It wraps errmap.ErrAuth so the CLI exits with the authentication code.
var ErrNoToken = fmt.Errorf("%w: pass --token, set ANEXIA_TOKEN, or run 'anexia config set token <value>'", errmap.ErrAuth)

// Options carries the fully resolved settings needed to reach the Anexia API.
// It performs no lookups of its own: callers resolve flags, environment, and
// the config file first.
type Options struct {
	Token   string
	BaseURL string
}

// clientOptions turns the resolved settings into go-anxcloud client options.
func (o Options) clientOptions() ([]client.Option, error) {
	if o.Token == "" {
		return nil, ErrNoToken
	}

	options := []client.Option{client.TokenFromString(o.Token)}
	if o.BaseURL != "" {
		options = append(options, client.BaseURL(o.BaseURL))
	}

	return options, nil
}

// wrap annotates a construction failure with the base URL in play.
func (o Options) wrap(err error) error {
	if o.BaseURL != "" {
		return fmt.Errorf("creating anexia client with base URL %q: %w", o.BaseURL, err)
	}

	return fmt.Errorf("creating anexia client: %w", err)
}

// NewClient returns a legacy Anexia Engine client, for the API areas the
// generic client has no objects for.
//
// Unlike the generic client this one keeps go-anxcloud's engine-error parsing
// on, because the legacy leaf clients only treat 5xx as a failure themselves
// and rely on that parsing to surface a 4xx at all. See errorBodyTransport for
// what has to be repaired to make it dependable.
func NewClient(opts Options) (client.Client, error) {
	options, err := opts.clientOptions()
	if err != nil {
		return nil, err
	}

	options = append(options, client.WithClient(&http.Client{Transport: errorBodyTransport{}}))

	c, err := client.New(options...)
	if err != nil {
		return nil, opts.wrap(err)
	}

	return c, nil
}

// errorBodyTransport guarantees that a failing response carries a JSON body
// naming its status.
//
// go-anxcloud parses an error response by decoding the body into a struct that
// holds the status, and on a decode failure it drops the response and returns a
// bare decode error instead. A bodyless 404, which is the ordinary answer to a
// DELETE of something absent, and an HTML 403 from a proxy in front of the
// Engine both take that path, which leaves the CLI nothing to classify and
// reports a transport-level status as a JSON syntax error. Substituting a
// minimal body keeps the status reachable.
type errorBodyTransport struct{}

func (errorBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := http.DefaultTransport.RoundTrip(req)
	if err != nil || res.StatusCode < http.StatusBadRequest {
		return res, err
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	_ = res.Body.Close()

	if !json.Valid(body) {
		body, err = json.Marshal(map[string]any{
			"error": map[string]any{"code": res.StatusCode, "message": res.Status},
		})
		if err != nil {
			return nil, err
		}
	}

	res.Body = io.NopCloser(bytes.NewReader(body))
	res.ContentLength = int64(len(body))

	return res, nil
}

// NewAPI returns the generic Anexia Engine client, which serves every object
// in go-anxcloud's pkg/apis tree.
func NewAPI(opts Options) (api.API, error) {
	options, err := opts.clientOptions()
	if err != nil {
		return nil, err
	}

	a, err := api.NewAPI(api.WithClientOptions(options...))
	if err != nil {
		return nil, opts.wrap(err)
	}

	return a, nil
}

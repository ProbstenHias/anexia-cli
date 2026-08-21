// Package anx builds go-anxcloud clients from already-resolved settings.
//
// go-anxcloud ships two clients. The generic one in pkg/api works with any
// object in pkg/apis and is the one upstream is migrating to. The legacy
// per-area clients under pkg/ still cover APIs the generic client has no
// objects for, so the CLI needs both. Commands ask for whichever one their
// API area requires; both are built from the same resolved options.
package anx

import (
	"fmt"

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
func NewClient(opts Options) (client.Client, error) {
	options, err := opts.clientOptions()
	if err != nil {
		return nil, err
	}

	c, err := client.New(options...)
	if err != nil {
		return nil, opts.wrap(err)
	}

	return c, nil
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

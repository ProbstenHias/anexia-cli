// Package anx builds go-anxcloud clients from already-resolved settings.
package anx

import (
	"errors"
	"fmt"

	"go.anx.io/go-anxcloud/pkg/client"
)

// ErrNoToken is returned when no API token could be resolved from any source.
var ErrNoToken = errors.New("no API token: pass --token, set ANEXIA_TOKEN, or run 'anexia config set token <value>'")

// Options carries the fully resolved settings needed to reach the Anexia API.
// It performs no lookups of its own: callers resolve flags, environment, and
// the config file first.
type Options struct {
	Token   string
	BaseURL string
}

// NewClient returns an Anexia Engine client for the given options.
func NewClient(opts Options) (client.Client, error) {
	if opts.Token == "" {
		return nil, ErrNoToken
	}

	options := []client.Option{client.TokenFromString(opts.Token)}
	if opts.BaseURL != "" {
		options = append(options, client.BaseURL(opts.BaseURL))
	}

	c, err := client.New(options...)
	if err != nil {
		if opts.BaseURL != "" {
			return nil, fmt.Errorf("creating anexia client with base URL %q: %w", opts.BaseURL, err)
		}

		return nil, fmt.Errorf("creating anexia client: %w", err)
	}

	return c, nil
}

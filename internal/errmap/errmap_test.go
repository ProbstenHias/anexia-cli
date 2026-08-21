package errmap_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.anx.io/go-anxcloud/pkg/api"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
)

// httpError builds the Engine error the generic client returns for a status.
func httpError(t *testing.T, status int) error {
	t.Helper()

	u, err := url.Parse("https://engine.anexia-it.com/api/core/v1/location.json")
	require.NoError(t, err)

	return api.NewHTTPError(status, http.MethodGet, u, nil)
}

func TestUsagef(t *testing.T) {
	t.Parallel()

	err := errmap.Usagef("--limit %d must be between 1 and %d", 0, 1000)

	require.ErrorIs(t, err, errmap.ErrUsage)
	assert.Equal(t, "invalid usage: --limit 0 must be between 1 and 1000", err.Error())
}

func TestUsageWrapsExistingError(t *testing.T) {
	t.Parallel()

	err := errmap.Usage(errors.New("unknown flag: --bogus"))

	require.ErrorIs(t, err, errmap.ErrUsage)
	assert.Equal(t, "invalid usage: unknown flag: --bogus", err.Error())
	assert.Equal(t, errmap.ExitUsage, errmap.ExitCode(err))
}

func TestUsageLeavesNilAlone(t *testing.T) {
	t.Parallel()

	assert.NoError(t, errmap.Usage(nil))
}

func TestExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, errmap.ExitOK},
		{"canceled", fmt.Errorf("%w: delete", errmap.ErrCanceled), errmap.ExitCanceled},
		{"usage", errmap.Usagef("bad flag"), errmap.ExitUsage},
		{"timeout", fmt.Errorf("listing: %w", context.DeadlineExceeded), errmap.ExitTimeout},
		{"rate limited", api.RateLimitError{RetryAfter: time.Now()}, errmap.ExitRateLimited},
		{"not found", fmt.Errorf("reading: %w", api.ErrNotFound), errmap.ExitNotFound},
		{"auth sentinel", fmt.Errorf("%w: no token", errmap.ErrAuth), errmap.ExitAuth},
		{"access denied", fmt.Errorf("listing: %w", api.ErrAccessDenied), errmap.ExitAuth},
		{"unclassified", errors.New("boom"), errmap.ExitError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, errmap.ExitCode(tt.err))
		})
	}
}

func TestExitCodeFromHTTPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		want   int
	}{
		{"unauthorized", http.StatusUnauthorized, errmap.ExitAuth},
		{"forbidden", http.StatusForbidden, errmap.ExitAuth},
		{"not found", http.StatusNotFound, errmap.ExitNotFound},
		{"too many requests", http.StatusTooManyRequests, errmap.ExitRateLimited},
		{"server error", http.StatusInternalServerError, errmap.ExitError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := fmt.Errorf("listing locations: %w", httpError(t, tt.status))

			assert.Equal(t, tt.want, errmap.ExitCode(err))
		})
	}
}

func TestMessageAddsTokenHint(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("listing locations: %w", api.ErrAccessDenied)

	assert.Contains(t, errmap.Message(err), "check your token with 'anexia config view'")
}

func TestMessageAddsTokenHintForUnauthorized(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("listing locations: %w", httpError(t, http.StatusUnauthorized))

	assert.Contains(t, errmap.Message(err), "check your token with 'anexia config view'")
}

func TestMessageAddsRateLimitHint(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("listing locations: %w", api.RateLimitError{RetryAfter: time.Now()})

	assert.Contains(t, errmap.Message(err), "retry later or lower the request rate")
}

func TestMessageLeavesOtherErrorsAlone(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "boom", errmap.Message(errors.New("boom")))
}

package errmap_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/client"

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

// responseError builds the Engine error the legacy client returns for a
// response whose body echoes the status, which is the common case.
func responseError(status int, message string) error {
	err := bodylessResponseError(status, message)
	err.ErrorData.Code = status

	return err
}

// bodylessResponseError builds the same error for a response whose body does
// not carry the status. Only the HTTP response has it, so anything reading the
// decoded body alone misclassifies these.
func bodylessResponseError(status int, message string) *client.ResponseError {
	err := &client.ResponseError{Response: &http.Response{StatusCode: status}}
	err.ErrorData.Message = message

	return err
}

// TestExitCodeFromLegacyStatus pins that a command using the legacy client
// exits with the same code as one using the generic client.
func TestExitCodeFromLegacyStatus(t *testing.T) {
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

			err := fmt.Errorf("listing tags: %w", responseError(tt.status, "nope"))

			assert.Equal(t, tt.want, errmap.ExitCode(err))
		})
	}
}

// TestExitCodeFromLegacyStatusWithoutBodyCode covers the Engine that answers
// with a status but a body that does not repeat it. The HTTP response carries
// the truth, so classification must not depend on the body.
func TestExitCodeFromLegacyStatusWithoutBodyCode(t *testing.T) {
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

			err := fmt.Errorf("listing tags: %w", bodylessResponseError(tt.status, ""))

			assert.Equal(t, tt.want, errmap.ExitCode(err))
		})
	}
}

// TestExitCodeFindsAStatusInEveryJoinedBranch covers a later legacy Engine
// error carrying the actionable class. errors.As returns only the first match
// of a type, so a 500 branch must not hide a joined 429, 404 or 401.
func TestExitCodeFindsAStatusInEveryJoinedBranch(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   int
	}{
		{name: "rate limited", status: http.StatusTooManyRequests, want: errmap.ExitRateLimited},
		{name: "not found", status: http.StatusNotFound, want: errmap.ExitNotFound},
		{name: "unauthorized", status: http.StatusUnauthorized, want: errmap.ExitAuth},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errors.Join(
				responseError(http.StatusInternalServerError, "first"),
				responseError(tt.status, "actionable"),
			)

			require.Equal(t, tt.want, errmap.ExitCode(err))
		})
	}
}

func TestMessageReportsTheStatusWithoutABodyCode(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("reading tag %q: %w", "t-1", bodylessResponseError(http.StatusNotFound, "nope"))

	assert.Equal(t, `reading tag "t-1": nope (404)`, errmap.Message(err))
}

func TestMessageRewritesLegacyErrorDump(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("reading tag %q: %w", "t-1", responseError(http.StatusNotFound, "tag not found"))

	assert.Equal(t, `reading tag "t-1": tag not found (404)`, errmap.Message(err))
}

// TestMessageKeepsFieldValidationDetail covers the user who mistyped one field
// of a create. The Engine says which field it rejected and why, and that detail
// is the only actionable part of the message, so rendering the error must not
// discard it.
func TestMessageKeepsFieldValidationDetail(t *testing.T) {
	t.Parallel()

	engineErr := responseError(http.StatusUnprocessableEntity, "validation failed")

	var responseErr *client.ResponseError
	require.ErrorAs(t, engineErr, &responseErr)

	responseErr.ErrorData.Validation = map[string]string{
		"service_identifier": "does not exist",
		"name":               "must not be empty",
	}

	message := errmap.Message(fmt.Errorf("creating tag: %w", engineErr))

	// Exact, so the field order is pinned: Go randomizes map iteration, and a
	// message that reorders between runs is unusable in a test or a diff.
	assert.Equal(t,
		"creating tag: validation failed (422) (name: must not be empty, service_identifier: does not exist)",
		message)
}

// TestMessageRewritesEveryLegacyErrorInTheChain covers a failure reported
// through more than one Engine call. Every struct dump has to be replaced, not
// just the outermost, or the user sees raw Go formatting mid-sentence.
func TestMessageRewritesEveryLegacyErrorInTheChain(t *testing.T) {
	t.Parallel()

	inner := fmt.Errorf("reading tag: %w", responseError(http.StatusNotFound, "tag not found"))
	outer := fmt.Errorf("listing tags: %w: %w", responseError(http.StatusTooManyRequests, "slow down"), inner)

	message := errmap.Message(outer)

	assert.NotContains(t, message, "received error from api")
	assert.Contains(t, message, "slow down (429)")
	assert.Contains(t, message, "tag not found (404)")
}

// TestMessageDoesNotRewriteUserContextThatLooksLikeAnErrorDump covers an
// identifier equal to the legacy error's own text. The context must remain
// verbatim while the wrapped cause is made readable.
func TestMessageDoesNotRewriteUserContextThatLooksLikeAnErrorDump(t *testing.T) {
	engineErr := responseError(http.StatusNotFound, "tag not found")
	err := fmt.Errorf("reading tag %q: %w", engineErr.Error(), engineErr)

	message := errmap.Message(err)

	require.Contains(t, message, `reading tag "`+engineErr.Error()+`"`)
	require.Contains(t, message, "tag not found (404)")
	require.Equal(t, 1, strings.Count(message, "received error from api:"),
		"only the user-supplied identifier should retain the dump-like text")
}

func TestMessageRewritesLegacyErrorWithoutMessage(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("listing tags: %w", responseError(http.StatusInternalServerError, ""))

	assert.Equal(t, "listing tags: the Engine returned status 500", errmap.Message(err))
}

func TestMessageAddsTokenHintForLegacyForbidden(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("listing tags: %w", responseError(http.StatusForbidden, "denied"))

	assert.Equal(t, "listing tags: denied (403) (check your token with 'anexia config view')", errmap.Message(err))
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

// TestMessageAddsTokenHintForMissingCredentials pins that every error which
// exits with ExitAuth also gets the hint. Message and ExitCode classify through
// the same switch, so the two cannot disagree.
func TestMessageAddsTokenHintForMissingCredentials(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("%w: pass --token", errmap.ErrAuth)

	require.Equal(t, errmap.ExitAuth, errmap.ExitCode(err))
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

// TestMessageSurvivesADeeplyWrappedChain covers a chain far deeper than any
// command builds. A cyclic chain is deliberately not tested: the standard
// library's own errors.Is loops forever on one, so no Go program can survive
// it and the CLI is not the right place to pretend otherwise.
func TestMessageSurvivesADeeplyWrappedChain(t *testing.T) {
	t.Parallel()

	err := responseError(http.StatusNotFound, "gone")
	for range 50 {
		err = fmt.Errorf("wrap: %w", err)
	}

	assert.Contains(t, errmap.Message(err), "gone (404)")
	assert.Equal(t, errmap.ExitNotFound, errmap.ExitCode(err))
}

// TestMessageSurvivesAJoinedChain covers the branching case: joined errors make
// the walk a tree rather than a list.
func TestMessageSurvivesAJoinedChain(t *testing.T) {
	t.Parallel()

	inner := responseError(http.StatusTooManyRequests, "slow down")
	err := fmt.Errorf("outer: %w: %w", inner, errors.New("side"))

	assert.Contains(t, errmap.Message(err), "slow down (429)")
	assert.Equal(t, errmap.ExitRateLimited, errmap.ExitCode(err))
}

func TestMessageOfNilIsEmpty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, errmap.Message(nil))
}

// TestMessageOrdersValidationFields pins the ordering, so the output of a
// failed create does not shuffle between runs of the same command.
func TestMessageOrdersValidationFields(t *testing.T) {
	t.Parallel()

	responseErr := bodylessResponseError(http.StatusUnprocessableEntity, "validation failed")
	responseErr.ErrorData.Validation = map[string]string{
		"name":    "required",
		"service": "unknown",
		"account": "closed",
	}

	assert.Equal(t,
		"creating tag: validation failed (422) (account: closed, name: required, service: unknown)",
		errmap.Message(fmt.Errorf("creating tag: %w", responseErr)))
}

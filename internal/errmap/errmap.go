// Package errmap translates errors into process exit codes and readable
// messages, so every command reports failures the same way.
package errmap

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"go.anx.io/go-anxcloud/pkg/api"
)

// Exit codes the CLI reports. Scripts can branch on them without parsing
// messages.
const (
	// ExitOK signals success.
	ExitOK = 0
	// ExitError signals an unclassified failure.
	ExitError = 1
	// ExitUsage signals bad flags or arguments.
	ExitUsage = 2
	// ExitAuth signals a missing or rejected token.
	ExitAuth = 3
	// ExitNotFound signals that the requested object does not exist.
	ExitNotFound = 4
	// ExitTimeout signals that the request deadline elapsed.
	ExitTimeout = 5
	// ExitRateLimited signals that the Engine throttled the request.
	ExitRateLimited = 6
	// ExitCanceled signals that the user aborted a confirmation prompt.
	ExitCanceled = 7
)

// ErrUsage marks an error as caused by bad user input.
var ErrUsage = errors.New("invalid usage")

// ErrCanceled marks an operation the user declined to confirm.
var ErrCanceled = errors.New("canceled")

// ErrAuth marks a missing or rejected credential.
var ErrAuth = errors.New("not authenticated")

// Usagef builds a usage error, which the CLI reports with ExitUsage.
func Usagef(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUsage, fmt.Sprintf(format, args...))
}

// Usage marks an existing error as caused by bad user input. It is used for
// the flag and argument errors cobra produces, which carry a good message but
// no way to classify them.
func Usage(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%w: %w", ErrUsage, err)
}

// ExitCode classifies err into one of the documented exit codes.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, ErrCanceled):
		return ExitCanceled
	case errors.Is(err, ErrUsage):
		return ExitUsage
	case errors.Is(err, context.DeadlineExceeded):
		return ExitTimeout
	case isRateLimited(err):
		return ExitRateLimited
	case errors.Is(err, api.ErrNotFound):
		return ExitNotFound
	case errors.Is(err, ErrAuth), errors.Is(err, api.ErrAccessDenied), hasStatus(err, http.StatusUnauthorized):
		return ExitAuth
	default:
		return ExitError
	}
}

// isRateLimited reports whether err was caused by Engine throttling.
// api.IsRateLimitError only type-switches on the error it is handed, so it
// misses the wrapped errors commands actually return.
func isRateLimited(err error) bool {
	var rateLimit api.RateLimitError
	if errors.As(err, &rateLimit) {
		return true
	}

	return hasStatus(err, http.StatusTooManyRequests)
}

// hasStatus reports whether err carries the given HTTP status from the Engine.
func hasStatus(err error, status int) bool {
	var httpErr api.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode() == status
	}

	return false
}

// Message renders err for the user, adding a hint for the failures where the
// Engine's own wording is not actionable on its own.
func Message(err error) string {
	switch {
	case errors.Is(err, api.ErrAccessDenied), hasStatus(err, http.StatusUnauthorized):
		return fmt.Sprintf("%v (check your token with 'anexia config view')", err)
	case ExitCode(err) == ExitRateLimited:
		return fmt.Sprintf("%v (retry later or lower the request rate)", err)
	default:
		return err.Error()
	}
}

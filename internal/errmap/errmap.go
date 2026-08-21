// Package errmap translates errors into process exit codes and readable
// messages, so every command reports failures the same way.
package errmap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/client"
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
	case errors.Is(err, api.ErrNotFound), hasStatus(err, http.StatusNotFound):
		return ExitNotFound
	case errors.Is(err, ErrAuth), errors.Is(err, api.ErrAccessDenied),
		hasStatus(err, http.StatusUnauthorized), hasStatus(err, http.StatusForbidden):
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
//
// The two go-anxcloud clients report status differently: the generic one
// returns api.HTTPError, the legacy one returns *client.ResponseError. Both are
// checked so a command's exit code does not depend on which client it uses.
func hasStatus(err error, status int) bool {
	var httpErr api.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode() == status
	}

	var responseErr *client.ResponseError
	if errors.As(err, &responseErr) {
		return legacyStatus(responseErr) == status
	}

	return false
}

// legacyStatus reads the status off a legacy client error. It prefers the HTTP
// response, because the code in the decoded body is only present when the
// Engine chose to echo it there.
func legacyStatus(err *client.ResponseError) int {
	if err.Response != nil {
		return err.Response.StatusCode
	}

	return err.ErrorData.Code
}

// Message renders err for the user, adding a hint for the failures where the
// Engine's own wording is not actionable on its own. It classifies through
// ExitCode so a message and an exit code can never disagree.
func Message(err error) string {
	text := readable(err)

	switch ExitCode(err) {
	case ExitAuth:
		return text + " (check your token with 'anexia config view')"
	case ExitRateLimited:
		return text + " (retry later or lower the request rate)"
	default:
		return text
	}
}

// readable replaces the legacy client's struct dumps with the message and
// status the Engine actually sent, keeping the calling command's prefix. Every
// dump in the chain is replaced: errors.As only finds the outermost, so a
// failure reported through more than one Engine call would otherwise still
// show Go struct formatting mid-sentence.
func readable(err error) string {
	text := err.Error()

	for _, responseErr := range responseErrors(err) {
		text = strings.Replace(text, responseErr.Error(), engineMessage(responseErr), 1)
	}

	return text
}

// responseErrors collects every legacy Engine error in the chain, outermost
// first. Joined errors branch, so the whole tree is walked rather than the
// single Unwrap chain errors.As follows.
func responseErrors(err error) []*client.ResponseError {
	var found []*client.ResponseError

	switch e := err.(type) { //nolint:errorlint // walking the tree needs the concrete shapes
	case interface{ Unwrap() []error }:
		for _, wrapped := range e.Unwrap() {
			found = append(found, responseErrors(wrapped)...)
		}
	case interface{ Unwrap() error }:
		found = responseErrors(e.Unwrap())
	}

	var responseErr *client.ResponseError
	if e, ok := err.(*client.ResponseError); ok { //nolint:errorlint // this frame only, children are handled above
		responseErr = e
	}

	if responseErr != nil {
		found = append([]*client.ResponseError{responseErr}, found...)
	}

	return found
}

// engineMessage renders one legacy Engine error as the Engine's own wording
// plus its status, keeping any field-level validation detail: when the Engine
// rejects a field, naming it is the only actionable part of the message.
func engineMessage(err *client.ResponseError) string {
	status := legacyStatus(err)

	text := fmt.Sprintf("%s (%d)", err.ErrorData.Message, status)
	if err.ErrorData.Message == "" {
		text = fmt.Sprintf("the Engine returned status %d", status)
	}

	if len(err.ErrorData.Validation) == 0 {
		return text
	}

	fields := make([]string, 0, len(err.ErrorData.Validation))
	for field, reason := range err.ErrorData.Validation {
		fields = append(fields, fmt.Sprintf("%s: %s", field, reason))
	}

	sort.Strings(fields)

	return fmt.Sprintf("%s (%s)", text, strings.Join(fields, ", "))
}

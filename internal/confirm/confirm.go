// Package confirm prompts before destructive operations.
package confirm

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
)

// Prompt asks the user to confirm the action described by what, writing the
// question to out and reading a yes/no answer from in. Anything other than yes
// counts as no. Callers pass assumeYes so the --yes flag is honored in one
// place instead of at every call site.
func Prompt(in io.Reader, out io.Writer, what string, assumeYes bool) error {
	if assumeYes {
		return nil
	}

	if in == nil {
		return unattended(what)
	}

	if _, err := fmt.Fprintf(out, "%s [y/N]: ", what); err != nil {
		return err
	}

	answer, err := bufio.NewReader(in).ReadString('\n')
	// io.EOF still leaves a usable answer in the buffer, so it is not a failure.
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reading confirmation: %w", err)
	}

	// Nothing at all to read means nobody is there to answer, which is the
	// normal shape for a scheduled job. Saying so beats reporting a refusal
	// the user never made.
	if errors.Is(err, io.EOF) && answer == "" {
		return unattended(what)
	}

	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	default:
		return fmt.Errorf("%w: %s", errmap.ErrCanceled, what)
	}
}

// unattended reports that confirmation was impossible rather than refused.
func unattended(what string) error {
	return fmt.Errorf("%w: %s needs confirmation but there is nothing to read an answer from, pass --yes", errmap.ErrCanceled, what)
}

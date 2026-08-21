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
// question to out and reading a yes/no answer from in. An empty or unreadable
// answer counts as no. Callers pass assumeYes so the --yes flag is honored in
// one place instead of at every call site.
func Prompt(in io.Reader, out io.Writer, what string, assumeYes bool) error {
	if assumeYes {
		return nil
	}

	if in == nil {
		return fmt.Errorf("%w: %s needs confirmation but no input is available, pass --yes", errmap.ErrCanceled, what)
	}

	if _, err := fmt.Fprintf(out, "%s [y/N]: ", what); err != nil {
		return err
	}

	answer, err := bufio.NewReader(in).ReadString('\n')
	// io.EOF still leaves a usable answer in the buffer, so it is not a failure.
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reading confirmation: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	default:
		return fmt.Errorf("%w: %s", errmap.ErrCanceled, what)
	}
}

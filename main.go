// Command anexia is a command-line interface for the Anexia Engine API.
package main

import (
	"fmt"
	"os"

	"github.com/ProbstenHias/anexia-cli/internal/cli"
	"github.com/ProbstenHias/anexia-cli/internal/errmap"
)

func main() {
	err := cli.Execute(cli.Deps{Stdout: os.Stdout, Stderr: os.Stderr})
	if err == nil {
		return
	}

	fmt.Fprintf(os.Stderr, "anexia: %s\n", errmap.Message(err))
	os.Exit(errmap.ExitCode(err))
}

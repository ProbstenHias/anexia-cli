// Command anexia is a command-line interface for the Anexia Engine API.
package main

import (
	"fmt"
	"os"

	"github.com/ProbstenHias/anexia-cli/internal/cli"
)

func main() {
	if err := cli.Execute(cli.Deps{Stdout: os.Stdout, Stderr: os.Stderr}); err != nil {
		fmt.Fprintf(os.Stderr, "anexia: %v\n", err)
		os.Exit(1)
	}
}

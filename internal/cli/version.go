package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Build metadata, overridden at link time via -ldflags -X.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the anexia version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "anexia %s (commit %s, built %s)\n", version, commit, date)

			return err
		},
	}
}

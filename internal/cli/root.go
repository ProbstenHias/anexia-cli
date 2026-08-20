// Package cli defines the anexia command tree.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.anx.io/go-anxcloud/pkg/client"
	"go.anx.io/go-anxcloud/pkg/vsphere/provisioning/location"

	"github.com/ProbstenHias/anexia-cli/internal/anx"
	"github.com/ProbstenHias/anexia-cli/internal/config"
	"github.com/ProbstenHias/anexia-cli/internal/output"
)

// Deps holds the collaborators the command tree needs, so tests can substitute
// streams and API constructors.
type Deps struct {
	Stdout io.Writer
	Stderr io.Writer

	// NewLocationAPI builds the location API from a client. Defaults to
	// location.NewAPI when nil.
	NewLocationAPI func(client.Client) location.API
}

func (d Deps) stdout() io.Writer {
	if d.Stdout != nil {
		return d.Stdout
	}

	return os.Stdout
}

func (d Deps) stderr() io.Writer {
	if d.Stderr != nil {
		return d.Stderr
	}

	return os.Stderr
}

func (d Deps) newLocationAPI(c client.Client) location.API {
	if d.NewLocationAPI != nil {
		return d.NewLocationAPI(c)
	}

	return location.NewAPI(c)
}

// globalOptions holds the values of the persistent flags.
type globalOptions struct {
	configPath   string
	outputFormat string
	timeout      time.Duration
}

// resolve layers the config file, the environment, and any changed flags.
func (g *globalOptions) resolve(flags *pflag.FlagSet) (config.Config, error) {
	return config.Resolve(g.configPath, flags, nil)
}

// writer builds an output writer for the requested format.
func (g *globalOptions) writer(out io.Writer) (*output.Writer, error) {
	f, err := output.ParseFormat(g.outputFormat)
	if err != nil {
		return nil, err
	}

	return output.NewWriter(out, f), nil
}

// client resolves configuration and builds an Anexia client.
func (g *globalOptions) client(flags *pflag.FlagSet) (client.Client, error) {
	cfg, err := g.resolve(flags)
	if err != nil {
		return nil, err
	}

	return anx.NewClient(anx.Options{Token: cfg.Token, BaseURL: cfg.APIBaseURL})
}

// context derives a context honoring the --timeout flag.
func (g *globalOptions) context(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, g.timeout)
}

// timeoutHint augments a deadline error with actionable advice.
func (g *globalOptions) timeoutHint(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w (timeout was %s, raise it with --timeout)", err, g.timeout)
	}

	return err
}

// NewRootCommand builds the anexia command tree.
func NewRootCommand(d Deps) *cobra.Command {
	opts := &globalOptions{}

	root := &cobra.Command{
		Use:   "anexia",
		Short: "Command-line interface for the Anexia Engine API",
		Long:  "anexia talks to the Anexia Engine API using the official go-anxcloud client.",
		// Errors are reported by main, so cobra must not add usage noise.
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	root.SetOut(d.stdout())
	root.SetErr(d.stderr())

	flags := root.PersistentFlags()
	flags.StringVar(&opts.configPath, "config", "", "path to the config file")
	flags.String("token", "", "Anexia API token")
	flags.String("api-base-url", "", "Anexia Engine base URL")
	flags.StringVarP(&opts.outputFormat, "output", "o", string(output.FormatTable), "output format: table or json")
	flags.DurationVar(&opts.timeout, "timeout", 30*time.Second, "timeout for API requests")

	root.AddCommand(
		newLocationCommand(d, opts),
		newConfigCommand(opts),
		newVersionCommand(),
	)

	return root
}

// Execute runs the command tree.
func Execute(d Deps) error {
	return NewRootCommand(d).Execute()
}

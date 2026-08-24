// Package cli defines the anexia command tree.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/client"

	"github.com/ProbstenHias/anexia-cli/internal/anx"
	"github.com/ProbstenHias/anexia-cli/internal/config"
	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/output"
)

// Deps holds the collaborators the command tree needs, so tests can substitute
// the output streams.
type Deps struct {
	Stdout io.Writer
	Stderr io.Writer
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

// globalOptions holds the values of the persistent flags and implements the
// resource.Env interface the generated commands run against.
type globalOptions struct {
	configPath   string
	outputFormat string
	noHeaders    bool
	assumeYes    bool
	timeout      time.Duration
}

// resolve layers the config file, the environment, and any changed flags.
func (g *globalOptions) resolve(flags *pflag.FlagSet) (config.Config, error) {
	return config.Resolve(g.configPath, flags, nil)
}

// options turns the resolved configuration into client options.
func (g *globalOptions) options(flags *pflag.FlagSet) (anx.Options, error) {
	cfg, err := g.resolve(flags)
	if err != nil {
		return anx.Options{}, err
	}

	return anx.Options{Token: cfg.Token, BaseURL: cfg.APIBaseURL}, nil
}

// Writer builds an output writer for the requested format.
func (g *globalOptions) Writer(out io.Writer) (*output.Writer, error) {
	f, err := output.ParseFormat(g.outputFormat)
	if err != nil {
		// An unsupported --output is a flag mistake, not a failed request.
		return nil, errmap.Usage(err)
	}

	w := output.NewWriter(out, f)
	w.SetNoHeaders(g.noHeaders)

	return w, nil
}

// Client builds the legacy Anexia client.
func (g *globalOptions) Client(flags *pflag.FlagSet) (client.Client, error) {
	opts, err := g.options(flags)
	if err != nil {
		return nil, err
	}

	c, err := anx.NewClient(opts)

	return c, explicitBaseURLError(flags, err)
}

// API builds the generic Anexia client.
func (g *globalOptions) API(flags *pflag.FlagSet) (api.API, error) {
	opts, err := g.options(flags)
	if err != nil {
		return nil, err
	}

	a, err := anx.NewAPI(opts)

	return a, explicitBaseURLError(flags, err)
}

// explicitBaseURLError classifies a malformed value passed directly as a flag
// as usage. The same error from a stored config remains a config failure.
func explicitBaseURLError(flags *pflag.FlagSet, err error) error {
	if err != nil && flags.Changed("api-base-url") && errors.Is(err, client.ErrInvalidBaseURL) {
		value, _ := flags.GetString("api-base-url")

		return errmap.Usagef("invalid --api-base-url %q: %v", value, err)
	}

	return err
}

// Context derives a context honoring the --timeout flag.
func (g *globalOptions) Context(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, g.timeout)
}

// validate rejects global flag values that cannot lead anywhere. Commands call
// it before contacting the Engine.
func (g *globalOptions) validate() error {
	if g.timeout <= 0 {
		return errmap.Usagef("--timeout %s must be greater than zero", g.timeout)
	}

	// Checked here rather than only where a writer gets built, so a command
	// that prints no object still rejects a format it cannot honor instead
	// of appearing to accept one.
	if _, err := output.ParseFormat(g.outputFormat); err != nil {
		return errmap.Usage(err)
	}

	return nil
}

// AssumeYes reports whether --yes was passed.
func (g *globalOptions) AssumeYes() bool {
	return g.assumeYes
}

// Fail augments a deadline error with actionable advice.
func (g *globalOptions) Fail(err error) error {
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
		// Only root defines this, so cobra runs it for every command in the
		// tree and global flag values are checked exactly once.
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			return opts.validate()
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	// Bad flags and bad argument counts are the user's mistake, so they get
	// the usage exit code instead of the generic failure one. Cobra reports
	// both without a way to classify them, so they are marked here, in the
	// one place every command inherits from.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return errmap.Usage(err)
	})

	root.SetOut(d.stdout())
	root.SetErr(d.stderr())

	flags := root.PersistentFlags()
	flags.StringVar(&opts.configPath, "config", "", "path to the config file")
	flags.String("token", "", "Anexia API token")
	flags.String("api-base-url", "", "Anexia Engine base URL")
	flags.StringVarP(&opts.outputFormat, "output", "o", string(output.FormatTable), "output format: "+output.FormatNames())
	flags.BoolVar(&opts.noHeaders, "no-headers", false, "omit the header row in table and tsv output")
	flags.BoolVarP(&opts.assumeYes, "yes", "y", false, "skip confirmation prompts")
	flags.DurationVar(&opts.timeout, "timeout", 30*time.Second, "timeout for API requests")

	root.AddCommand(
		newCoreCommand(opts),
		newNetworkCommand(opts),
		newConfigCommand(opts),
		newVersionCommand(),
		newMovedCommand("location", "anexia core location list"),
		newCompletionCommand(root),
	)
	help := newHelpCommand(root)
	root.SetHelpCommand(help)
	root.AddCommand(help)

	markUsageErrors(root)

	return root
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "completion",
		Short:             "Generate a shell completion script",
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	generators := []struct {
		name string
		long string
		run  func(io.Writer, bool) error
	}{
		{name: "bash", long: `Generate a completion script for bash.

This script depends on the bash-completion package.
Load it in the current session with:

  source <(anexia completion bash)

Install it for future sessions under /etc/bash_completion.d/ on Linux or
$(brew --prefix)/etc/bash_completion.d/ on macOS.`, run: func(w io.Writer, descriptions bool) error {
			return root.GenBashCompletionV2(w, descriptions)
		}},
		{name: "zsh", long: `Generate a completion script for zsh.

Load it in the current session with:

  source <(anexia completion zsh)

Install it for future sessions in a directory on $fpath.`, run: func(w io.Writer, descriptions bool) error {
			if descriptions {
				return root.GenZshCompletion(w)
			}

			return root.GenZshCompletionNoDesc(w)
		}},
		{name: "fish", long: `Generate a completion script for fish.

Load it in the current session with:

  anexia completion fish | source

Install it for future sessions under ~/.config/fish/completions/.`, run: root.GenFishCompletion},
		{name: "powershell", long: `Generate a completion script for PowerShell.

Load it in the current session with:

  anexia completion powershell | Out-String | Invoke-Expression`, run: func(w io.Writer, descriptions bool) error {
			if descriptions {
				return root.GenPowerShellCompletionWithDesc(w)
			}

			return root.GenPowerShellCompletion(w)
		}},
	}
	for _, generator := range generators {
		noDescriptions := false
		shell := &cobra.Command{
			Use:                   generator.name,
			Short:                 "Generate completion for " + generator.name,
			Long:                  generator.long,
			Args:                  cobra.NoArgs,
			ValidArgsFunction:     cobra.NoFileCompletions,
			DisableFlagsInUseLine: true,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return generator.run(cmd.OutOrStdout(), !noDescriptions)
			},
		}
		shell.Flags().BoolVar(&noDescriptions, "no-descriptions", false, "disable completion descriptions")
		cmd.AddCommand(shell)
	}

	return cmd
}

func newHelpCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command",
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
			target, _, err := root.Find(args)
			if err != nil || target == nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}

			var completions []cobra.Completion
			for _, child := range target.Commands() {
				if (child.IsAvailableCommand() || child.Name() == "help") && strings.HasPrefix(child.Name(), toComplete) {
					completions = append(completions, cobra.CompletionWithDesc(child.Name(), child.Short))
				}
			}

			return completions, cobra.ShellCompDirectiveNoFileComp
		},
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}

			target, remaining, err := root.Find(args)
			if err != nil {
				return err
			}
			if len(remaining) > 0 {
				return fmt.Errorf("unknown command %q for %q", remaining[0], target.CommandPath())
			}

			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return root.Help()
			}

			target, _, _ := root.Find(args)

			return target.Help()
		},
	}
}

// newMovedCommand points a command that used to exist at its replacement.
// Cobra's suggestions cannot help here, because the old name is not a near
// miss of any current one, so without this the user only sees "unknown
// command" and has to find the migration note in the README.
//
// It is hidden, so it does not appear in help or completion as if it worked.
func newMovedCommand(name, replacement string) *cobra.Command {
	return &cobra.Command{
		Use:                name,
		Short:              "Moved to " + replacement,
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errmap.Usagef("%q has moved, use %q instead", name, replacement)
		},
	}
}

// markUsageErrors makes every argument validator in the tree report a usage
// error. Cobra's validators produce a good message but no way to classify it,
// and doing this once here beats wrapping 20 call sites that would drift.
func markUsageErrors(cmd *cobra.Command) {
	if validate := cmd.Args; validate != nil {
		cmd.Args = func(cmd *cobra.Command, args []string) error {
			return errmap.Usage(validate(cmd, args))
		}
	}

	for _, child := range cmd.Commands() {
		markUsageErrors(child)
	}
}

// Execute runs the command tree.
func Execute(d Deps) error {
	return NewRootCommand(d).Execute()
}

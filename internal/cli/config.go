package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ProbstenHias/anexia-cli/internal/config"
	"github.com/ProbstenHias/anexia-cli/internal/errmap"
)

func newConfigCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and modify the anexia config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newConfigPathCommand(opts),
		newConfigInitCommand(opts),
		newConfigSetCommand(opts),
		newConfigGetCommand(opts),
		newConfigViewCommand(opts),
	)

	return cmd
}

func newConfigPathCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the config file path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := config.Path(opts.configPath)
			if err != nil {
				return err
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), path)

			return err
		},
	}
}

func newConfigInitCommand(opts *globalOptions) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create an empty config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := config.Path(opts.configPath)
			if err != nil {
				return err
			}

			if !force {
				if _, err := os.Stat(path); err == nil {
					return fmt.Errorf("config file %s already exists: pass --force to overwrite it", path)
				} else if !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("checking config %s: %w", path, err)
				}
			}

			if err := config.Save(path, config.Config{}); err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", path)

			return err
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing config file")

	return cmd
}

func newConfigSetCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Long:  "Set a config value. Valid keys are " + strings.Join(config.Keys, ", ") + ".",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.Path(opts.configPath)
			if err != nil {
				return err
			}

			cfg, err := config.Load(path)
			if err != nil {
				return err
			}

			if err := cfg.Set(args[0], args[1]); err != nil {
				return errmap.Usage(err)
			}

			if err := config.Save(path, cfg); err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "set %s in %s\n", args[0], path)

			return err
		},
	}
}

func newConfigGetCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print a config value, with the token masked",
		Long:  "Print a config value. Valid keys are " + strings.Join(config.Keys, ", ") + ". The token is masked.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(opts.configPath)
			if err != nil {
				return err
			}

			value, err := cfg.Get(args[0])
			if err != nil {
				return errmap.Usage(err)
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), value)

			return err
		},
	}
}

func newConfigViewCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "view",
		Short: "Show the stored config with the token masked",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(opts.configPath)
			if err != nil {
				return err
			}

			w, err := opts.Writer(cmd.OutOrStdout())
			if err != nil {
				return err
			}

			redacted := cfg.Redacted()

			if w.Format().Structured() {
				return w.Object(map[string]string{
					"token":        redacted.Token,
					"api_base_url": redacted.APIBaseURL,
				})
			}

			return w.Table([]string{"key", "value"}, [][]string{
				{"token", redacted.Token},
				{"api_base_url", redacted.APIBaseURL},
			})
		},
	}
}

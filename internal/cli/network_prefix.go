package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"go.anx.io/go-anxcloud/pkg/ipam/prefix"

	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// newNetworkPrefixCommand builds "network prefix". Prefixes have no object in
// go-anxcloud's generic pkg/apis tree, so these commands drive the legacy
// ipam/prefix client directly, sharing the paging and rendering helpers so the
// result is indistinguishable from a Spec-driven noun.
//
// The library implements create, update and delete here too. They are not
// wired yet because the resource registry has no write verbs, and adding them
// on this half alone is the divergence between the two halves that
// docs/cli-design.md records as a bug every time it has happened.
func newNetworkPrefixCommand(opts *globalOptions) *cobra.Command {
	return resource.Noun("prefix", "prefixes", "Work with Anexia IP prefixes",
		newNetworkPrefixListCommand(opts),
		newNetworkPrefixGetCommand(opts),
	)
}

func newNetworkPrefixListCommand(opts *globalOptions) *cobra.Command {
	var (
		page   int
		limit  int
		all    bool
		search string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List prefixes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := resource.ValidatePaging(page, limit, all); err != nil {
				return err
			}

			w, err := opts.Writer(cmd.OutOrStdout())
			if err != nil {
				return err
			}

			c, err := opts.Client(cmd.Flags())
			if err != nil {
				return err
			}

			ctx, cancel := opts.Context(cmd.Context())
			defer cancel()

			// Unlike the tags client, this one query-escapes its search
			// term itself, so escaping here would encode the percent
			// signs and search for the escaping rather than the value.
			a := prefix.NewAPI(c)

			found, err := resource.FetchPages(cmd.ErrOrStderr(), "prefixes", page, limit, all, func(p int) ([]prefix.Summary, error) {
				return a.List(ctx, p, limit, search)
			})
			if err != nil {
				return opts.Fail(fmt.Errorf("listing prefixes: %w", err))
			}

			return resource.RenderList(cmd, w, "prefixes", found,
				[]string{"identifier", "name", "description"},
				func(s *prefix.Summary) []string {
					return []string{s.ID, s.Name, s.CustomerDescription}
				},
			)
		},
	}

	flags := cmd.Flags()
	resource.RegisterPagingFlags(flags, &page, &limit, &all, "prefixes")
	flags.StringVar(&search, "search", "", "only list prefixes matching this term")

	return cmd
}

func newNetworkPrefixGetCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Show one prefix",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := resource.ValidateIdentifier("prefix", args[0]); err != nil {
				return err
			}

			w, err := opts.Writer(cmd.OutOrStdout())
			if err != nil {
				return err
			}

			c, err := opts.Client(cmd.Flags())
			if err != nil {
				return err
			}

			ctx, cancel := opts.Context(cmd.Context())
			defer cancel()

			info, err := prefix.NewAPI(c).Get(ctx, pathValue(args[0]))
			if err != nil {
				return opts.Fail(fmt.Errorf("reading prefix %q: %w", args[0], err))
			}

			if w.Format().Structured() {
				return w.Object(info)
			}

			return w.Table(
				[]string{"identifier", "name", "version", "netmask", "status", "location"},
				[][]string{{
					info.ID,
					info.Name,
					strconv.Itoa(info.IPVersion),
					strconv.Itoa(info.NetworkMask),
					info.Status,
					prefixLocation(&info),
				}},
			)
		},
	}
}

// prefixLocation renders the one location a prefix sits in. A prefix belongs
// to a single location in the Engine even though the field is an array, so a
// column showing the first is the whole truth in every case the Engine
// produces, and "-o json" carries the array either way.
func prefixLocation(info *prefix.Info) string {
	if len(info.Locations) == 0 {
		return ""
	}

	return info.Locations[0].Code
}

package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	corev1 "go.anx.io/go-anxcloud/pkg/apis/core/v1"
	"go.anx.io/go-anxcloud/pkg/core/tags"

	"github.com/ProbstenHias/anexia-cli/internal/confirm"
	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/output"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// newCoreTagCommand builds "core tag". Tags have no object in go-anxcloud's
// generic pkg/apis tree, so these commands drive the legacy core/tags client
// directly rather than going through the resource registry.
func newCoreTagCommand(opts *globalOptions) *cobra.Command {
	return resource.Group("tag", "Work with Anexia tags",
		newCoreTagListCommand(opts),
		newCoreTagGetCommand(opts),
		newCoreTagCreateCommand(opts),
		newCoreTagDeleteCommand(opts),
	)
}

func newCoreTagListCommand(opts *globalOptions) *cobra.Command {
	var (
		page         int
		limit        int
		query        string
		service      string
		organization string
		order        string
		descending   bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tags",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if page < 1 {
				return errmap.Usagef("--page %d must be 1 or greater", page)
			}

			if limit < 1 || limit > resource.MaxLimit {
				return errmap.Usagef("--limit %d must be between 1 and %d", limit, resource.MaxLimit)
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

			// The legacy client names its last parameter sortAscending but
			// sends it as sort_descending, so it is inverted here to make the
			// flag mean what it says.
			found, err := tags.NewAPI(c).List(ctx, page, limit, query, service, organization, order, descending)
			if err != nil {
				return opts.Fail(fmt.Errorf("listing tags: %w", err))
			}

			return renderTagSummaries(cmd, w, found)
		},
	}

	flags := cmd.Flags()
	flags.IntVar(&page, "page", 1, "page number to fetch")
	flags.IntVar(&limit, "limit", 50, "maximum number of tags per page")
	flags.StringVar(&query, "query", "", "filter by tag name")
	flags.StringVar(&service, "service", "", "filter by service identifier")
	flags.StringVar(&organization, "organization", "", "filter by organization identifier")
	flags.StringVar(&order, "order", "", "field to order by")
	flags.BoolVar(&descending, "descending", false, "reverse the sort order")

	return cmd
}

func newCoreTagGetCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Show one tag",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			info, err := tags.NewAPI(c).Get(ctx, args[0])
			if err != nil {
				return opts.Fail(fmt.Errorf("reading tag %q: %w", args[0], err))
			}

			if w.Format().Structured() {
				return w.Object(info)
			}

			rows := make([][]string, 0, len(info.Organisations))
			for i := range info.Organisations {
				o := &info.Organisations[i]
				rows = append(rows, []string{info.Name, info.Identifier, o.Service.Name, o.Customer.Name})
			}

			if len(rows) == 0 {
				rows = append(rows, []string{info.Name, info.Identifier, "", ""})
			}

			return w.Table([]string{"name", "identifier", "service", "customer"}, rows)
		},
	}
}

func newCoreTagCreateCommand(opts *globalOptions) *cobra.Command {
	var (
		name         string
		service      string
		organization string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a tag",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name == "" {
				return errmap.Usagef("--name is required")
			}

			if service == "" {
				return errmap.Usagef("--service is required")
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

			created, err := tags.NewAPI(c).Create(ctx, tags.Create{
				Name:       name,
				ServiceID:  service,
				CustomerID: organization,
			})
			if err != nil {
				return opts.Fail(fmt.Errorf("creating tag %q: %w", name, err))
			}

			if w.Format().Structured() {
				return w.Object(created)
			}

			return w.Table([]string{"name", "identifier"}, [][]string{{created.Name, created.Identifier}})
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&name, "name", "", "tag name")
	flags.StringVar(&service, "service", "", "service identifier the tag belongs to")
	flags.StringVar(&organization, "organization", "", "organization identifier to assign the tag to")

	return cmd
}

func newCoreTagDeleteCommand(opts *globalOptions) *cobra.Command {
	var service string

	cmd := &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"destroy"},
		Short:   "Delete a tag",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if service == "" {
				return errmap.Usagef("--service is required")
			}

			question := fmt.Sprintf("delete tag %q", args[0])
			if err := confirm.Prompt(cmd.InOrStdin(), cmd.ErrOrStderr(), question, opts.AssumeYes()); err != nil {
				return err
			}

			c, err := opts.Client(cmd.Flags())
			if err != nil {
				return err
			}

			ctx, cancel := opts.Context(cmd.Context())
			defer cancel()

			if err := tags.NewAPI(c).Delete(ctx, args[0], service); err != nil {
				return opts.Fail(fmt.Errorf("deleting tag %q: %w", args[0], err))
			}

			_, err = fmt.Fprintf(cmd.ErrOrStderr(), "deleted tag %s\n", args[0])

			return err
		},
	}

	cmd.Flags().StringVar(&service, "service", "", "service identifier the tag belongs to")

	return cmd
}

// renderTagSummaries writes a tag list in the writer's format.
func renderTagSummaries(cmd *cobra.Command, w *output.Writer, found []tags.Summary) error {
	if w.Format().Structured() {
		if found == nil {
			found = []tags.Summary{}
		}

		return w.Object(found)
	}

	rows := make([][]string, 0, len(found))
	for i := range found {
		rows = append(rows, []string{found[i].Name, found[i].Identifier})
	}

	if err := w.Table([]string{"name", "identifier"}, rows); err != nil {
		return err
	}

	if len(found) == 0 {
		_, err := fmt.Fprintln(cmd.ErrOrStderr(), "no tags found")

		return err
	}

	return nil
}

// newCoreResourceTagCommand builds "core resource tag", the sub-noun that
// labels an existing resource. It uses the generic client's tag helpers, which
// handle the Engine's retry and already-tagged quirks.
func newCoreResourceTagCommand(opts *globalOptions) *cobra.Command {
	return resource.Group("tag", "Manage the tags of a resource",
		newCoreResourceTagListCommand(opts),
		newCoreResourceTagAddCommand(opts),
		newCoreResourceTagRemoveCommand(opts),
	)
}

func newCoreResourceTagListCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list <resource-id>",
		Short: "List the tags of a resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := opts.Writer(cmd.OutOrStdout())
			if err != nil {
				return err
			}

			a, err := opts.API(cmd.Flags())
			if err != nil {
				return err
			}

			ctx, cancel := opts.Context(cmd.Context())
			defer cancel()

			found, err := corev1.ListTags(ctx, a, &corev1.Resource{Identifier: args[0]})
			if err != nil {
				return opts.Fail(fmt.Errorf("listing tags of resource %q: %w", args[0], err))
			}

			if w.Format().Structured() {
				if found == nil {
					found = []string{}
				}

				return w.Object(found)
			}

			rows := make([][]string, 0, len(found))
			for _, name := range found {
				rows = append(rows, []string{name})
			}

			if err := w.Table([]string{"name"}, rows); err != nil {
				return err
			}

			if len(rows) == 0 {
				_, err := fmt.Fprintln(cmd.ErrOrStderr(), "no tags found")

				return err
			}

			return nil
		},
	}
}

func newCoreResourceTagAddCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "add <resource-id> <tag>...",
		Short: "Add tags to a resource",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := opts.API(cmd.Flags())
			if err != nil {
				return err
			}

			ctx, cancel := opts.Context(cmd.Context())
			defer cancel()

			if err := corev1.Tag(ctx, a, &corev1.Resource{Identifier: args[0]}, args[1:]...); err != nil {
				return opts.Fail(fmt.Errorf("tagging resource %q: %w", args[0], err))
			}

			_, err = fmt.Fprintf(cmd.ErrOrStderr(), "tagged resource %s\n", args[0])

			return err
		},
	}
}

func newCoreResourceTagRemoveCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <resource-id> <tag>...",
		Aliases: []string{"delete"},
		Short:   "Remove tags from a resource",
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := opts.API(cmd.Flags())
			if err != nil {
				return err
			}

			ctx, cancel := opts.Context(cmd.Context())
			defer cancel()

			if err := corev1.Untag(ctx, a, &corev1.Resource{Identifier: args[0]}, args[1:]...); err != nil {
				return opts.Fail(fmt.Errorf("untagging resource %q: %w", args[0], err))
			}

			_, err = fmt.Fprintf(cmd.ErrOrStderr(), "untagged resource %s\n", args[0])

			return err
		},
	}
}

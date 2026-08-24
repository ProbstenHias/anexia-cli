package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
	corev1 "go.anx.io/go-anxcloud/pkg/apis/core/v1"
	"go.anx.io/go-anxcloud/pkg/core/tags"

	"github.com/ProbstenHias/anexia-cli/internal/confirm"
	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// queryValue escapes a value destined for a legacy client's query string.
//
// The legacy clients build their URLs with fmt.Sprintf and no escaping, so a
// filter containing a space fails the request and one containing an ampersand
// injects further parameters. The generic client escapes for itself, which is
// why only the hand-written commands need this.
func queryValue(v string) string {
	return url.QueryEscape(v)
}

// pathValue escapes an identifier destined for a legacy client's URL path.
//
// Unescaped, a question mark in an identifier ends the path and the rest
// becomes query parameters, so the request addresses a different object than
// the user named and can override the flags they passed. On a delete that
// means removing the wrong tag and reporting success. Query escaping is not
// interchangeable here: it encodes a space as a plus, which a path reader takes
// literally.
func pathValue(v string) string {
	return url.PathEscape(v)
}

// pathValues escapes each value for its own path segment.
func pathValues(vs []string) []string {
	escaped := make([]string, 0, len(vs))
	for _, v := range vs {
		escaped = append(escaped, pathValue(v))
	}

	return escaped
}

// validateRelation rejects a resource identifier or tag name that addresses no
// object. Both go into the URL path, and go-anxcloud builds that path from the
// object's fields, so the CLI has to refuse here: by the time the library sees
// them a relative segment has already been normalized away, which turned
// "tag remove r-1 .." into a delete against the resource itself.
func validateRelation(resourceID string, names []string) error {
	if err := resource.ValidateIdentifier("resource", resourceID); err != nil {
		return err
	}

	for _, name := range names {
		if err := resource.ValidateIdentifier("tag", name); err != nil {
			return err
		}
	}

	return nil
}

// newCoreTagCommand builds "core tag". Tags have no object in go-anxcloud's
// generic pkg/apis tree, so these commands drive the legacy core/tags client
// directly rather than going through the resource registry.
func newCoreTagCommand(opts *globalOptions) *cobra.Command {
	return resource.Noun("tag", "tags", "Work with Anexia tags",
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
		all          bool
		name         string
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

			// The legacy client names its last parameter sortAscending but
			// sends it verbatim as sort_descending, so --descending is passed
			// straight through and the flag means what it says.
			a := tags.NewAPI(c)

			found, err := resource.FetchPages(cmd.ErrOrStderr(), "tags", page, limit, all, func(p int) ([]tags.Summary, error) {
				return a.List(ctx, p, limit,
					queryValue(name), queryValue(service), queryValue(organization), queryValue(order),
					descending)
			})
			if err != nil {
				return opts.Fail(fmt.Errorf("listing tags: %w", err))
			}

			return resource.RenderList(cmd, w, "tags", found,
				[]string{"name", "identifier"},
				func(s *tags.Summary) []string {
					return []string{s.Name, s.Identifier}
				},
			)
		},
	}

	flags := cmd.Flags()
	resource.RegisterPagingFlags(flags, &page, &limit, &all, "tags")
	flags.StringVar(&name, "name", "", "filter by tag name")
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
			if err := resource.ValidateIdentifier("tag", args[0]); err != nil {
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

			info, err := tags.NewAPI(c).Get(ctx, pathValue(args[0]))
			if err != nil {
				return opts.Fail(fmt.Errorf("reading tag %q: %w", args[0], err))
			}

			if w.Format().Structured() {
				return w.Object(info)
			}

			// A tag is listed once per organisation it is assigned to. An
			// unassigned tag drops those two columns rather than padding
			// them, which would emit trailing empty fields in tsv.
			if len(info.Organisations) == 0 {
				return w.Table([]string{"name", "identifier"},
					[][]string{{info.Name, info.Identifier}})
			}

			rows := make([][]string, 0, len(info.Organisations))
			for i := range info.Organisations {
				o := &info.Organisations[i]
				rows = append(rows, []string{info.Name, info.Identifier, o.Service.Name, o.Customer.Name})
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
			if err := resource.ValidateIdentifier("tag", args[0]); err != nil {
				return err
			}

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

			if err := tags.NewAPI(c).Delete(ctx, pathValue(args[0]), queryValue(service)); err != nil {
				return opts.Fail(fmt.Errorf("deleting tag %q: %w", args[0], err))
			}

			_, err = fmt.Fprintf(cmd.ErrOrStderr(), "deleted tag %s\n", args[0])

			return err
		},
	}

	cmd.Flags().StringVar(&service, "service", "", "service identifier the tag belongs to")

	return cmd
}

// newCoreResourceTagCommand builds "core resource tag", the sub-noun that
// labels an existing resource. It uses the generic client's tag helpers, which
// handle the Engine's retry and already-tagged quirks.
func newCoreResourceTagCommand(opts *globalOptions) *cobra.Command {
	return resource.Noun("tag", "tags", "Manage the tags of a resource",
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
			if err := resource.ValidateIdentifier("resource", args[0]); err != nil {
				return err
			}

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

			return resource.RenderList(cmd, w, "tags", found,
				[]string{"name"},
				func(name *string) []string {
					return []string{*name}
				},
			)
		},
	}
}

func newCoreResourceTagAddCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "add <resource-id> <tag>...",
		Short: "Add tags to a resource",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateRelation(args[0], args[1:]); err != nil {
				return err
			}

			a, err := opts.API(cmd.Flags())
			if err != nil {
				return err
			}

			ctx, cancel := opts.Context(cmd.Context())
			defer cancel()

			if err := corev1.Tag(ctx, a, &corev1.Resource{Identifier: pathValue(args[0])}, pathValues(args[1:])...); err != nil {
				return opts.Fail(fmt.Errorf("tagging resource %q: %w", args[0], err))
			}

			_, err = fmt.Fprintf(cmd.ErrOrStderr(), "tagged resource %s\n", args[0])

			return err
		},
	}
}

func newCoreResourceTagRemoveCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		// No delete alias: removing a tag from a resource does not delete
		// the tag, and "core tag delete" is the command that does.
		Use:   "remove <resource-id> <tag>...",
		Short: "Remove tags from a resource",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateRelation(args[0], args[1:]); err != nil {
				return err
			}

			a, err := opts.API(cmd.Flags())
			if err != nil {
				return err
			}

			ctx, cancel := opts.Context(cmd.Context())
			defer cancel()

			if err := corev1.Untag(ctx, a, &corev1.Resource{Identifier: pathValue(args[0])}, pathValues(args[1:])...); err != nil {
				return opts.Fail(fmt.Errorf("untagging resource %q: %w", args[0], err))
			}

			_, err = fmt.Fprintf(cmd.ErrOrStderr(), "untagged resource %s\n", args[0])

			return err
		},
	}
}

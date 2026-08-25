package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.anx.io/go-anxcloud/pkg/ipam/address"
	"go.anx.io/go-anxcloud/pkg/utils/param"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// newNetworkAddressCommand builds "network address". Addresses have no object
// in go-anxcloud's generic pkg/apis tree, so these commands drive the legacy
// ipam/address client directly, sharing the paging and rendering helpers.
//
// The library implements create, update, delete and ReserveRandom here as
// well. They are left for the change that gives the resource registry its
// write verbs, so the two halves of the CLI keep offering the same verbs.
func newNetworkAddressCommand(opts *globalOptions) *cobra.Command {
	return resource.Noun("address", "addresses", "Work with Anexia IP addresses",
		newNetworkAddressListCommand(opts),
		newNetworkAddressGetCommand(opts),
	)
}

// addressFilters registers the fields the Engine's filtered address endpoint
// accepts and returns the ones the user set. Names follow the field rather
// than the query parameter, so --role sets role_text and --organization sets
// organization_identifier.
func addressFilters(flags *pflag.FlagSet) func() ([]param.Parameter, error) {
	var (
		prefixID     string
		vlan         string
		version      int
		role         string
		status       string
		location     string
		organization string
	)

	flags.StringVar(&prefixID, "prefix", "", "only list addresses in this prefix")
	flags.StringVar(&vlan, "vlan", "", "only list addresses in this VLAN")
	flags.IntVar(&version, "version", 0, "only list addresses of this IP version, 4 or 6")
	flags.StringVar(&role, "role", "", "only list addresses in this role")
	flags.StringVar(&status, "status", "", "only list addresses in this status")
	flags.StringVar(&location, "location", "", "only list addresses in this location")
	flags.StringVar(&organization, "organization", "", "only list addresses of this organization")

	return func() ([]param.Parameter, error) {
		// The Engine takes the version as a number and answers a value it
		// does not know with every address rather than an error, so a typo
		// would silently look like no filter at all. Whether the flag was
		// passed decides this rather than its value, because zero is a
		// value the user can type and is no more a version than five is.
		if flags.Changed("version") && version != 4 && version != 6 {
			return nil, errmap.Usagef("--version %d must be 4 or 6", version)
		}

		set := []struct {
			value string
			build func(string) param.Parameter
		}{
			{prefixID, address.PrefixFilter},
			{vlan, address.VlanFilter},
			{versionValue(version), address.VersionFilter},
			{role, address.RoleTextFilter},
			{status, address.StatusFilter},
			{location, address.LocationFilter},
			{organization, address.OrganizationFilter},
		}

		filters := make([]param.Parameter, 0, len(set))

		for _, f := range set {
			if f.value != "" {
				filters = append(filters, f.build(f.value))
			}
		}

		return filters, nil
	}
}

// versionValue renders an IP version. There is no version 0, so the zero a
// dropped Engine field or an unset flag decodes to renders as empty: as a
// filter it is then left out of the request, and in a table it says the Engine
// did not send one rather than claiming a version that cannot exist.
func versionValue(version int) string {
	if version == 0 {
		return ""
	}

	return strconv.Itoa(version)
}

func newNetworkAddressListCommand(opts *globalOptions) *cobra.Command {
	var (
		page   int
		limit  int
		all    bool
		search string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List addresses",
		Args:  cobra.NoArgs,
	}

	flags := cmd.Flags()
	resource.RegisterPagingFlags(flags, &page, &limit, &all, "addresses")
	flags.StringVar(&search, "search", "", "only list addresses matching this term")

	buildFilters := addressFilters(flags)

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := resource.ValidatePaging(page, limit, all); err != nil {
			return err
		}

		filters, err := buildFilters()
		if err != nil {
			return err
		}

		// Free-text search and field filters are two different Engine
		// endpoints, and neither accepts the other's parameters. Picking
		// one silently would drop the other and report success, so this
		// says which combination cannot be served.
		if search != "" && len(filters) > 0 {
			return errmap.Usagef("--search cannot be combined with the field filters, because the Engine serves them from different endpoints")
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

		a := address.NewAPI(c)

		// The search endpoint escapes its term itself, and the filtered
		// one builds a url.Values, so neither wants a value escaped here.
		found, err := resource.FetchPages(cmd.ErrOrStderr(), "addresses", page, limit, all, func(p int) ([]address.Summary, error) {
			if len(filters) > 0 {
				return a.GetFiltered(ctx, p, limit, filters...)
			}

			return a.List(ctx, p, limit, search)
		})
		if err != nil {
			return opts.Fail(fmt.Errorf("listing addresses: %w", err))
		}

		return resource.RenderList(cmd, w, "addresses", found,
			[]string{"identifier", "name", "role", "description"},
			func(s *address.Summary) []string {
				return []string{s.ID, s.Name, s.Role, s.DescriptionCustomer}
			},
		)
	}

	return cmd
}

func newNetworkAddressGetCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Show one address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := resource.ValidateIdentifier("address", args[0]); err != nil {
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

			info, err := address.NewAPI(c).Get(ctx, pathValue(args[0]))
			if err != nil {
				return opts.Fail(fmt.Errorf("reading address %q: %w", args[0], err))
			}

			if w.Format().Structured() {
				return w.Object(info)
			}

			// Four columns, per the column budget in docs/cli-design.md.
			// The VLAN and prefix an address sits in are one "-o json"
			// away, and are less use at a glance than what it is.
			return w.Table(
				[]string{"identifier", "name", "version", "status"},
				[][]string{{
					info.ID,
					info.Name,
					versionValue(info.Version),
					info.Status,
				}},
			)
		},
	}
}

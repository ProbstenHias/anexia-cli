package cli

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.anx.io/go-anxcloud/pkg/ipam/address"
	"go.anx.io/go-anxcloud/pkg/utils/param"

	"github.com/ProbstenHias/anexia-cli/internal/confirm"
	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/output"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// newNetworkAddressCommand builds "network address". Addresses have no object
// in go-anxcloud's generic pkg/apis tree, so these commands drive the legacy
// ipam/address client directly, sharing the paging and rendering helpers.
//
// The write verbs drive the same legacy client, using its Create, Update, and
// ReserveRandom types as the Engine payload contract.
func newNetworkAddressCommand(opts *globalOptions) *cobra.Command {
	return resource.Noun("address", "addresses", "Work with Anexia IP addresses",
		newNetworkAddressListCommand(opts),
		newNetworkAddressGetCommand(opts),
		newNetworkAddressCreateCommand(opts),
		newNetworkAddressUpdateCommand(opts),
		newNetworkAddressDeleteCommand(opts),
		newNetworkAddressReserveCommand(opts),
	)
}

var addressColumns = []string{"identifier", "name", "role", "description"}

func addressRow(s *address.Summary) []string {
	return []string{s.ID, s.Name, s.Role, s.DescriptionCustomer}
}

func renderAddressSummary(w *output.Writer, s *address.Summary) error {
	if w.Format().Structured() {
		return w.Object(s)
	}

	return w.Table(addressColumns, [][]string{addressRow(s)})
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

		return resource.RenderList(cmd, w, "addresses", found, addressColumns, addressRow)
	}

	return cmd
}

type addressCreateFlags struct {
	prefix       string
	address      string
	description  string
	role         string
	organization string
	rdns         string
}

func (f *addressCreateFlags) register(flags *pflag.FlagSet) {
	flags.StringVar(&f.prefix, "prefix", "", "prefix identifier the address belongs to")
	flags.StringVar(&f.address, "address", "", "IP address to create")
	flags.StringVar(&f.description, "description", "", "customer description")
	flags.StringVar(&f.role, "role", "Default", "address role")
	flags.StringVar(&f.organization, "organization", "", "organization identifier the address belongs to")
	flags.StringVar(&f.rdns, "rdns", "", "reverse DNS name")
}

func (f *addressCreateFlags) payload() (address.Create, error) {
	if f.prefix == "" {
		return address.Create{}, errmap.Usagef("--prefix is required")
	}

	if err := resource.ValidateIdentifier("prefix", f.prefix); err != nil {
		return address.Create{}, err
	}

	if f.address == "" {
		return address.Create{}, errmap.Usagef("--address is required")
	}

	if f.organization != "" {
		if err := resource.ValidateIdentifier("organization", f.organization); err != nil {
			return address.Create{}, err
		}
	}

	return address.Create{
		PrefixID:            f.prefix,
		Address:             f.address,
		DescriptionCustomer: f.description,
		Role:                f.role,
		Organization:        f.organization,
		RDNSName:            f.rdns,
	}, nil
}

func newNetworkAddressCreateCommand(opts *globalOptions) *cobra.Command {
	var f addressCreateFlags

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an address",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := f.payload()
			if err != nil {
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

			created, err := address.NewAPI(c).Create(ctx, body)
			if err != nil {
				return opts.Fail(fmt.Errorf("creating address: %w", err))
			}

			return renderAddressSummary(w, &created)
		},
	}

	f.register(cmd.Flags())

	return cmd
}

func newNetworkAddressUpdateCommand(opts *globalOptions) *cobra.Command {
	var description, role, rdns string

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := resource.ValidateIdentifier("address", args[0]); err != nil {
				return err
			}

			flags := cmd.Flags()
			if !flags.Changed("description") && !flags.Changed("role") && !flags.Changed("rdns") {
				return errmap.Usagef("nothing to update: pass at least one field to change")
			}

			if flags.Changed("description") && description == "" {
				return errmap.Usagef("--description cannot be emptied: go-anxcloud drops an empty description from the request")
			}

			if flags.Changed("role") && role == "" {
				return errmap.Usagef("--role cannot be emptied: go-anxcloud drops an empty role from the request")
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
			current, err := a.Get(ctx, pathValue(args[0]))
			if err != nil {
				return opts.Fail(fmt.Errorf("reading address %q: %w", args[0], err))
			}

			body := address.Update{RDNSName: current.RDNSName}
			if flags.Changed("description") {
				body.DescriptionCustomer = description
			}
			if flags.Changed("role") {
				body.Role = role
			}
			if flags.Changed("rdns") {
				body.RDNSName = rdns
			}

			updated, err := a.Update(ctx, pathValue(args[0]), body)
			if err != nil {
				return opts.Fail(fmt.Errorf("updating address %q: %w", args[0], err))
			}

			return renderAddressSummary(w, &updated)
		},
	}

	cmd.Flags().StringVar(&description, "description", "", "customer description")
	cmd.Flags().StringVar(&role, "role", "", "address role")
	cmd.Flags().StringVar(&rdns, "rdns", "", "reverse DNS name")

	return cmd
}

func newNetworkAddressDeleteCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"destroy"},
		Short:   "Delete an address",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := resource.ValidateIdentifier("address", args[0]); err != nil {
				return err
			}

			if err := confirm.Prompt(cmd.InOrStdin(), cmd.ErrOrStderr(), fmt.Sprintf("delete address %q", args[0]), opts.AssumeYes()); err != nil {
				return err
			}

			c, err := opts.Client(cmd.Flags())
			if err != nil {
				return err
			}

			ctx, cancel := opts.Context(cmd.Context())
			defer cancel()

			if err := address.NewAPI(c).Delete(ctx, pathValue(args[0])); err != nil {
				return opts.Fail(fmt.Errorf("deleting address %q: %w", args[0], err))
			}

			_, err = fmt.Fprintf(cmd.ErrOrStderr(), "deleted address %s\n", args[0])

			return err
		},
	}
}

type addressReserveFlags struct {
	location          string
	vlan              string
	count             int
	prefix            string
	version           int
	reservationPeriod time.Duration
}

func (f *addressReserveFlags) register(flags *pflag.FlagSet) {
	flags.StringVar(&f.location, "location", "", "location identifier to reserve addresses in")
	flags.StringVar(&f.vlan, "vlan", "", "VLAN identifier to reserve addresses in")
	flags.IntVar(&f.count, "count", 1, "number of addresses to reserve")
	flags.StringVar(&f.prefix, "prefix", "", "prefix identifier to reserve addresses from")
	flags.IntVar(&f.version, "version", 0, "IP version to reserve, 4 or 6")
	flags.DurationVar(&f.reservationPeriod, "reservation-period", 0, "how long to reserve addresses")
}

func (f *addressReserveFlags) payload(flags *pflag.FlagSet) (address.ReserveRandom, error) {
	if f.location == "" {
		return address.ReserveRandom{}, errmap.Usagef("--location is required")
	}

	if err := resource.ValidateIdentifier("location", f.location); err != nil {
		return address.ReserveRandom{}, err
	}

	if f.vlan == "" {
		return address.ReserveRandom{}, errmap.Usagef("--vlan is required")
	}

	if err := resource.ValidateIdentifier("vlan", f.vlan); err != nil {
		return address.ReserveRandom{}, err
	}

	if f.count < 1 {
		return address.ReserveRandom{}, errmap.Usagef("--count must be at least 1")
	}

	if flags.Changed("version") && f.version != 4 && f.version != 6 {
		return address.ReserveRandom{}, errmap.Usagef("--version %d must be 4 or 6", f.version)
	}

	if f.prefix != "" {
		if err := resource.ValidateIdentifier("prefix", f.prefix); err != nil {
			return address.ReserveRandom{}, err
		}
	}

	seconds := f.reservationPeriod / time.Second
	if flags.Changed("reservation-period") && seconds <= 0 {
		return address.ReserveRandom{}, errmap.Usagef("--reservation-period must be positive")
	}
	if strconv.IntSize == 32 && seconds > math.MaxUint32 {
		return address.ReserveRandom{}, errmap.Usagef("--reservation-period is too large")
	}

	// #nosec G115 -- seconds is positive and bounded to MaxUint32 on 32-bit platforms.
	period := uint(seconds)

	return address.ReserveRandom{
		LocationID:        f.location,
		VlanID:            f.vlan,
		Count:             f.count,
		PrefixID:          f.prefix,
		IPVersion:         address.IPReserveVersionLimit(f.version),
		ReservationPeriod: period,
	}, nil
}

func newNetworkAddressReserveCommand(opts *globalOptions) *cobra.Command {
	var f addressReserveFlags

	cmd := &cobra.Command{
		Use:   "reserve",
		Short: "Reserve random addresses",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := f.payload(cmd.Flags())
			if err != nil {
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

			reserved, err := address.NewAPI(c).ReserveRandom(ctx, body)
			if err != nil {
				return opts.Fail(fmt.Errorf("reserving addresses: %w", err))
			}

			if w.Format().Structured() {
				return w.Object(reserved)
			}

			return resource.RenderList(cmd, w, "reserved addresses", reserved.Data,
				[]string{"identifier", "address", "prefix"},
				func(ip *address.ReservedIP) []string {
					return []string{ip.ID, ip.Address, ip.Prefix}
				},
			)
		},
	}

	f.register(cmd.Flags())

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

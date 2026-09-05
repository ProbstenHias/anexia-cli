package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.anx.io/go-anxcloud/pkg/ipam/prefix"

	"github.com/ProbstenHias/anexia-cli/internal/confirm"
	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/output"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// newNetworkPrefixCommand builds "network prefix". Prefixes have no object in
// go-anxcloud's generic pkg/apis tree, so these commands drive the legacy
// ipam/prefix client directly, sharing the paging and rendering helpers so the
// result is indistinguishable from a Spec-driven noun.
//
// The write verbs drive the same legacy client. Its Create struct is the
// contract for what the Engine accepts, so the create flags mirror it field for
// field and nothing else is sent. Update offers --description only; see
// newNetworkPrefixUpdateCommand for why its Name field is left out.
func newNetworkPrefixCommand(opts *globalOptions) *cobra.Command {
	return resource.Noun("prefix", "prefixes", "Work with Anexia IP prefixes",
		newNetworkPrefixListCommand(opts),
		newNetworkPrefixGetCommand(opts),
		newNetworkPrefixCreateCommand(opts),
		newNetworkPrefixUpdateCommand(opts),
		newNetworkPrefixDeleteCommand(opts),
	)
}

// prefixColumns is the projection list, create and update share: the Engine
// answers the two writes with the list summary rather than the full object.
var prefixColumns = []string{"identifier", "name", "description"}

func prefixRow(s *prefix.Summary) []string {
	return []string{s.ID, s.Name, s.CustomerDescription}
}

// renderPrefixSummary shows the Engine's answer to a write with the same
// columns as list, or the whole object when the output is structured.
func renderPrefixSummary(w *output.Writer, s prefix.Summary) error {
	if w.Format().Structured() {
		return w.Object(s)
	}

	return w.Table(prefixColumns, [][]string{prefixRow(&s)})
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

			return resource.RenderList(cmd, w, "prefixes", found, prefixColumns, prefixRow)
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

			// Four columns, per the column budget in docs/cli-design.md.
			// The netmask is already the tail of the name the Engine
			// sends, and the location is one "-o json" away.
			return w.Table(
				[]string{"identifier", "name", "version", "status"},
				[][]string{{
					info.ID,
					info.Name,
					versionValue(info.IPVersion),
					info.Status,
				}},
			)
		},
	}
}

// prefixCreateFlags holds what "prefix create" collects. The Engine takes the
// type as a number; the user types the word.
type prefixCreateFlags struct {
	location        string
	version         int
	netmask         int
	prefixType      string
	vlan            string
	newVLAN         bool
	vlanDescription string
	createEmpty     bool
	redundancy      bool
	vmProvisioning  bool
	description     string
	organization    string
}

func (f *prefixCreateFlags) register(flags *pflag.FlagSet) {
	flags.StringVar(&f.location, "location", "", "location identifier to create the prefix in")
	flags.IntVar(&f.version, "version", 0, "IP version of the prefix, 4 or 6")
	flags.IntVar(&f.netmask, "netmask", 0, "prefix length, 0 to 32 for IPv4 or 0 to 128 for IPv6")
	flags.StringVar(&f.prefixType, "type", "", "prefix type, public or private")
	flags.StringVar(&f.vlan, "vlan", "", "identifier of an existing VLAN to attach the prefix to")
	flags.BoolVar(&f.newVLAN, "new-vlan", false, "create a new VLAN for the prefix instead of naming one with --vlan")
	flags.StringVar(&f.vlanDescription, "vlan-description", "", "customer description of the VLAN created with --new-vlan")
	flags.BoolVar(&f.createEmpty, "create-empty", false, "create only the infrastructure addresses (network, broadcast and router for IPv4) instead of every address of the prefix")
	flags.BoolVar(&f.redundancy, "router-redundancy", false, "enable router redundancy")
	flags.BoolVar(&f.vmProvisioning, "vm-provisioning", false, "allow virtual machines to be provisioned into the prefix")
	flags.StringVar(&f.description, "description", "", "customer description")
	flags.StringVar(&f.organization, "organization", "", "organization identifier the prefix belongs to")
}

// payload validates the flags and builds the legacy create body. Every check
// runs before anything is sent, so a usage error never reaches the Engine.
func (f *prefixCreateFlags) payload(flags *pflag.FlagSet) (prefix.Create, error) {
	if f.location == "" {
		return prefix.Create{}, errmap.Usagef("--location is required")
	}

	if !flags.Changed("version") {
		return prefix.Create{}, errmap.Usagef("--version is required")
	}

	if f.version != 4 && f.version != 6 {
		return prefix.Create{}, errmap.Usagef("--version %d must be 4 or 6", f.version)
	}

	if !flags.Changed("netmask") {
		return prefix.Create{}, errmap.Usagef("--netmask is required")
	}

	if f.netmask < 0 || f.netmask > prefixMaxNetmask(f.version) {
		return prefix.Create{}, errmap.Usagef("--netmask %d must be between 0 and %d for IPv%d", f.netmask, prefixMaxNetmask(f.version), f.version)
	}

	prefixType, err := prefixTypeValue(f.prefixType)
	if err != nil {
		return prefix.Create{}, err
	}

	if f.vlan != "" && f.newVLAN {
		return prefix.Create{}, errmap.Usagef("--vlan and --new-vlan cannot be combined: attach to an existing VLAN or create one")
	}

	if f.vlan == "" && !f.newVLAN {
		return prefix.Create{}, errmap.Usagef("--vlan or --new-vlan is required: a prefix lives in a VLAN")
	}

	if flags.Changed("vlan-description") && !f.newVLAN {
		return prefix.Create{}, errmap.Usagef("--vlan-description requires --new-vlan: it describes the VLAN created with the prefix")
	}

	if f.vlan != "" {
		if err := resource.ValidateIdentifier("vlan", f.vlan); err != nil {
			return prefix.Create{}, err
		}
	}

	return prefix.Create{
		Location:                f.location,
		IPVersion:               f.version,
		Type:                    prefixType,
		NetworkMask:             f.netmask,
		CreateVLAN:              f.newVLAN,
		CreateEmpty:             f.createEmpty,
		VLANID:                  f.vlan,
		EnableRedundancy:        f.redundancy,
		EnableVMProvisioning:    f.vmProvisioning,
		CustomerDescription:     f.description,
		CustomerVLANDescription: f.vlanDescription,
		Organization:            f.organization,
	}, nil
}

func prefixMaxNetmask(version int) int {
	if version == 6 {
		return 128
	}

	return 32
}

// prefixTypeValue maps the word the user types to the number the Engine takes.
func prefixTypeValue(v string) (int, error) {
	switch v {
	case "":
		return 0, errmap.Usagef("--type is required")
	case "public":
		return prefix.TypePublic, nil
	case "private":
		return prefix.TypePrivate, nil
	default:
		return 0, errmap.Usagef("--type %q must be public or private", v)
	}
}

func newNetworkPrefixCreateCommand(opts *globalOptions) *cobra.Command {
	var f prefixCreateFlags

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a prefix",
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

			created, err := prefix.NewAPI(c).Create(ctx, body)
			if err != nil {
				return opts.Fail(fmt.Errorf("creating prefix: %w", err))
			}

			return renderPrefixSummary(w, created)
		},
	}

	f.register(cmd.Flags())

	return cmd
}

// newNetworkPrefixUpdateCommand offers exactly the description. The name is
// the CIDR the Engine assigns, and location, version, type and netmask are
// fixed at creation, so none of them gets a flag.
//
// The legacy Update body drops every empty field, so the Engine only sees the
// fields named here and keeps the rest. That is the promise the registry's
// read-then-write update makes, without the read. The same omitempty means an
// emptied description would silently not be sent, so it is refused, as on vlan.
func newNetworkPrefixUpdateCommand(opts *globalOptions) *cobra.Command {
	var description string

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a prefix",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := resource.ValidateIdentifier("prefix", args[0]); err != nil {
				return err
			}

			if !cmd.Flags().Changed("description") {
				return errmap.Usagef("nothing to update: pass at least one field to change")
			}

			if description == "" {
				return errmap.Usagef("--description cannot be emptied: go-anxcloud drops an empty description from the request")
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

			updated, err := prefix.NewAPI(c).Update(ctx, pathValue(args[0]), prefix.Update{
				CustomerDescription: description,
			})
			if err != nil {
				return opts.Fail(fmt.Errorf("updating prefix %q: %w", args[0], err))
			}

			return renderPrefixSummary(w, updated)
		},
	}

	cmd.Flags().StringVar(&description, "description", "", "customer description")

	return cmd
}

func newNetworkPrefixDeleteCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"destroy"},
		Short:   "Delete a prefix",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := resource.ValidateIdentifier("prefix", args[0]); err != nil {
				return err
			}

			question := fmt.Sprintf("delete prefix %q", args[0])
			if err := confirm.Prompt(cmd.InOrStdin(), cmd.ErrOrStderr(), question, opts.AssumeYes()); err != nil {
				return err
			}

			c, err := opts.Client(cmd.Flags())
			if err != nil {
				return err
			}

			ctx, cancel := opts.Context(cmd.Context())
			defer cancel()

			if err := prefix.NewAPI(c).Delete(ctx, pathValue(args[0])); err != nil {
				return opts.Fail(fmt.Errorf("deleting prefix %q: %w", args[0], err))
			}

			_, err = fmt.Fprintf(cmd.ErrOrStderr(), "deleted prefix %s\n", args[0])

			return err
		},
	}
}

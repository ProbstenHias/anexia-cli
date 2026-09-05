package cli

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	corev1 "go.anx.io/go-anxcloud/pkg/apis/core/v1"
	vlanv1 "go.anx.io/go-anxcloud/pkg/apis/vlan/v1"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// newNetworkCommand builds the "network" group, covering the Engine's IP
// address management: the VLANs things are deployed into and the prefixes and
// addresses that live inside them.
func newNetworkCommand(opts *globalOptions) *cobra.Command {
	return resource.Group("network", "VLANs, prefixes, and IP addresses",
		newNetworkVlanCommand(opts),
		newNetworkPrefixCommand(opts),
		newNetworkAddressCommand(opts),
	)
}

// newNetworkVlanCommand builds "network vlan". VLANs are the one object in
// this group go-anxcloud models generically, so this is a Spec with every verb.
// prefix is hand-written against the legacy client with the same verbs, and
// address is read-only until its write verbs are declared.
func newNetworkVlanCommand(opts *globalOptions) *cobra.Command {
	return resource.Command(opts, resource.Spec[vlanv1.VLAN, *vlanv1.VLAN]{
		Noun:   "vlan",
		Short:  "Work with Anexia VLANs",
		List:   true,
		Get:    true,
		Delete: true,
		Identify: func(v *vlanv1.VLAN, id string) {
			v.Identifier = id
		},
		CreatePayload: vlanCreateFlags,
		UpdatePayload: vlanUpdateFlags,
		Filters: func(flags *pflag.FlagSet) func(*vlanv1.VLAN) {
			// Status and Locations are the only two fields go-anxcloud
			// marks filterable, and it refuses more than one location,
			// so --location takes a single identifier.
			status := flags.String("status", "", "only list VLANs in this status")
			location := flags.String("location", "", "only list VLANs in this location")

			return func(v *vlanv1.VLAN) {
				if *status != "" {
					v.Status = vlanv1.Status(*status)
				}

				if *location != "" {
					v.Locations = []corev1.Location{{Identifier: *location}}
				}
			}
		},
		Columns: []resource.Column[vlanv1.VLAN]{
			{Name: "identifier", Value: func(v *vlanv1.VLAN) string { return v.Identifier }},
			{Name: "name", Value: func(v *vlanv1.VLAN) string { return v.Name }},
			{Name: "status", Value: func(v *vlanv1.VLAN) string { return string(v.Status) }},
			{Name: "location", Value: vlanLocation},
		},
	})
}

// vlanFields registers the flags shared by vlan create and update, returning
// the values so each verb can decide which are required.
type vlanFields struct {
	location       *string
	description    *string
	vmProvisioning *bool
}

// registerVlanFields declares every VLAN payload flag except --location, which
// only create takes. go-anxcloud documents that the location cannot be changed
// through the API, so update does not pretend otherwise.
func registerVlanFields(flags *pflag.FlagSet) vlanFields {
	return vlanFields{
		description:    flags.String("description", "", "customer description of the VLAN"),
		vmProvisioning: flags.Bool("vm-provisioning", false, "allow virtual machines to be provisioned into the VLAN"),
	}
}

func vlanCreateFlags(flags *pflag.FlagSet) func(*vlanv1.VLAN) error {
	fields := registerVlanFields(flags)
	fields.location = flags.String("location", "", "location identifier the VLAN is created in")

	return func(v *vlanv1.VLAN) error {
		if *fields.location == "" {
			return errmap.Usagef("--location is required")
		}

		v.Locations = []corev1.Location{{Identifier: *fields.location}}
		v.DescriptionCustomer = *fields.description
		v.VMProvisioning = *fields.vmProvisioning

		return nil
	}
}

// vlanUpdateFlags applies only the flags the user actually set, so
// "--vm-provisioning=false" counts as a change rather than as the default.
//
// The Engine's VLAN update changes description_customer and vm_provisioning
// and nothing else (go-anxcloud's legacy vlan.UpdateDefinition is the
// contract); name, role and status are Engine-assigned and the location is
// fixed at creation. The read that precedes the write fills them all in, so
// those are cleared again rather than echoed back at an endpoint that does not
// take them. The identifier stays so the object still addresses the same VLAN.
func vlanUpdateFlags(flags *pflag.FlagSet) func(*vlanv1.VLAN) error {
	fields := registerVlanFields(flags)

	return func(v *vlanv1.VLAN) error {
		// go-anxcloud marks description_customer omitempty, so an empty
		// string never reaches the Engine and the "cleared" description
		// would be reported as a success that changed nothing.
		if flags.Changed("description") && *fields.description == "" {
			return errmap.Usagef("--description cannot be emptied: go-anxcloud drops an empty description from the request")
		}

		changed := 0

		for name, apply := range map[string]func(){
			"description":     func() { v.DescriptionCustomer = *fields.description },
			"vm-provisioning": func() { v.VMProvisioning = *fields.vmProvisioning },
		} {
			if flags.Changed(name) {
				apply()

				changed++
			}
		}

		if changed == 0 {
			return errmap.Usagef("nothing to update: pass at least one field to change")
		}

		*v = vlanv1.VLAN{
			Identifier:          v.Identifier,
			DescriptionCustomer: v.DescriptionCustomer,
			VMProvisioning:      v.VMProvisioning,
		}

		return nil
	}
}

// vlanLocation renders the one location a VLAN has. The Engine returns an
// array and go-anxcloud says there is no way to configure more than one, so
// anything past the first is not something a column can show honestly; the
// full array is one "-o json" away.
//
// The two endpoints disagree on where the site code lives. core/location fills
// "code" and gives "name" the long form, while the VLAN endpoint sends the code
// as "name" and no "code" at all. Both decode into the same struct, so the
// column takes whichever field arrived rather than picking one and rendering
// blank against the other.
//
// A create has neither: the Engine assigns the name and the user typed only a
// location identifier, so that is what a failed create is described by.
func vlanLocation(v *vlanv1.VLAN) string {
	if len(v.Locations) == 0 {
		return ""
	}

	if code := v.Locations[0].Code; code != "" {
		return code
	}

	if name := v.Locations[0].Name; name != "" {
		return name
	}

	return v.Locations[0].Identifier
}

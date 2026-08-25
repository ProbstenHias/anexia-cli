package cli

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	corev1 "go.anx.io/go-anxcloud/pkg/apis/core/v1"
	vlanv1 "go.anx.io/go-anxcloud/pkg/apis/vlan/v1"

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
// this group go-anxcloud models generically, so this is a Spec and the other
// two are not.
func newNetworkVlanCommand(opts *globalOptions) *cobra.Command {
	return resource.Command(opts, resource.Spec[vlanv1.VLAN, *vlanv1.VLAN]{
		Noun:  "vlan",
		Short: "Work with Anexia VLANs",
		List:  true,
		Get:   true,
		Identify: func(v *vlanv1.VLAN, id string) {
			v.Identifier = id
		},
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
func vlanLocation(v *vlanv1.VLAN) string {
	if len(v.Locations) == 0 {
		return ""
	}

	if code := v.Locations[0].Code; code != "" {
		return code
	}

	return v.Locations[0].Name
}

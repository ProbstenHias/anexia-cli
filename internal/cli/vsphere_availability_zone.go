package cli

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/client"
	"go.anx.io/go-anxcloud/pkg/vsphere/provisioning/availabilityzones"
	"go.anx.io/go-anxcloud/pkg/vsphere/provisioning/location"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// engineIdentifier matches the 32 hex digit identifiers the Engine assigns.
// Anything else given as --location is taken as a location code.
var engineIdentifier = regexp.MustCompile(`^[0-9a-f]{32}$`)

func newVSphereAvailabilityZoneCommand(opts *globalOptions) *cobra.Command {
	return resource.Noun("availability-zone", "availability-zones", "Work with vSphere availability zones",
		newVSphereAvailabilityZoneListCommand(opts),
	)
}

// newVSphereAvailabilityZoneListCommand lists the zones of one location with
// the CPU and disk categories each zone offers. The endpoint returns every
// zone in one response, so like the template list it has nothing to page.
func newVSphereAvailabilityZoneListCommand(opts *globalOptions) *cobra.Command {
	var page, limit int
	var all bool
	var locationRef string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List availability zones",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := resource.ValidatePaging(page, limit, all); err != nil {
				return err
			}
			if page > 1 {
				return resource.UnservablePageError("availability zones", page)
			}
			if locationRef == "" {
				return errmap.Usagef("--location is required")
			}
			if err := resource.ValidateIdentifier("location", locationRef); err != nil {
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
			zones, err := listAvailabilityZones(ctx, c, cmd.ErrOrStderr(), locationRef)
			if err != nil {
				return opts.Fail(fmt.Errorf("listing availability zones: %w", err))
			}
			return resource.RenderList(cmd, w, "availability zones", zones,
				[]string{"identifier", "name", "cpu-categories", "disk-categories"},
				func(z *availabilityzones.AvailabilityZone) []string {
					return []string{z.Identifier, z.Name, strings.Join(z.CpuCategories, ","), strings.Join(z.DiskCategories, ",")}
				})
		},
	}
	flags := cmd.Flags()
	resource.RegisterPagingFlags(flags, &page, &limit, &all, "availability zones")
	flags.StringVar(&locationRef, "location", "", "location identifier or code, such as ANX04")
	return cmd
}

func listAvailabilityZones(ctx context.Context, c client.Client, notices io.Writer, locationRef string) ([]availabilityzones.AvailabilityZone, error) {
	locationID, err := resolveLocation(ctx, c, notices, locationRef)
	if err != nil {
		return nil, err
	}
	return availabilityzones.NewAPI(c).List(ctx, pathValue(locationID))
}

// resolveLocation returns the identifier of the location named by an
// identifier or a code. A code is matched against every location, because the
// Engine's location_code filter does not find a location by its exact code.
func resolveLocation(ctx context.Context, c client.Client, notices io.Writer, ref string) (string, error) {
	if engineIdentifier.MatchString(ref) {
		return ref, nil
	}
	a := location.NewAPI(c)
	found, err := resource.FetchPages(notices, "locations", 1, resource.MaxLimit, true, func(p int) ([]location.Location, error) {
		return a.List(ctx, p, resource.MaxLimit, "", "")
	})
	if err != nil {
		return "", fmt.Errorf("resolving location code %q: %w", ref, err)
	}
	for _, l := range found {
		if strings.EqualFold(l.Code, ref) {
			return l.ID, nil
		}
	}
	return "", fmt.Errorf("%w: no location with code %q", api.ErrNotFound, ref)
}

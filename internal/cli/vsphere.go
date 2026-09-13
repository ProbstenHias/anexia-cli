package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	corev1 "go.anx.io/go-anxcloud/pkg/apis/core/v1"
	vspherev1 "go.anx.io/go-anxcloud/pkg/apis/vsphere/v1"
	"go.anx.io/go-anxcloud/pkg/vsphere/provisioning/disktype"
	"go.anx.io/go-anxcloud/pkg/vsphere/provisioning/location"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

func newVSphereCommand(opts *globalOptions) *cobra.Command {
	return resource.Group("vsphere", "vSphere provisioning resources",
		newVSphereLocationCommand(opts),
		newVSphereTemplateCommand(opts),
		newVSphereDiskTypeCommand(opts),
	)
}

func newVSphereLocationCommand(opts *globalOptions) *cobra.Command {
	return resource.Noun("location", "locations", "Work with vSphere locations",
		newVSphereLocationListCommand(opts),
	)
}

func newVSphereLocationListCommand(opts *globalOptions) *cobra.Command {
	var page, limit int
	var all bool
	var code, organization string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List locations",
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
			a := location.NewAPI(c)
			found, err := resource.FetchPages(cmd.ErrOrStderr(), "locations", page, limit, all, func(p int) ([]location.Location, error) {
				return a.List(ctx, p, limit, queryValue(code), queryValue(organization))
			})
			if err != nil {
				return opts.Fail(fmt.Errorf("listing locations: %w", err))
			}
			return resource.RenderList(cmd, w, "locations", found,
				[]string{"identifier", "code", "name", "country"},
				func(l *location.Location) []string { return []string{l.ID, l.Code, l.Name, locationCountry(l)} })
		},
	}
	flags := cmd.Flags()
	resource.RegisterPagingFlags(flags, &page, &limit, &all, "locations")
	flags.StringVar(&code, "code", "", "filter by location code")
	flags.StringVar(&organization, "organization", "", "filter by organization identifier")
	return cmd
}

func newVSphereTemplateCommand(opts *globalOptions) *cobra.Command {
	return resource.Command(opts, resource.Spec[vspherev1.Template, *vspherev1.Template]{
		Noun:  "template",
		Short: "Work with vSphere templates",
		List:  true,
		Get:   true,
		Identify: func(t *vspherev1.Template, id string) {
			t.Identifier = id
		},
		Scope: templateScope,
		Columns: []resource.Column[vspherev1.Template]{
			{Name: "identifier", Value: func(t *vspherev1.Template) string { return t.Identifier }},
			{Name: "name", Value: func(t *vspherev1.Template) string { return t.Name }},
			{Name: "build", Value: func(t *vspherev1.Template) string { return t.Build }},
			{Name: "bit", Value: func(t *vspherev1.Template) string { return t.Bit }},
		},
	})
}

func templateScope(flags *pflag.FlagSet) func(*vspherev1.Template) error {
	locationID := flags.String("location", "", "location identifier")
	templateType := flags.String("type", string(vspherev1.TypeTemplate), "template type: templates or from_scratch")

	return func(t *vspherev1.Template) error {
		if *locationID == "" {
			return errmap.Usagef("--location is required")
		}
		if err := resource.ValidateIdentifier("location", *locationID); err != nil {
			return err
		}
		if *templateType != string(vspherev1.TypeTemplate) && *templateType != string(vspherev1.TypeFromScratch) {
			return errmap.Usagef(`--type must be %q or %q`, vspherev1.TypeTemplate, vspherev1.TypeFromScratch)
		}

		t.Location = corev1.Location{Identifier: pathValue(*locationID)}
		t.Type = vspherev1.TemplateType(*templateType)

		return nil
	}
}

func newVSphereDiskTypeCommand(opts *globalOptions) *cobra.Command {
	return resource.Noun("disk-type", "disk-types", "Work with vSphere disk types",
		newVSphereDiskTypeListCommand(opts),
	)
}

func newVSphereDiskTypeListCommand(opts *globalOptions) *cobra.Command {
	var page, limit int
	var all bool
	var locationID string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List disk types",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := resource.ValidatePaging(page, limit, all); err != nil {
				return err
			}
			if locationID == "" {
				return errmap.Usagef("--location is required")
			}
			if err := resource.ValidateIdentifier("location", locationID); err != nil {
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
			a := disktype.NewAPI(c)
			found, err := resource.FetchPages(cmd.ErrOrStderr(), "disk types", page, limit, all, func(p int) ([]disktype.DiskType, error) {
				return a.List(ctx, pathValue(locationID), p, limit)
			})
			if err != nil {
				return opts.Fail(fmt.Errorf("listing disk types: %w", err))
			}
			return resource.RenderList(cmd, w, "disk types", found,
				[]string{"identifier", "storage-type", "bandwidth", "iops", "latency"},
				func(d *disktype.DiskType) []string {
					return []string{d.ID, d.StorageType, strconv.Itoa(d.Bandwidth), strconv.Itoa(d.IOPS), strconv.Itoa(d.Latency)}
				})
		},
	}
	flags := cmd.Flags()
	resource.RegisterPagingFlags(flags, &page, &limit, &all, "disk types")
	flags.StringVar(&locationID, "location", "", "location identifier")
	return cmd
}

func locationCountry(l *location.Location) string {
	if l.CountryName != "" {
		return l.CountryName
	}

	return l.Country
}

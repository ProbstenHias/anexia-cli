package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"go.anx.io/go-anxcloud/pkg/vsphere/provisioning/disktype"
	"go.anx.io/go-anxcloud/pkg/vsphere/provisioning/location"
	"go.anx.io/go-anxcloud/pkg/vsphere/provisioning/templates"

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
		Short: "List vSphere locations",
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
				[]string{"id", "code", "name", "country"},
				func(l *location.Location) []string { return []string{l.ID, l.Code, l.Name, l.CountryName} })
		},
	}
	flags := cmd.Flags()
	resource.RegisterPagingFlags(flags, &page, &limit, &all, "locations")
	flags.StringVar(&code, "code", "", "filter by location code")
	flags.StringVar(&organization, "organization", "", "filter by organization identifier")
	return cmd
}

func newVSphereTemplateCommand(opts *globalOptions) *cobra.Command {
	return resource.Noun("template", "templates", "Work with vSphere templates",
		newVSphereTemplateListCommand(opts),
	)
}

func newVSphereTemplateListCommand(opts *globalOptions) *cobra.Command {
	var page, limit int
	var all bool
	var locationID, templateType string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List vSphere templates",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := resource.ValidatePaging(page, limit, all); err != nil {
				return err
			}
			if locationID == "" {
				return errmap.Usagef("--location is required")
			}
			if templateType != templates.TemplateTypeTemplates && templateType != templates.TemplateTypeFromScratch {
				return errmap.Usagef(`--type must be %q or %q`, templates.TemplateTypeTemplates, templates.TemplateTypeFromScratch)
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
			a := templates.NewAPI(c)
			found, err := resource.FetchPages(cmd.ErrOrStderr(), "templates", page, limit, all, func(p int) ([]templates.Template, error) {
				return a.List(ctx, pathValue(locationID), templateType, p, limit)
			})
			if err != nil {
				return opts.Fail(fmt.Errorf("listing templates: %w", err))
			}
			return resource.RenderList(cmd, w, "templates", found,
				[]string{"id", "name", "build", "bit"},
				func(t *templates.Template) []string { return []string{t.ID, t.Name, t.Build, t.WordSize} })
		},
	}
	flags := cmd.Flags()
	resource.RegisterPagingFlags(flags, &page, &limit, &all, "templates")
	flags.StringVar(&locationID, "location", "", "location identifier")
	flags.StringVar(&templateType, "type", templates.TemplateTypeTemplates, "template type: templates or from_scratch")
	return cmd
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
		Short: "List vSphere disk types",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := resource.ValidatePaging(page, limit, all); err != nil {
				return err
			}
			if locationID == "" {
				return errmap.Usagef("--location is required")
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
				[]string{"id", "storage type", "bandwidth", "iops", "latency"},
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

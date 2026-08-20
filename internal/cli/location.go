package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.anx.io/go-anxcloud/pkg/vsphere/provisioning/location"

	"github.com/ProbstenHias/anexia-cli/internal/output"
)

// maxLimit is the largest page size the CLI accepts.
const maxLimit = 1000

func newLocationCommand(d Deps, opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "location",
		Short: "Work with vSphere provisioning locations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newLocationListCommand(d, opts))

	return cmd
}

func newLocationListCommand(d Deps, opts *globalOptions) *cobra.Command {
	var (
		page         int
		limit        int
		locationCode string
		organization string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List vSphere provisioning locations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if page < 1 {
				return fmt.Errorf("invalid --page %d: must be 1 or greater", page)
			}

			if limit < 1 || limit > maxLimit {
				return fmt.Errorf("invalid --limit %d: must be between 1 and %d", limit, maxLimit)
			}

			w, err := opts.writer(cmd.OutOrStdout())
			if err != nil {
				return err
			}

			c, err := opts.client(cmd.Flags())
			if err != nil {
				return err
			}

			ctx, cancel := opts.context(cmd.Context())
			defer cancel()

			locations, err := d.newLocationAPI(c).List(ctx, page, limit, locationCode, organization)
			if err != nil {
				return opts.timeoutHint(fmt.Errorf("listing locations: %w", err))
			}

			return renderLocations(cmd, w, locations)
		},
	}

	flags := cmd.Flags()
	flags.IntVar(&page, "page", 1, "page number to fetch")
	flags.IntVar(&limit, "limit", 50, "maximum number of locations per page")
	flags.StringVar(&locationCode, "location-code", "", "filter by location code")
	flags.StringVar(&organization, "organization", "", "filter by organization")

	return cmd
}

// renderLocations writes locations in the writer's format.
func renderLocations(cmd *cobra.Command, w *output.Writer, locations []location.Location) error {
	if w.Format() == output.FormatJSON {
		if locations == nil {
			locations = []location.Location{}
		}

		return w.JSON(locations)
	}

	rows := make([][]string, 0, len(locations))
	for i := range locations {
		l := &locations[i]
		rows = append(rows, []string{l.Code, l.Name, countryOf(l), l.ID})
	}

	if err := w.Table([]string{"code", "name", "country", "id"}, rows); err != nil {
		return err
	}

	if len(locations) == 0 {
		if _, err := fmt.Fprintln(cmd.ErrOrStderr(), "no locations found"); err != nil {
			return err
		}
	}

	return nil
}

// countryOf prefers the human-readable country name over the country code.
func countryOf(l *location.Location) string {
	if l.CountryName != "" {
		return l.CountryName
	}

	return l.Country
}

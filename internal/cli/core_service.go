package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.anx.io/go-anxcloud/pkg/core/service"

	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// newCoreServiceCommand builds "core service". go-anxcloud reaches services
// only through the legacy client and implements only a list there, so that is
// the one verb. Whether the Engine offers more is not something the library
// says.
func newCoreServiceCommand(opts *globalOptions) *cobra.Command {
	return resource.Noun("service", "services", "Work with Anexia services",
		newCoreServiceListCommand(opts),
	)
}

func newCoreServiceListCommand(opts *globalOptions) *cobra.Command {
	var (
		page  int
		limit int
		all   bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List services",
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

			a := service.NewAPI(c)

			found, err := resource.FetchPages(cmd.ErrOrStderr(), "services", page, limit, all, func(p int) ([]service.Service, error) {
				return a.List(ctx, p, limit)
			})
			if err != nil {
				return opts.Fail(fmt.Errorf("listing services: %w", err))
			}

			return resource.RenderList(cmd, w, "services", found,
				[]string{"identifier", "name", "title", "category"},
				func(s *service.Service) []string {
					return []string{s.ID, s.Name, s.Title, s.Category}
				},
			)
		},
	}

	resource.RegisterPagingFlags(cmd.Flags(), &page, &limit, &all, "services")

	return cmd
}

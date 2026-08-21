package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.anx.io/go-anxcloud/pkg/core/service"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// newCoreServiceCommand builds "core service". The Engine exposes services
// read-only and only in the legacy client, so this is a single list verb.
func newCoreServiceCommand(opts *globalOptions) *cobra.Command {
	return resource.Group("service", "Work with Anexia services",
		newCoreServiceListCommand(opts),
	)
}

func newCoreServiceListCommand(opts *globalOptions) *cobra.Command {
	var (
		page  int
		limit int
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List services",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if page < 1 {
				return errmap.Usagef("--page %d must be 1 or greater", page)
			}

			if limit < 1 || limit > resource.MaxLimit {
				return errmap.Usagef("--limit %d must be between 1 and %d", limit, resource.MaxLimit)
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

			found, err := service.NewAPI(c).List(ctx, page, limit)
			if err != nil {
				return opts.Fail(fmt.Errorf("listing services: %w", err))
			}

			if w.Format().Structured() {
				if found == nil {
					found = []service.Service{}
				}

				return w.Object(found)
			}

			rows := make([][]string, 0, len(found))
			for i := range found {
				s := &found[i]
				rows = append(rows, []string{s.ID, s.Name, s.Title, s.Category})
			}

			if err := w.Table([]string{"identifier", "name", "title", "category"}, rows); err != nil {
				return err
			}

			if len(found) == 0 {
				_, err := fmt.Fprintln(cmd.ErrOrStderr(), "no services found")

				return err
			}

			return nil
		},
	}

	flags := cmd.Flags()
	flags.IntVar(&page, "page", 1, "page number to fetch")
	flags.IntVar(&limit, "limit", 50, "maximum number of services per page")

	return cmd
}

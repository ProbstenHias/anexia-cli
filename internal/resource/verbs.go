package resource

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/api/types"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
)

// newListCommand builds "<noun> list", the paged read of many objects.
func newListCommand[O any, PO Pointer[O]](env Env, spec Spec[O, PO]) *cobra.Command {
	var (
		page  int
		limit int
		all   bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List " + spec.plural(),
		Args:  cobra.NoArgs,
	}

	flags := cmd.Flags()
	RegisterPagingFlags(flags, &page, &limit, spec.plural())
	flags.BoolVar(&all, "all", false, "fetch every page instead of just one")

	var applyFilters func(*O)
	if spec.Filters != nil {
		applyFilters = spec.Filters(flags)
	}

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := ValidatePaging(page, limit); err != nil {
			return err
		}

		w, err := env.Writer(cmd.OutOrStdout())
		if err != nil {
			return err
		}

		a, err := env.API(cmd.Flags())
		if err != nil {
			return err
		}

		ctx, cancel := env.Context(cmd.Context())
		defer cancel()

		var filter O
		if applyFilters != nil {
			applyFilters(&filter)
		}

		items, err := fetch(ctx, a, PO(&filter), page, limit, all)
		if err != nil {
			return env.Fail(fmt.Errorf("listing %s: %w", spec.plural(), err))
		}

		return render(cmd, w, spec, items)
	}

	return cmd
}

// fetch reads one page of objects, or every page from the requested one on
// when all is set.
//
// Each page is a fresh List with an explicit page number rather than a walk
// over types.PageInfo. PageInfo.Next replays the page List already fetched on
// its first call while still advancing its internal counter, so driving the
// loop with it skips a page whenever the caller starts past page one.
func fetch[O any, PO Pointer[O]](ctx context.Context, a api.API, filter PO, page, limit int, all bool) ([]O, error) {
	items := make([]O, 0, limit)

	for p := page; ; p++ {
		var info types.PageInfo
		if err := a.List(ctx, filter, api.Paged(uint(p), uint(limit), &info)); err != nil {
			return nil, err
		}

		var pageItems []O

		info.Next(&pageItems)

		if err := info.Error(); err != nil {
			return nil, err
		}

		items = append(items, pageItems...)

		// Stop on a short page as well as an empty one: an endpoint that
		// ignores the page parameter would otherwise loop until --timeout.
		if !all || len(pageItems) < limit {
			break
		}

		// A total page count is advisory, but when the Engine reports one
		// there is no reason to ask for anything beyond it.
		if total := info.TotalPages(); total > 0 && uint(p) >= total {
			break
		}
	}

	return items, nil
}

// newGetCommand builds "<noun> get <id>", the read of a single object.
func newGetCommand[O any, PO Pointer[O]](env Env, spec Spec[O, PO]) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Show one " + spec.Noun,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := env.Writer(cmd.OutOrStdout())
			if err != nil {
				return err
			}

			obj, err := spec.identify(args[0])
			if err != nil {
				return err
			}

			a, err := env.API(cmd.Flags())
			if err != nil {
				return err
			}

			ctx, cancel := env.Context(cmd.Context())
			defer cancel()

			if err := a.Get(ctx, obj); err != nil {
				return env.Fail(fmt.Errorf("reading %s %q: %w", spec.Noun, args[0], err))
			}

			return renderOne(w, spec, (*O)(obj))
		},
	}
}

// RegisterPagingFlags declares --page and --limit. Commands written against
// the legacy client call this too, so paging looks identical everywhere.
func RegisterPagingFlags(flags *pflag.FlagSet, page, limit *int, plural string) {
	flags.IntVar(page, "page", 1, "page number to fetch")
	flags.IntVar(limit, "limit", 50, "maximum number of "+plural+" per page")
}

// ValidatePaging rejects out-of-range paging flags. Commands written against
// the legacy client register the same flags by hand and share this check.
func ValidatePaging(page, limit int) error {
	if page < 1 {
		return errmap.Usagef("--page %d must be 1 or greater", page)
	}

	if limit < 1 || limit > MaxLimit {
		return errmap.Usagef("--limit %d must be between 1 and %d", limit, MaxLimit)
	}

	return nil
}

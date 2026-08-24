package resource

import (
	"context"
	"fmt"
	"io"
	"strings"

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
	RegisterPagingFlags(flags, &page, &limit, &all, spec.plural())

	var applyFilters func(*O)
	if spec.Filters != nil {
		applyFilters = spec.Filters(flags)
	}

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := ValidatePaging(page, limit, all); err != nil {
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

		items, err := fetch(ctx, a, cmd.ErrOrStderr(), PO(&filter), spec.plural(), page, limit, all)
		if err != nil {
			return env.Fail(fmt.Errorf("listing %s: %w", spec.plural(), err))
		}

		return render(cmd, w, spec, items)
	}

	return cmd
}

// maxPages bounds an --all walk. Nothing in the Engine's contract forces a
// paged endpoint to ever report a last page, so the walk needs a backstop that
// does not depend on the server behaving.
const maxPages = 1000

// pageLimitError reports a walk that hit the backstop. Narrowing the results
// always applies, and raising the limit only when there is room to raise it, so
// the advice is never something the reader has already done.
func pageLimitError(limit int) error {
	if limit < MaxLimit {
		return fmt.Errorf("gave up after %d pages of %d: narrow the results with a filter, or raise --limit (up to %d) to fetch more per request",
			maxPages, limit, MaxLimit)
	}

	return fmt.Errorf("gave up after %d pages of %d: narrow the results with a filter, or fetch pages individually with --page",
		maxPages, limit)
}

// fetch reads one page of objects, or every page from the requested one on
// when all is set.
//
// Each page is a fresh List with an explicit page number rather than a walk
// over types.PageInfo. PageInfo.Next replays the page List already fetched on
// its first call while still advancing its internal counter, so driving the
// loop with it skips a page whenever the caller starts past page one. Next is
// still called, but only to decode the body already in hand.
//
// The walk ends on an empty page and on nothing else. Every paging field a
// response carries is optional and none of them is trustworthy: the library
// reports a missing page or limit as the same zero it reports a literal one as,
// and a total page count computed from the requested limit understates the
// truth whenever the Engine caps its page size lower. Each of those has already
// cost a release a silent truncation, so the page contents are the only signal
// left worth acting on.
func fetch[O any, PO Pointer[O]](ctx context.Context, a api.API, notices io.Writer, filter PO, plural string, page, limit int, all bool) ([]O, error) {
	paginated, err := paginates(ctx, filter)
	if err != nil {
		return nil, err
	}

	if !paginated {
		// The library drops the page parameter for these, so every request
		// returns the same full result set: asking again is pointless, and
		// asking for a later page cannot be answered at all.
		if page > 1 {
			return nil, errmap.Usagef("%s does not support paging, so --page %d cannot be served", plural, page)
		}

		all = false
	}

	items := make([]O, 0, limit)

	for p := page; ; p++ {
		// Bounds pages of results, leaving room for the one request that
		// confirms the end, so a result set exactly maxPages long is
		// returned rather than discarded as a runaway.
		if p-page > maxPages {
			return nil, pageLimitError(limit)
		}

		var info types.PageInfo
		if err := a.List(ctx, filter, api.Paged(uint(p), uint(limit), &info)); err != nil {
			if endOfWalk(err, page, p, all) {
				noteIncompleteWalk(notices, plural, p)

				break
			}

			return nil, err
		}

		var pageItems []O

		info.Next(&pageItems)

		if err := info.Error(); err != nil {
			return nil, err
		}

		items = append(items, pageItems...)

		if !all || len(pageItems) == 0 {
			break
		}
	}

	return items, nil
}

// noteIncompleteWalk says that a walk ended on a not-found rather than on an
// empty page.
//
// Ending there is the right call, since an Engine that answers a page past the
// end with a 404 is otherwise unwalkable, but the same 404 could be a parent
// object deleted mid-walk or a proxy having a bad moment. Those return fewer
// results than exist, so saying nothing would make partial output look
// complete. It goes to stderr, leaving stdout clean for a pipeline.
func noteIncompleteWalk(notices io.Writer, plural string, page int) {
	if notices == nil {
		return
	}

	_, _ = fmt.Fprintf(notices,
		"warning: stopped at page %d, which the Engine reports does not exist; %s may be missing\n",
		page, plural)
}

// endOfWalk reports whether a failed page request means the results simply ran
// out. Some endpoints answer a page past the end with a 404 instead of an empty
// list, and a walk always asks one page beyond the results, so that is the end
// rather than a failure. On the page the caller asked for it is a real miss and
// they have to hear about it.
//
// Both paging loops share this, because a walk that dies here on one client and
// succeeds on the other is the divergence users notice.
func endOfWalk(err error, page, current int, all bool) bool {
	return all && current > page && errmap.IsNotFound(err)
}

// paginates reports whether the object's endpoint pages at all. go-anxcloud
// asks the object the same question through PaginationSupportHook and, for the
// ones that answer no, stops sending the page parameter entirely. A caller that
// does not check ends up requesting the same full result set over and over.
func paginates(ctx context.Context, obj types.Object) (bool, error) {
	hook, ok := obj.(types.PaginationSupportHook)
	if !ok {
		return true, nil
	}

	// The library calls this hook from inside a List, so the operation is on
	// the context by the time an implementation reads it. None of the objects
	// in go-anxcloud v0.14.5 do, but one that did would fail here otherwise.
	paginated, err := hook.HasPagination(types.ContextWithOperation(ctx, types.OperationList))
	if err != nil {
		return false, fmt.Errorf("checking paging support: %w", err)
	}

	return paginated, nil
}

// newGetCommand builds "<noun> get <id>", the read of a single object.
func newGetCommand[O any, PO Pointer[O]](env Env, spec Spec[O, PO]) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Show one " + spec.Noun,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ValidateIdentifier(spec.Noun, args[0]); err != nil {
				return err
			}

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

// RegisterPagingFlags declares --page, --limit and --all. Commands written
// against the legacy client call this too, so paging looks identical
// everywhere.
func RegisterPagingFlags(flags *pflag.FlagSet, page, limit *int, all *bool, plural string) {
	flags.IntVar(page, "page", 1, "page number to fetch")
	flags.IntVar(limit, "limit", 50, "maximum number of "+plural+" per page")
	flags.BoolVar(all, "all", false, "fetch every page instead of just one")
}

// FetchPages walks pages by calling get once per page, for commands written
// against the legacy client. Those clients discard every byte of page metadata
// before returning, so an empty page is the only end-of-results signal there
// is; maxPages backstops an Engine that never produces one. Stopping on a page
// merely shorter than the limit would truncate the walk against an Engine that
// caps page size below --limit, which the default limit of 50 makes likely.
func FetchPages[T any](notices io.Writer, plural string, page, limit int, all bool, get func(page int) ([]T, error)) ([]T, error) {
	items := make([]T, 0, limit)

	for p := page; ; p++ {
		// Bounds pages of results, leaving room for the one request that
		// confirms the end, so a result set exactly maxPages long is
		// returned rather than discarded as a runaway.
		if p-page > maxPages {
			return nil, pageLimitError(limit)
		}

		pageItems, err := get(p)
		if err != nil {
			if endOfWalk(err, page, p, all) {
				noteIncompleteWalk(notices, plural, p)

				return items, nil
			}

			return nil, err
		}

		items = append(items, pageItems...)

		if !all || len(pageItems) == 0 {
			return items, nil
		}
	}
}

// ValidateIdentifier rejects an argument that names no object.
//
// Both clients put an identifier straight into the URL path, so a value that is
// empty, contains a path separator, or is only path punctuation does not address
// a member of the collection.
// Empty leaves a trailing slash and addresses the collection itself; a relative
// segment such as ".." is normalized away and addresses whatever sits above the
// endpoint. Either can make the Engine act on something the caller never named,
// and on a destructive verb that is the worst outcome available, so this refuses
// before anything reaches the network. Escaping cannot help: percent-encoding a
// dot leaves it a dot.
//
// What counts as a valid identifier is otherwise the Engine's business, so this
// only rules out values that cannot stay in one URL path segment.
func ValidateIdentifier(what, value string) error {
	if strings.Contains(value, "/") {
		return errmap.Usagef("%s %q does not name a %s", what, value, what)
	}

	switch strings.TrimSpace(value) {
	case "", ".", "..":
		return errmap.Usagef("%s %q does not name a %s", what, value, what)
	}

	return nil
}

// ValidatePaging rejects out-of-range paging flags. Commands written against
// the legacy client register the same flags by hand and share this check.
func ValidatePaging(page, limit int, all bool) error {
	if page < 1 {
		return errmap.Usagef("--page %d must be 1 or greater", page)
	}

	if limit < 1 || limit > MaxLimit {
		return errmap.Usagef("--limit %d must be between 1 and %d", limit, MaxLimit)
	}

	if all && page > int(^uint(0)>>1)-maxPages {
		return errmap.Usagef("--page %d is too large for --all", page)
	}

	return nil
}

package resource

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/api/types"

	"github.com/ProbstenHias/anexia-cli/internal/await"
	"github.com/ProbstenHias/anexia-cli/internal/confirm"
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
	flags.IntVar(&page, "page", 1, "page number to fetch")
	flags.IntVar(&limit, "limit", 50, "maximum number of "+spec.plural()+" per page")
	flags.BoolVar(&all, "all", false, "fetch every page instead of just one")

	var applyFilters func(*O)
	if spec.Filters != nil {
		applyFilters = spec.Filters(flags)
	}

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if page < 1 {
			return errmap.Usagef("--page %d must be 1 or greater", page)
		}

		if limit < 1 || limit > MaxLimit {
			return errmap.Usagef("--limit %d must be between 1 and %d", limit, MaxLimit)
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
func fetch[O any, PO Pointer[O]](ctx context.Context, a api.API, filter PO, page, limit int, all bool) ([]O, error) {
	var info types.PageInfo
	if err := a.List(ctx, filter, api.Paged(uint(page), uint(limit), &info)); err != nil {
		return nil, err
	}

	items := make([]O, 0, limit)

	for {
		// The first Next call yields the page List already fetched.
		var pageItems []O
		if !info.Next(&pageItems) {
			break
		}

		items = append(items, pageItems...)

		if !all {
			break
		}
	}

	if err := info.Error(); err != nil {
		return nil, err
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

// newCreateCommand builds "<noun> create", which POSTs a new object.
func newCreateCommand[O any, PO Pointer[O]](env Env, spec Spec[O, PO]) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a " + spec.Noun,
		Args:  cobra.NoArgs,
	}

	build := spec.Create(cmd.Flags())
	waiter := newWaiter(cmd, spec)

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
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

		var obj O
		build(&obj)

		if err := a.Create(ctx, PO(&obj)); err != nil {
			return env.Fail(fmt.Errorf("creating %s: %w", spec.Noun, err))
		}

		if err := waiter.wait(ctx, env, a, PO(&obj), spec.Noun); err != nil {
			return err
		}

		return renderOne(w, spec, &obj)
	}

	return cmd
}

// newUpdateCommand builds "<noun> update <id>": read, mutate, PUT. Reading
// first means callers only pass the fields they want to change.
func newUpdateCommand[O any, PO Pointer[O]](env Env, spec Spec[O, PO]) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a " + spec.Noun,
		Args:  cobra.ExactArgs(1),
	}

	apply := spec.Update(cmd.Flags())
	waiter := newWaiter(cmd, spec)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
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

		apply((*O)(obj))

		if err := a.Update(ctx, obj); err != nil {
			return env.Fail(fmt.Errorf("updating %s %q: %w", spec.Noun, args[0], err))
		}

		if err := waiter.wait(ctx, env, a, obj, spec.Noun); err != nil {
			return err
		}

		return renderOne(w, spec, (*O)(obj))
	}

	return cmd
}

// newDeleteCommand builds "<noun> delete <id>", guarded by a confirmation.
func newDeleteCommand[O any, PO Pointer[O]](env Env, spec Spec[O, PO]) *cobra.Command {
	return &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"destroy"},
		Short:   "Delete a " + spec.Noun,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			obj, err := spec.identify(args[0])
			if err != nil {
				return err
			}

			question := fmt.Sprintf("delete %s %q", spec.Noun, args[0])
			if err := confirm.Prompt(cmd.InOrStdin(), cmd.ErrOrStderr(), question, env.AssumeYes()); err != nil {
				return err
			}

			a, err := env.API(cmd.Flags())
			if err != nil {
				return err
			}

			ctx, cancel := env.Context(cmd.Context())
			defer cancel()

			if err := a.Destroy(ctx, obj); err != nil {
				return env.Fail(fmt.Errorf("deleting %s %q: %w", spec.Noun, args[0], err))
			}

			_, err = fmt.Fprintf(cmd.ErrOrStderr(), "deleted %s %s\n", spec.Noun, args[0])

			return err
		},
	}
}

// waiter holds the --wait flag values of a state-changing verb.
type waiter struct {
	enabled bool
	timeout time.Duration
}

// newWaiter registers --wait and --wait-timeout when the resource reports a
// provisioning state.
func newWaiter[O any, PO Pointer[O]](cmd *cobra.Command, spec Spec[O, PO]) *waiter {
	w := &waiter{}

	if !spec.Awaitable {
		return w
	}

	flags := cmd.Flags()
	flags.BoolVar(&w.enabled, "wait", false, "wait until the "+spec.Noun+" leaves its pending state")
	flags.DurationVar(&w.timeout, "wait-timeout", 10*time.Minute, "how long to wait when --wait is set")

	return w
}

// wait blocks on the object's provisioning state when --wait was passed.
func (w *waiter) wait(ctx context.Context, env Env, a api.API, obj types.IdentifiedObject, noun string) error {
	if !w.enabled {
		return nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	if err := await.Completion(waitCtx, a, obj, 0); err != nil {
		return env.Fail(fmt.Errorf("waiting for %s: %w", noun, err))
	}

	return nil
}

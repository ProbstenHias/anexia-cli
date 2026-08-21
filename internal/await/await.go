// Package await polls Engine objects until they leave their pending state.
package await

import (
	"context"
	"fmt"
	"time"

	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/api/types"
	"go.anx.io/go-anxcloud/pkg/apis/common/gs"
)

// DefaultInterval is how often Completion re-reads the object. go-anxcloud's
// own gs.AwaitCompletion polls every 30 seconds, which is too coarse for an
// interactive CLI.
const DefaultInterval = 5 * time.Second

// ErrNotSupported is returned for objects that carry no Engine state and so
// can never be waited on.
var ErrNotSupported = fmt.Errorf("%w: this resource reports no state to wait for", api.ErrOperationNotSupported)

// Completion polls obj until its state is no longer pending. It returns as
// soon as the state is OK, and fails on an error state, on a read failure, or
// when ctx is done. A non-positive interval means DefaultInterval.
func Completion(ctx context.Context, a api.API, obj types.IdentifiedObject, interval time.Duration) error {
	stateful, ok := obj.(gs.StateRetriever)
	if !ok {
		return ErrNotSupported
	}

	if interval <= 0 {
		interval = DefaultInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := a.Get(ctx, obj); err != nil {
			return fmt.Errorf("reading state: %w", err)
		}

		switch {
		case stateful.StateOK():
			return nil
		case stateful.StateError():
			return gs.ErrStateError
		case !stateful.StatePending():
			return gs.ErrStateUnknown
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

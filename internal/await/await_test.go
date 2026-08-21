package await_test

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/api/types"
	"go.anx.io/go-anxcloud/pkg/apis/common/gs"

	"github.com/ProbstenHias/anexia-cli/internal/await"
)

// statelessObject is an Engine object that reports no state.
type statelessObject struct{}

func (statelessObject) EndpointURL(context.Context) (*url.URL, error) {
	return url.Parse("/api/test/v1/thing.json")
}

func (statelessObject) GetIdentifier(context.Context) (string, error) {
	return "thing-1", nil
}

// statefulObject is an Engine object whose state changes on every read.
type statefulObject struct {
	statelessObject

	types []int
	reads int
}

func (o *statefulObject) current() int {
	if o.reads == 0 {
		return gs.StateTypePending
	}

	i := min(o.reads, len(o.types)) - 1

	return o.types[i]
}

func (o *statefulObject) StateOK() bool {
	return o.current() == gs.StateTypeOK
}

func (o *statefulObject) StatePending() bool {
	return o.current() == gs.StateTypePending
}

func (o *statefulObject) StateError() bool {
	return o.current() == gs.StateTypeError
}

// fakeAPI counts Get calls and can fail them. Only Get is exercised, so the
// remaining operations are deliberately unimplemented.
type fakeAPI struct {
	object *statefulObject
	err    error
}

func (f *fakeAPI) Get(_ context.Context, _ types.IdentifiedObject, _ ...types.GetOption) error {
	if f.err != nil {
		return f.err
	}

	f.object.reads++

	return nil
}

func (*fakeAPI) List(context.Context, types.FilterObject, ...types.ListOption) error {
	return api.ErrOperationNotSupported
}

func (*fakeAPI) Create(context.Context, types.Object, ...types.CreateOption) error {
	return api.ErrOperationNotSupported
}

func (*fakeAPI) Update(context.Context, types.IdentifiedObject, ...types.UpdateOption) error {
	return api.ErrOperationNotSupported
}

func (*fakeAPI) Destroy(context.Context, types.IdentifiedObject, ...types.DestroyOption) error {
	return api.ErrOperationNotSupported
}

func TestCompletionRejectsStatelessObjects(t *testing.T) {
	t.Parallel()

	err := await.Completion(context.Background(), &fakeAPI{}, statelessObject{}, time.Millisecond)

	require.ErrorIs(t, err, await.ErrNotSupported)
	assert.ErrorIs(t, err, api.ErrOperationNotSupported)
}

func TestCompletionReturnsOnOKState(t *testing.T) {
	t.Parallel()

	obj := &statefulObject{types: []int{gs.StateTypeOK}}

	require.NoError(t, await.Completion(context.Background(), &fakeAPI{object: obj}, obj, time.Millisecond))
	assert.Equal(t, 1, obj.reads)
}

func TestCompletionPollsWhilePending(t *testing.T) {
	t.Parallel()

	obj := &statefulObject{types: []int{gs.StateTypePending, gs.StateTypePending, gs.StateTypeOK}}

	require.NoError(t, await.Completion(context.Background(), &fakeAPI{object: obj}, obj, time.Millisecond))
	assert.Equal(t, 3, obj.reads)
}

func TestCompletionFailsOnErrorState(t *testing.T) {
	t.Parallel()

	obj := &statefulObject{types: []int{gs.StateTypeError}}

	err := await.Completion(context.Background(), &fakeAPI{object: obj}, obj, time.Millisecond)

	assert.ErrorIs(t, err, gs.ErrStateError)
}

func TestCompletionFailsOnUnknownState(t *testing.T) {
	t.Parallel()

	obj := &statefulObject{types: []int{42}}

	err := await.Completion(context.Background(), &fakeAPI{object: obj}, obj, time.Millisecond)

	assert.ErrorIs(t, err, gs.ErrStateUnknown)
}

func TestCompletionReportsReadFailure(t *testing.T) {
	t.Parallel()

	obj := &statefulObject{types: []int{gs.StateTypeOK}}

	err := await.Completion(context.Background(), &fakeAPI{object: obj, err: errors.New("boom")}, obj, time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading state: boom")
}

func TestCompletionHonorsContext(t *testing.T) {
	t.Parallel()

	obj := &statefulObject{types: []int{gs.StateTypePending}}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := await.Completion(ctx, &fakeAPI{object: obj}, obj, time.Millisecond)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestCompletionDefaultsInterval(t *testing.T) {
	t.Parallel()

	obj := &statefulObject{types: []int{gs.StateTypeOK}}

	// A non-positive interval must not stall: the first read already
	// resolves, so DefaultInterval is never waited on.
	require.NoError(t, await.Completion(context.Background(), &fakeAPI{object: obj}, obj, 0))
}

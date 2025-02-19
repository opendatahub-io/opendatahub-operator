package actions

import (
	"context"
	"reflect"
	"runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

//
// Common
//

const (
	ActionGroup = "action"
)

type Fn func(ctx context.Context, rr *types.ReconciliationRequest) error

// TODO replace with type alias in GO 1.24.
type StringGetter func(context.Context, *types.ReconciliationRequest) (string, error)

func (f Fn) String() string {
	fn := runtime.FuncForPC(reflect.ValueOf(f).Pointer())
	return fn.Name()
}

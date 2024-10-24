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

func (f Fn) String() string {
	fn := runtime.FuncForPC(reflect.ValueOf(f).Pointer())
	return fn.Name()
}

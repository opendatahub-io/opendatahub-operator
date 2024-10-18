package actions

import (
	"context"

	"github.com/go-logr/logr"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

//
// Common
//

const (
	ActionGroup = "action"
)

type Action interface {
	Execute(ctx context.Context, rr *types.ReconciliationRequest) error
}

type BaseAction struct {
	Log logr.Logger
}

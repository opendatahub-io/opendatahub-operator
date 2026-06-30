package render

import (
	"github.com/opendatahub-io/operator-actions-framework/controller/types"
)

type CachingKeyFn func(rr *types.ReconciliationRequest) ([]byte, error)

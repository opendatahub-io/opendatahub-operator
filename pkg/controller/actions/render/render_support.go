package render

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type CachingKeyFn func(rr *types.ReconciliationRequest) ([]byte, error)

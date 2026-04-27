package precondition

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func MonitorCRD(gvk schema.GroupVersionKind) PreCondition {
	return MonitorCRDs(gvk)
}

// All CRDs are checked and absent ones are reported together in a single failure message.
// The first API error encountered is returned immediately.
func MonitorCRDs(gvks ...schema.GroupVersionKind) PreCondition {
	return New(func(ctx context.Context, rr *types.ReconciliationRequest) (CheckResult, error) {
		var missing []string

		for _, gvk := range gvks {
			has, err := cluster.HasCRD(ctx, rr.Client, gvk)
			if err != nil {
				return CheckResult{}, fmt.Errorf("%s: failed to check CRD presence: %w", gvk.Kind, err)
			}

			if !has {
				missing = append(missing, gvk.Kind+": CRD not found")
			}
		}

		if len(missing) > 0 {
			return CheckResult{Pass: false, Message: strings.Join(missing, "; ")}, nil
		}

		return CheckResult{Pass: true}, nil
	})
}

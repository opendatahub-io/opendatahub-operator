package precondition

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func MonitorCRD(gvk schema.GroupVersionKind, opts ...Option) PreCondition {
	return MonitorCRDs([]schema.GroupVersionKind{gvk}, opts...)
}

// MonitorCRDs creates a PreCondition that checks for the presence of multiple CRDs.
// All CRDs are checked and absent ones are reported together in a single failure message.
// The first API error encountered is returned immediately.
func MonitorCRDs(gvks []schema.GroupVersionKind, opts ...Option) PreCondition {
	monitoredGVKs := slices.Clone(gvks)

	return newPreCondition(func(ctx context.Context, rr *types.ReconciliationRequest) (CheckResult, error) {
		if len(monitoredGVKs) == 0 {
			return CheckResult{}, errors.New("MonitorCRDs called with empty GVK list")
		}

		var missing []string

		for _, gvk := range monitoredGVKs {
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
	}, opts...)
}

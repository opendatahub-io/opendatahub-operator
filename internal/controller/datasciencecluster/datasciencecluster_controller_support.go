package datasciencecluster

import (
	"context"
	"errors"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// computeComponentsStatus checks the status of all registered components in a DataScienceCluster instance
// and updates the status condition accordingly.
//
// Parameters:
// - ctx: The context for managing request deadlines and cancellation.
// - instance: The DataScienceCluster instance being reconciled.
// - reg: The registry containing all component handlers.
//
// Returns:
// - error: An error if any component status retrieval or update fails.
func computeComponentsStatus(
	ctx context.Context,
	rr *types.ReconciliationRequest,
	reg *cr.Registry,
) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return errors.New("failed to convert to DataScienceCluster")
	}

	notReadyComponents := make([]string, 0)
	managedComponent := 0

	err := reg.ForEach(func(component cr.ComponentHandler) error {
		cs, err := component.UpdateDSCStatus(ctx, rr)
		if err != nil {
			notReadyComponents = append(notReadyComponents, component.GetName())
			return err
		}

		enabled := component.IsEnabled(instance)
		if !enabled && cs != metav1.ConditionFalse {
			return nil
		}

		if enabled {
			managedComponent++
		}

		if cs != metav1.ConditionTrue {
			notReadyComponents = append(notReadyComponents, component.GetName())
		}

		return nil
	})

	switch {
	case len(notReadyComponents) > 0:
		rr.Conditions.SetCondition(common.Condition{
			Type:    status.ConditionTypeComponentsReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.NotReadyReason,
			Message: fmt.Sprintf("Some components are not ready: %s", strings.Join(notReadyComponents, ", ")),
		})
	case managedComponent == 0:
		rr.Conditions.SetCondition(common.Condition{
			Type:     status.ConditionTypeComponentsReady,
			Status:   metav1.ConditionTrue,
			Severity: common.ConditionSeverityInfo,
			Reason:   status.NoManagedComponentsReason,
			Message:  "All registered components have ManagementState Removed or are not configured",
		})
	default:
		rr.Conditions.MarkTrue(status.ConditionTypeComponentsReady)
	}

	if err != nil {
		return err
	}

	return nil
}

// collectModuleGVKs returns the GVK of every registered module so the DSC
// controller can set up dynamic watches. It iterates all modules regardless
// of enabled state because the Dynamic predicate defers actual watch setup
// until the CRD exists on the cluster.
func collectModuleGVKs(reg *modules.Registry) []schema.GroupVersionKind {
	if !reg.HasEntries() {
		return nil
	}

	var gvks []schema.GroupVersionKind

	_ = reg.ForAll(func(h modules.ModuleHandler, _ bool) error {
		gvks = append(gvks, h.GetGVK())
		return nil
	})

	return gvks
}

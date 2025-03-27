package datasciencecluster

import (
	"context"
	"errors"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
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
	instance, ok := rr.Instance.(*dscv1.DataScienceCluster)
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

		if !cr.IsManaged(component, instance) {
			return nil
		}

		managedComponent++

		if cs == metav1.ConditionFalse {
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
			Message: fmt.Sprintf("Some components are not ready: %s", strings.Join(notReadyComponents, ",")),
		})
	case managedComponent == 0:
		rr.Conditions.SetCondition(common.Condition{
			Type:     status.ConditionTypeComponentsReady,
			Status:   metav1.ConditionTrue,
			Severity: common.ConditionSeverityInfo,
			Reason:   status.NoManagedComponentsReason,
			Message:  status.NoManagedComponentsReason,
		})
	default:
		rr.Conditions.MarkTrue(status.ConditionTypeComponentsReady)
	}

	if err != nil {
		return err
	}

	return nil
}

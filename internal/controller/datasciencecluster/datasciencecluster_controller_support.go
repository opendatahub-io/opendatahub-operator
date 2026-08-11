package datasciencecluster

import (
	"context"
	"errors"
	"fmt"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
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

// updateDeprecatedTrainingOperatorStatus sets DSC status for the deprecated
// Training Operator v1 component. The handler has been removed; this inline
// check replaces it so customers see the Obsolete condition without requiring
// handler infrastructure (CRD, informer, PROJECT entry) on fresh clusters.
func updateDeprecatedTrainingOperatorStatus(rr *types.ReconciliationRequest) error {
	dsc, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return errors.New("failed to convert to DataScienceCluster")
	}

	ms := components.NormalizeManagementState(dsc.Spec.Components.TrainingOperator.ManagementState)
	dsc.Status.Components.TrainingOperator.ManagementState = ms
	dsc.Status.Components.TrainingOperator.TrainingOperatorCommonStatus = nil

	condType := componentApi.TrainingOperatorKind + status.ReadySuffix

	if ms == operatorv1.Managed {
		rr.Conditions.MarkFalse(
			condType,
			conditions.WithReason("Obsolete"),
			conditions.WithMessage("Training Operator v1 is obsolete in RHOAI 3.6. Set managementState to Removed to uninstall, then use Trainer v2."),
		)
	} else {
		rr.Conditions.MarkFalse(
			condType,
			conditions.WithReason(string(ms)),
			conditions.WithMessage("Component ManagementState is set to %s", string(ms)),
			conditions.WithSeverity(common.ConditionSeverityInfo),
		)
	}

	return nil
}

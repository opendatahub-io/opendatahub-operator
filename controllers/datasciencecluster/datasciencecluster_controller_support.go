package datasciencecluster

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
)

// reconcileComponents reconciles the components of a DataScienceCluster instance.
//
// This function iterates over all components defined in the components registry and performs necessary reconciliation
// actions such as creating, updating, or deleting resources. It tracks errors during the reconciliation process and
// updates the status of the DSC instance accordingly.
//
// Parameters:
// - ctx: The context for the reconciliation request.
// - instance: The DataScienceCluster instance being reconciled.
func reconcileComponents(
	ctx context.Context,
	cli *odhClient.Client,
	instance *dscv1.DataScienceCluster,
	registry *cr.Registry,
) {
	if instance.Status.InstalledComponents == nil {
		instance.Status.InstalledComponents = make(map[string]bool)
	}

	var reconcileErrors error
	var nonReadyComponents []string

	// ignore the aggregate error returned by the ForEach function since we
	// need to aggregate other errors
	_ = registry.ForEach(func(component cr.ComponentHandler) error {
		err := reconcileComponent(ctx, cli, instance, component)
		if err != nil {
			reconcileErrors = multierror.Append(reconcileErrors, err)
		}

		ci := component.NewCRObject(instance)

		// read the component instance to get tha actual status
		err = cli.Get(ctx, client.ObjectKeyFromObject(ci), ci)
		if err != nil && !k8serr.IsNotFound(err) {
			reconcileErrors = multierror.Append(reconcileErrors, err)
			return nil
		}

		if err := component.UpdateDSCStatus(instance, ci); err != nil {
			reconcileErrors = multierror.Append(err)
		}

		if !cr.IsManaged(component, instance) {
			return nil
		}

		if !meta.IsStatusConditionTrue(ci.GetStatus().Conditions, status.ConditionTypeReady) {
			nonReadyComponents = append(nonReadyComponents, component.GetName())
		}

		return nil
	})

	// Process reconciliation failures and update the Available condition.
	available := setAvailability(instance, reconcileErrors)
	// Process component status and update the Ready condition and Phase.
	ready := setReadiness(instance, nonReadyComponents)

	instance.Status.Release = cluster.GetRelease()
	instance.Status.ObservedGeneration = instance.Generation
	instance.Status.Phase = status.PhaseReady

	if !available || !ready {
		instance.Status.Phase = status.PhaseNotReady
	}
}

// reconcileComponent reconciles a specific component within a DataScienceCluster (DSC).
//
// It handles the component based on its management state, applying or deleting the component as needed using
// the provided client.
func reconcileComponent(
	ctx context.Context,
	cli *odhClient.Client,
	instance *dscv1.DataScienceCluster,
	component cr.ComponentHandler,
) error {
	ms := component.GetManagementState(instance)
	ci := component.NewCRObject(instance)

	switch ms {
	case operatorv1.Managed:
		err := ctrl.SetControllerReference(instance, ci, cli.Scheme())
		if err != nil {
			return err
		}
		err = cli.Apply(ctx, ci, client.FieldOwner(fieldOwner), client.ForceOwnership)
		if err != nil {
			return client.IgnoreNotFound(err)
		}
	case operatorv1.Removed:
		err := cli.Delete(ctx, ci, client.PropagationPolicy(metav1.DeletePropagationForeground))
		if err != nil {
			return client.IgnoreNotFound(err)
		}
	default:
		return fmt.Errorf("unsupported management state: %s", ms)
	}

	return nil
}

// setAvailability updates the "Available" condition of the DataScienceCluster instance  based on the provided error.
// If the error is nil, the condition is set to True with a success message.
// If an error is present, the condition is set to False with the error message.
//
// Parameters:
//   - instance: The DataScienceCluster instance whose status conditions are being updated.
//   - err: The error encountered during reconciliation, if any.
//
// Returns:
//   - A boolean indicating whether the "Available" condition is True
func setAvailability(instance *dscv1.DataScienceCluster, err error) bool {
	condition := conditionsv1.Condition{
		Type:    conditionsv1.ConditionAvailable,
		Status:  corev1.ConditionTrue,
		Reason:  status.AvailableReason,
		Message: "DataScienceCluster resource reconciled successfully",
	}

	if err != nil {
		condition.Status = corev1.ConditionFalse
		condition.Reason = status.DegradedReason
		condition.Message = fmt.Sprintf("DataScienceCluster resource reconciled with errors: %v", err)
	}

	conditionsv1.SetStatusCondition(&instance.Status.Conditions, condition)

	return condition.Status == corev1.ConditionTrue
}

// setReadiness updates the "Ready" condition and the phase of the DataScienceCluster instance based on the list of
// non-ready components.
//
// If all components are ready, the condition is set to True  with a success message.
// If any components are not ready, the condition is set to False  with the error message.
//
// Parameters:
//   - instance: The DataScienceCluster instance whose status conditions are being updated.
//   - nonReadyComponents: A slice of strings representing the names of components that are not ready.
//
// Returns:
//   - A boolean indicating whether the "Ready" condition is True.
func setReadiness(instance *dscv1.DataScienceCluster, nonReadyComponents []string) bool {
	condition := conditionsv1.Condition{
		Type:    conditionsv1.ConditionType(status.ConditionTypeReady),
		Status:  corev1.ConditionTrue,
		Reason:  status.ReadyReason,
		Message: status.ReadyReason,
	}

	slices.Sort(nonReadyComponents)

	if len(nonReadyComponents) != 0 {
		condition.Status = corev1.ConditionFalse
		condition.Reason = status.NotReadyReason
		condition.Message = fmt.Sprintf("Some components are not ready: %s", strings.Join(nonReadyComponents, ","))
	}

	conditionsv1.SetStatusCondition(&instance.Status.Conditions, condition)

	return condition.Status == corev1.ConditionTrue
}

package datasciencecluster

import (
	"context"
	"fmt"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
)

// reconcileComponent reconciles a specific component within a DataScienceCluster.
//
// It determines the management state of the component and applies or deletes
// the component accordingly using the provided client.
//
// Parameters:
// - ctx: The context for the request.
// - cli: The client used to interact with the Kubernetes API.
// - instance: The DataScienceCluster instance being reconciled.
// - component: The component handler that provides component-specific behavior.
//
// Returns:
// - An error if any operation fails, otherwise nil.
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

// reconcileComponents reconciles all registered components within a DataScienceCluster.
//
// It iterates over each component in the registry and calls reconcileComponent to
// ensure the component is applied or deleted based on its management state.
//
// Parameters:
// - ctx: The context for the request.
// - cli: The client used to interact with the Kubernetes API.
// - instance: The DataScienceCluster instance being reconciled.
// - reg: The registry containing all component handlers.
//
// Returns:
// - An error if any operation fails, otherwise nil.
func reconcileComponents(
	ctx context.Context,
	cli *odhClient.Client,
	instance *dscv1.DataScienceCluster,
	reg *cr.Registry,
) error {
	err := reg.ForEach(func(component cr.ComponentHandler) error {
		err := reconcileComponent(ctx, cli, instance, component)
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	return nil
}

// reconcileComponentsStatus checks the status of all registered components in a DataScienceCluster instance
// and updates the status condition accordingly.
//
// Parameters:
// - ctx: The context for managing request deadlines and cancellation.
// - cli: A client for interacting with the Kubernetes API.
// - instance: The DataScienceCluster instance being reconciled.
// - reg: The registry containing all component handlers.
//
// Returns:
// - error: An error if any component status retrieval or update fails.
func reconcileComponentsStatus(
	ctx context.Context,
	cli *odhClient.Client,
	instance *dscv1.DataScienceCluster,
	reg *cr.Registry,
) error {
	notReadyComponents := make([]string, 0)
	managedComponent := 0

	err := reg.ForEach(func(component cr.ComponentHandler) error {
		ci := component.NewCRObject(instance)

		if err := cli.Get(ctx, client.ObjectKeyFromObject(ci), ci); err != nil && !k8serr.IsNotFound(err) {
			notReadyComponents = append(notReadyComponents, component.GetName())
			return err
		}

		if err := component.UpdateDSCStatus(instance, ci); err != nil {
			notReadyComponents = append(notReadyComponents, component.GetName())
			return err
		}

		if !cr.IsManaged(component, instance) {
			return nil
		}

		managedComponent++

		if !conditions.IsStatusConditionTrue(ci.GetStatus().Conditions, status.ConditionTypeReady) {
			notReadyComponents = append(notReadyComponents, component.GetName())
		}

		return nil
	})

	switch {
	case len(notReadyComponents) > 0:
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    status.ConditionTypeComponentsReady,
			Status:  corev1.ConditionFalse,
			Reason:  status.NotReadyReason,
			Message: fmt.Sprintf("Some components are not ready: %s", strings.Join(notReadyComponents, ",")),
		})
	case managedComponent == 0:
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    status.ConditionTypeComponentsReady,
			Status:  corev1.ConditionTrue,
			Reason:  status.NoManagedComponentsReason,
			Message: status.NoManagedComponentsReason,
		})
	default:
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    status.ConditionTypeComponentsReady,
			Status:  corev1.ConditionTrue,
			Reason:  status.ReadyReason,
			Message: status.ReadyReason,
		})
	}

	if err != nil {
		return err
	}

	return nil
}

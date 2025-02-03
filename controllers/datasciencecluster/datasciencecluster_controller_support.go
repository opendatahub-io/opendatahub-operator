package datasciencecluster

import (
	"context"
	"fmt"
	"strings"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
)

// computeComponentsStatus checks the status of all registered components in a DataScienceCluster instance
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
func computeComponentsStatus(
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
		conditions.SetStatusCondition(&instance.Status.Conditions, common.Condition{
			Type:    status.ConditionTypeComponentsReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.NotReadyReason,
			Message: fmt.Sprintf("Some components are not ready: %s", strings.Join(notReadyComponents, ",")),
		})
	case managedComponent == 0:
		conditions.SetStatusCondition(&instance.Status.Conditions, common.Condition{
			Type:     status.ConditionTypeComponentsReady,
			Status:   metav1.ConditionTrue,
			Severity: common.ConditionSeverityInfo,
			Reason:   status.NoManagedComponentsReason,
			Message:  status.NoManagedComponentsReason,
		})
	default:
		conditions.SetStatusCondition(&instance.Status.Conditions, common.Condition{
			Type:   status.ConditionTypeComponentsReady,
			Status: metav1.ConditionTrue,
		})
	}

	if err != nil {
		return err
	}

	return nil
}

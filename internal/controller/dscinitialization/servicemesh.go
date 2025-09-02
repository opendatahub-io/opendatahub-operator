package dscinitialization

import (
	"context"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func (r *DSCInitializationReconciler) handleServiceMesh(ctx context.Context, dscInit *dsciv1.DSCInitialization) error {
	log := logf.FromContext(ctx)

	if dscInit.Spec.ServiceMesh == nil {
		log.Info("ServiceMesh not defined in DSCI, deleting ServiceMesh CR if present")

		conditions := dscInit.Status.Conditions
		status.SetCondition(
			&conditions,
			status.CapabilityServiceMesh,
			status.RemovedReason,
			"ServiceMesh spec is not defined in DSCI",
			metav1.ConditionFalse,
		)
		status.SetCondition(
			&conditions,
			status.CapabilityServiceMeshAuthorization,
			status.RemovedReason,
			"ServiceMesh spec is not defined in DSCI",
			metav1.ConditionFalse,
		)

		// Update DSCI status with the conditions
		_, err := status.UpdateWithRetry(ctx, r.Client, dscInit, func(saved *dsciv1.DSCInitialization) {
			saved.Status.SetConditions(conditions)
		})
		if err != nil {
			log.Error(err, "failed to update DSCI status condition for ServiceMesh")
			return err
		}

		return r.deleteServiceMesh(ctx)
	}

	if dscInit.Spec.ServiceMesh.ManagementState == operatorv1.Removed {
		log.Info("ServiceMesh set to Removed, deleting ServiceMesh instance and its dependent resources")

		// Set conditions on DSCI before deletion
		conditions := dscInit.Status.Conditions
		status.SetCondition(
			&conditions,
			status.CapabilityServiceMesh,
			status.RemovedReason,
			"ServiceMesh is set to Removed",
			metav1.ConditionFalse,
		)
		status.SetCondition(
			&conditions,
			status.CapabilityServiceMeshAuthorization,
			status.RemovedReason,
			"ServiceMesh is set to Removed, ServiceMesh Authorization is therefore also Removed",
			metav1.ConditionFalse,
		)

		// Update DSCI status with the conditions
		_, err := status.UpdateWithRetry(ctx, r.Client, dscInit, func(saved *dsciv1.DSCInitialization) {
			saved.Status.SetConditions(conditions)
		})
		if err != nil {
			log.Error(err, "failed to update DSCI status condition for ServiceMesh")
			return err
		}

		return r.deleteServiceMesh(ctx)
	}

	// Create or update ServiceMesh CR for Managed/Unmanaged states
	log.Info("ServiceMesh is currently set in DSCI", "managementState", dscInit.Spec.ServiceMesh.ManagementState)
	log.Info("Updating ServiceMesh CR instance")

	desiredServiceMesh := &serviceApi.ServiceMesh{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.ServiceMeshInstanceName,
		},
		Spec: serviceApi.ServiceMeshSpec{
			ManagementState: dscInit.Spec.ServiceMesh.ManagementState,
			ControlPlane: serviceApi.ServiceMeshControlPlaneSpec{
				Name:              dscInit.Spec.ServiceMesh.ControlPlane.Name,
				Namespace:         dscInit.Spec.ServiceMesh.ControlPlane.Namespace,
				MetricsCollection: dscInit.Spec.ServiceMesh.ControlPlane.MetricsCollection,
			},
			Auth: serviceApi.ServiceMeshAuthSpec{
				Namespace: dscInit.Spec.ServiceMesh.Auth.Namespace,
				Audiences: dscInit.Spec.ServiceMesh.Auth.Audiences,
			},
		},
	}
	if err := controllerutil.SetControllerReference(dscInit, desiredServiceMesh, r.Client.Scheme()); err != nil {
		return err
	}

	err := resources.Apply(
		ctx,
		r.Client,
		desiredServiceMesh,
		client.FieldOwner(fieldManager),
		client.ForceOwnership,
	)
	if err != nil {
		return err
	}

	return nil
}

func (r *DSCInitializationReconciler) deleteServiceMesh(ctx context.Context) error {
	log := logf.FromContext(ctx)
	log.Info("Deleting ServiceMesh instance")

	// ServiceMesh not defined in DSCI -> remove ServiceMesh instance if it exists
	sm := &serviceApi.ServiceMesh{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.ServiceMeshInstanceName,
		},
	}

	propagationPolicy := metav1.DeletePropagationForeground
	if err := r.Client.Delete(
		ctx,
		sm,
		&client.DeleteOptions{
			PropagationPolicy: &propagationPolicy,
		},
	); err != nil {
		if k8serr.IsNotFound(err) {
			log.Info("ServiceMesh instance not found, proceeding as deletion success")
			return nil
		}

		log.Error(err, "Failed to delete ServiceMesh instance")
		return err
	}

	log.Info("ServiceMesh instance deleted successfully")
	return nil
}

func (r *DSCInitializationReconciler) syncServiceMeshConditions(ctx context.Context, dscInit *dsciv1.DSCInitialization) error {
	log := logf.FromContext(ctx)

	sm := &serviceApi.ServiceMesh{}
	err := r.Client.Get(ctx, client.ObjectKey{
		Name: serviceApi.ServiceMeshInstanceName,
	}, sm)

	if err != nil && !k8serr.IsNotFound(err) {
		return err
	}

	if k8serr.IsNotFound(err) {
		dsciConditions := dscInit.Status.Conditions
		status.SetCondition(
			&dsciConditions,
			status.CapabilityServiceMesh,
			status.NotReadyReason,
			"ServiceMesh CR not found",
			metav1.ConditionFalse,
		)
		status.SetCondition(
			&dsciConditions,
			status.CapabilityServiceMeshAuthorization,
			status.NotReadyReason,
			"ServiceMesh CR not found",
			metav1.ConditionFalse,
		)

		dscInit.Status.SetConditions(dsciConditions)
		return nil
	}

	dsciConditions := dscInit.Status.Conditions

	if meshCondition := conditions.FindStatusCondition(sm.GetStatus(), status.CapabilityServiceMesh); meshCondition != nil {
		status.SetCondition(
			&dsciConditions,
			status.CapabilityServiceMesh,
			meshCondition.Reason,
			meshCondition.Message,
			meshCondition.Status,
		)
	}

	if authCondition := conditions.FindStatusCondition(sm.GetStatus(), status.CapabilityServiceMeshAuthorization); authCondition != nil {
		status.SetCondition(
			&dsciConditions,
			status.CapabilityServiceMeshAuthorization,
			authCondition.Reason,
			authCondition.Message,
			authCondition.Status,
		)
	}

	_, err = status.UpdateWithRetry(ctx, r.Client, dscInit, func(saved *dsciv1.DSCInitialization) {
		saved.Status.SetConditions(dsciConditions)
	})
	if err != nil {
		log.Error(err, "failed to update DSCI status with ServiceMesh conditions")
		return err
	}

	log.Info("Successfully synced ServiceMesh conditions to DSCI")
	return nil
}

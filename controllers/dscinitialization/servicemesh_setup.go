package dscinitialization

import (
	"path"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

func (r *DSCInitializationReconciler) configureServiceMesh(instance *dsciv1.DSCInitialization) error {
	switch instance.Spec.ServiceMesh.ManagementState {
	case operatorv1.Managed:
		serviceMeshFeatures := feature.ClusterFeaturesHandler(instance, configureServiceMeshFeatures())

		if err := serviceMeshFeatures.Apply(); err != nil {
			r.Log.Error(err, "failed applying service mesh resources")
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "failed applying service mesh resources")
			return err
		}
	case operatorv1.Unmanaged:
		r.Log.Info("ServiceMesh CR is not configured by the operator, we won't do anything")
	case operatorv1.Removed:
		r.Log.Info("existing ServiceMesh CR (owned by operator) will be removed")
		if err := r.removeServiceMesh(instance); err != nil {
			return err
		}
	}

	return nil
}

func (r *DSCInitializationReconciler) removeServiceMesh(instance *dsciv1.DSCInitialization) error {
	// on condition of Managed, do not handle Removed when set to Removed it trigger DSCI reconcile to cleanup
	if instance.Spec.ServiceMesh.ManagementState == operatorv1.Managed {
		serviceMeshFeatures := feature.ClusterFeaturesHandler(instance, configureServiceMeshFeatures())

		if err := serviceMeshFeatures.Delete(); err != nil {
			r.Log.Error(err, "failed deleting service mesh resources")
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "failed deleting service mesh resources")

			return err
		}
	}

	return nil
}

func configureServiceMeshFeatures() feature.FeaturesProvider {
	return func(handler *feature.FeaturesHandler) error {
		serviceMeshSpec := handler.DSCInitializationSpec.ServiceMesh

		smcpCreationErr := feature.CreateFeature("mesh-control-plane-creation").
			For(handler).
			Manifests(
				path.Join(feature.ServiceMeshDir, "base", "create-smcp.tmpl"),
			).
			PreConditions(
				servicemesh.EnsureServiceMeshOperatorInstalled,
				feature.CreateNamespaceIfNotExists(serviceMeshSpec.ControlPlane.Namespace),
			).
			PostConditions(
				feature.WaitForPodsToBeReady(serviceMeshSpec.ControlPlane.Namespace),
			).
			Load()

		if smcpCreationErr != nil {
			return smcpCreationErr
		}

		if serviceMeshSpec.ControlPlane.MetricsCollection == "Istio" {
			metricsCollectionErr := feature.CreateFeature("mesh-metrics-collection").
				For(handler).
				PreConditions(
					servicemesh.EnsureServiceMeshInstalled,
				).
				Manifests(
					path.Join(feature.ServiceMeshDir, "metrics-collection"),
				).
				Load()
			if metricsCollectionErr != nil {
				return metricsCollectionErr
			}
		}

		return nil
	}
}

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
		serviceMeshInitializer := feature.ClusterFeaturesInitializer(instance, configureServiceMeshFeatures())
		if err := serviceMeshInitializer.Prepare(); err != nil {
			r.Log.Error(err, "failed configuring service mesh resources")
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "failed configuring service mesh resources")
			return err
		}

		if err := serviceMeshInitializer.Apply(); err != nil {
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
		serviceMeshInitializer := feature.ClusterFeaturesInitializer(instance, configureServiceMeshFeatures())
		if err := serviceMeshInitializer.Prepare(); err != nil {
			r.Log.Error(err, "failed configuring service mesh resources")
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "failed configuring service mesh resources")

			return err
		}

		if err := serviceMeshInitializer.Delete(); err != nil {
			r.Log.Error(err, "failed deleting service mesh resources")
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "failed deleting service mesh resources")

			return err
		}
	}

	return nil
}

func configureServiceMeshFeatures() feature.DefinedFeatures {
	return func(initializer *feature.FeaturesInitializer) error {
		serviceMeshSpec := initializer.DSCInitializationSpec.ServiceMesh

		smcpCreation, errSmcp := feature.CreateFeature("mesh-control-plane-creation").
			With(initializer.DSCInitializationSpec).
			From(initializer.Source).
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

		if errSmcp != nil {
			return errSmcp
		}
		initializer.Features = append(initializer.Features, smcpCreation)

		if serviceMeshSpec.ControlPlane.MetricsCollection == "Istio" {
			metricsCollection, errMetrics := feature.CreateFeature("mesh-metrics-collection").
				With(initializer.DSCInitializationSpec).
				From(initializer.Source).
				PreConditions(
					servicemesh.EnsureServiceMeshInstalled,
				).
				Manifests(
					path.Join(feature.ServiceMeshDir, "metrics-collection"),
				).
				Load()
			if errMetrics != nil {
				return errMetrics
			}
			initializer.Features = append(initializer.Features, metricsCollection)
		}

		cfMaps, cfgMapErr := feature.CreateFeature("shared-config-maps").
			With(initializer.DSCInitializationSpec).
			From(initializer.Source).
			WithResources(servicemesh.ConfigMaps).
			Load()
		if cfgMapErr != nil {
			return cfgMapErr
		}
		initializer.Features = append(initializer.Features, cfMaps)

		extAuthz, extAuthzErr := feature.CreateFeature("service-mesh-control-plane-setup-external-authorization").
			With(initializer.DSCInitializationSpec).
			From(initializer.Source).
			Manifests(
				path.Join(feature.AuthDir, "auth-smm.tmpl"),
				path.Join(feature.AuthDir, "base"),
				path.Join(feature.AuthDir, "mesh-authz-ext-provider.patch.tmpl"),
			).
			WithData(servicemesh.ClusterDetails).
			PreConditions(
				feature.EnsureCRDIsInstalled("authconfiginitializer.authorino.kuadrant.io"),
				servicemesh.EnsureServiceMeshInstalled,
			).
			PostConditions(
				feature.WaitForPodsToBeReady(serviceMeshSpec.ControlPlane.Namespace),
				func(f *feature.Feature) error {
					// We do not have the control over deployment resource creation.
					// It is created by Authorino operator using Authorino CR
					//
					// To make it part of Service Mesh we have to patch it with injection
					// enabled instead, otherwise it will not have proxy pod injected.
					return f.ApplyManifest(path.Join(feature.AuthDir, "deployment.injection.patch.tmpl"))
				},
			).
			OnDelete(servicemesh.RemoveExtensionProvider).
			Load()
		if extAuthzErr != nil {
			return extAuthzErr
		}
		initializer.Features = append(initializer.Features, extAuthz)

		return nil
	}
}

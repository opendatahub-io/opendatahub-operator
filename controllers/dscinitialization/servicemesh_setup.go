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
	// on condition of Managed, do not handle Removed when set to Removed it trigger DSCI reconcile to clean up
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
				path.Join(feature.ControlPlaneDir, "base", "create-control-plane.tmpl"),
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

		oauthErr := feature.CreateFeature("service-mesh-control-plane-configure-oauth").
			For(handler).
			Manifests(
				path.Join(feature.ControlPlaneDir, "base"),
				path.Join(feature.ControlPlaneDir, "oauth"),
				path.Join(feature.ControlPlaneDir, "filters"),
			).
			WithResources(
				servicemesh.DefaultValues,
				servicemesh.SelfSignedCertificate,
				servicemesh.EnvoyOAuthSecrets,
			).
			WithData(servicemesh.ClusterDetails, servicemesh.OAuthConfig).
			PreConditions(
				servicemesh.EnsureServiceMeshInstalled,
			).
			PostConditions(
				feature.WaitForPodsToBeReady(serviceMeshSpec.ControlPlane.Namespace),
			).
			OnDelete(
				servicemesh.RemoveOAuthClient,
				servicemesh.RemoveTokenVolumes,
			).Load()

		if oauthErr != nil {
			return oauthErr
		}

		cfMapsErr := feature.CreateFeature("shared-config-maps").
			For(handler).
			WithResources(servicemesh.ConfigMaps).
			Load()

		if cfMapsErr != nil {
			return cfMapsErr
		}

		// TODO rethink
		enrollAppNsErr := feature.CreateFeature("app-add-namespace-to-service-mesh").
			For(handler).
			Manifests(
				path.Join(feature.ControlPlaneDir, "smm.tmpl"),
				path.Join(feature.ControlPlaneDir, "namespace.patch.tmpl"),
			).
			WithData(servicemesh.ClusterDetails).
			Load()

		if enrollAppNsErr != nil {
			return enrollAppNsErr
		}

		// TODO make separate deployment
		gatewayRouteErr := feature.CreateFeature("service-mesh-create-gateway-route").
			For(handler).
			Manifests(
				path.Join(feature.ControlPlaneDir, "routing"),
			).
			WithData(servicemesh.ClusterDetails).
			PostConditions(
				feature.WaitForPodsToBeReady(serviceMeshSpec.ControlPlane.Namespace),
			).
			Load()

		if gatewayRouteErr != nil {
			return gatewayRouteErr
		}

		dataScienceProjectsErr := feature.CreateFeature("app-migrate-data-science-projects").
			For(handler).
			WithResources(servicemesh.MigratedDataScienceProjects).
			Load()

		if dataScienceProjectsErr != nil {
			return dataScienceProjectsErr
		}

		extAuthzErr := feature.CreateFeature("service-mesh-control-plane-setup-external-authorization").
			For(handler).
			Manifests(
				path.Join(feature.AuthDir, "auth-smm.tmpl"),
				path.Join(feature.AuthDir, "base"),
				path.Join(feature.AuthDir, "rbac"),
				path.Join(feature.AuthDir, "mesh-authz-ext-provider.patch.tmpl"),
			).
			WithData(servicemesh.ClusterDetails).
			PreConditions(
				feature.EnsureCRDIsInstalled("authconfigs.authorino.kuadrant.io"),
				servicemesh.EnsureServiceMeshInstalled,
				feature.CreateNamespaceIfNotExists(serviceMeshSpec.Auth.Namespace),
			).
			PostConditions(
				feature.WaitForPodsToBeReady(serviceMeshSpec.ControlPlane.Namespace),
				feature.WaitForPodsToBeReady(serviceMeshSpec.Auth.Namespace),
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

		return nil
	}
}

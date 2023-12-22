package dscinitialization

import (
	"path"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

func defineServiceMeshFeatures(f *feature.FeaturesInitializer) error {
	serviceMeshSpec := f.ServiceMesh

	smcpCreation, errSmcp := feature.CreateFeature("service-mesh-control-plane-creation").
		For(f.DSCInitializationSpec).
		Manifests(
			path.Join(feature.ControlPlaneDir, "base", "control-plane.tmpl"),
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
	f.Features = append(f.Features, smcpCreation)

	if serviceMeshSpec.ControlPlane.MetricsCollection == "Istio" {
		metricsCollection, errMetrics := feature.CreateFeature("service-mesh-monitoring").
			For(f.DSCInitializationSpec).
			Manifests(
				path.Join(feature.MonitoringDir),
			).
			PreConditions(
				servicemesh.EnsureServiceMeshInstalled,
			).
			Load()
		if errMetrics != nil {
			return errMetrics
		}

		f.Features = append(f.Features, metricsCollection)
	}

	if oauth, err := feature.CreateFeature("service-mesh-control-plane-configure-oauth").
		For(f.DSCInitializationSpec).
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
		).Load(); err != nil {
		return err
	} else {
		f.Features = append(f.Features, oauth)
	}

	if cfMaps, err := feature.CreateFeature("shared-config-maps").
		For(f.DSCInitializationSpec).
		WithResources(servicemesh.ConfigMaps).
		Load(); err != nil {
		return err
	} else {
		f.Features = append(f.Features, cfMaps)
	}

	if serviceMesh, err := feature.CreateFeature("app-add-namespace-to-service-mesh").
		For(f.DSCInitializationSpec).
		Manifests(
			path.Join(feature.ControlPlaneDir, "smm.tmpl"),
			path.Join(feature.ControlPlaneDir, "namespace.patch.tmpl"),
		).
		WithData(servicemesh.ClusterDetails).
		Load(); err != nil {
		return err
	} else {
		f.Features = append(f.Features, serviceMesh)
	}

	if gatewayRoute, err := feature.CreateFeature("service-mesh-create-gateway-route").
		For(f.DSCInitializationSpec).
		Manifests(
			path.Join(feature.ControlPlaneDir, "routing"),
		).
		WithData(servicemesh.ClusterDetails).
		PostConditions(
			feature.WaitForPodsToBeReady(serviceMeshSpec.ControlPlane.Namespace),
		).
		Load(); err != nil {
		return err
	} else {
		f.Features = append(f.Features, gatewayRoute)
	}

	if dataScienceProjects, err := feature.CreateFeature("app-migrate-data-science-projects").
		For(f.DSCInitializationSpec).
		WithResources(servicemesh.MigratedDataScienceProjects).
		Load(); err != nil {
		return err
	} else {
		f.Features = append(f.Features, dataScienceProjects)
	}

	if extAuthz, err := feature.CreateFeature("service-mesh-control-plane-setup-external-authorization").
		For(f.DSCInitializationSpec).
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
		Load(); err != nil {
		return err
	} else {
		f.Features = append(f.Features, extAuthz)
	}

	return nil
}

func (r *DSCInitializationReconciler) configureServiceMesh(instance *dsciv1.DSCInitialization) error {
	shouldConfigureServiceMesh, err := deploy.ShouldConfigureServiceMesh(r.Client, &instance.Spec)
	if !shouldConfigureServiceMesh || err != nil {
		return err
	}

	switch instance.Spec.ServiceMesh.ManagementState {
	case operatorv1.Managed:
		serviceMeshInitializer := feature.NewFeaturesInitializer(&instance.Spec, defineServiceMeshFeatures)
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
	// on condition of Managed, do not handle Removed when set to Removed it tigger DSCI reconcile to cleanup
	if instance.Spec.ServiceMesh.ManagementState == operatorv1.Managed {
		serviceMeshInitializer := feature.NewFeaturesInitializer(&instance.Spec, defineServiceMeshFeatures)

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

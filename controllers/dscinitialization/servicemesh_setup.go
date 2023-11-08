package dscinitialization

import (
	"path"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

func defineServiceMeshFeatures(f *feature.FeaturesInitializer) error {
	var rootDir = filepath.Join(feature.BaseOutputDir, f.DSCInitializationSpec.ApplicationsNamespace)
	if err := feature.CopyEmbeddedFiles("templates", rootDir); err != nil {
		return err
	}

	serviceMeshSpec := f.ServiceMesh

	if oauth, err := feature.CreateFeature("control-plane-configure-oauth").
		For(f.DSCInitializationSpec).
		Manifests(
			path.Join(rootDir, feature.ControlPlaneDir, "base"),
			path.Join(rootDir, feature.ControlPlaneDir, "oauth"),
			path.Join(rootDir, feature.ControlPlaneDir, "filters"),
		).
		WithResources(
			servicemesh.SelfSignedCertificate,
			servicemesh.EnvoyOAuthSecrets,
		).
		WithData(servicemesh.ClusterDetails, servicemesh.OAuthConfig).
		PreConditions(
			servicemesh.EnsureServiceMeshInstalled,
		).
		PostConditions(
			feature.WaitForPodsToBeReady(serviceMeshSpec.Mesh.Namespace),
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
			path.Join(rootDir, feature.ControlPlaneDir, "smm.tmpl"),
			path.Join(rootDir, feature.ControlPlaneDir, "namespace.patch.tmpl"),
		).
		WithData(servicemesh.ClusterDetails).
		Load(); err != nil {
		return err
	} else {
		f.Features = append(f.Features, serviceMesh)
	}

	if gatewayRoute, err := feature.CreateFeature("create-gateway-route").
		For(f.DSCInitializationSpec).
		Manifests(
			path.Join(rootDir, feature.ControlPlaneDir, "routing"),
		).
		WithData(servicemesh.ClusterDetails).
		PostConditions(
			feature.WaitForPodsToBeReady(serviceMeshSpec.Mesh.Namespace),
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

	if extAuthz, err := feature.CreateFeature("control-plane-setup-external-authorization").
		For(f.DSCInitializationSpec).
		Manifests(
			path.Join(rootDir, feature.AuthDir, "auth-smm.tmpl"),
			path.Join(rootDir, feature.AuthDir, "base"),
			path.Join(rootDir, feature.AuthDir, "rbac"),
			path.Join(rootDir, feature.AuthDir, "mesh-authz-ext-provider.patch.tmpl"),
		).
		WithData(servicemesh.ClusterDetails).
		PreConditions(
			feature.EnsureCRDIsInstalled("authconfigs.authorino.kuadrant.io"),
			servicemesh.EnsureServiceMeshInstalled,
			feature.CreateNamespace(serviceMeshSpec.Auth.Namespace),
		).
		PostConditions(
			feature.WaitForPodsToBeReady(serviceMeshSpec.Mesh.Namespace),
			feature.WaitForPodsToBeReady(serviceMeshSpec.Auth.Namespace),
			func(f *feature.Feature) error {
				// We do not have the control over deployment resource creation.
				// It is created by Authorino operator using Authorino CR
				//
				// To make it part of Service Mesh we have to patch it with injection
				// enabled instead, otherwise it will not have proxy pod injected.
				return f.ApplyManifest(path.Join(rootDir, feature.AuthDir, "deployment.injection.patch.tmpl"))
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

func (r *DSCInitializationReconciler) configureServiceMesh(instance *dsci.DSCInitialization) error {
	shouldConfigureServiceMesh, err := deploy.ShouldConfigureServiceMesh(r.Client, &instance.Spec)
	if err != nil {
		return err
	}

	if shouldConfigureServiceMesh {
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
	}

	return nil
}

func (r *DSCInitializationReconciler) cleanupServiceMesh(instance *dsci.DSCInitialization) error {
	shouldConfigureServiceMesh, err := deploy.ShouldConfigureServiceMesh(r.Client, &instance.Spec)
	if err != nil {
		return err
	}

	if shouldConfigureServiceMesh {
		serviceMeshInitializer := feature.NewFeaturesInitializer(&instance.Spec, defineServiceMeshFeatures)
		if err := serviceMeshInitializer.Prepare(); err != nil {
			return err
		}
		if err := serviceMeshInitializer.Delete(); err != nil {
			return err
		}
	}

	return nil
}

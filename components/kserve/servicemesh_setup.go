package kserve

import (
	"path"

	operatorv1 "github.com/openshift/api/operator/v1"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

func (k *Kserve) configureServiceMesh(dscispec *dsciv1.DSCInitializationSpec) error {
	if dscispec.ServiceMesh.ManagementState == operatorv1.Managed && k.GetManagementState() == operatorv1.Managed {
		serviceMeshInitializer := feature.ComponentFeaturesHandler(k.GetComponentName(), dscispec, k.defineServiceMeshFeatures())
		return serviceMeshInitializer.Apply()
	}
	if dscispec.ServiceMesh.ManagementState == operatorv1.Unmanaged && k.GetManagementState() == operatorv1.Managed {
		return nil
	}

	return k.removeServiceMeshConfigurations(dscispec)
}

func (k *Kserve) removeServiceMeshConfigurations(dscispec *dsciv1.DSCInitializationSpec) error {
	serviceMeshInitializer := feature.ComponentFeaturesHandler(k.GetComponentName(), dscispec, k.defineServiceMeshFeatures())
	return serviceMeshInitializer.Delete()
}

func (k *Kserve) defineServiceMeshFeatures() feature.FeaturesProvider {
	return func(handler *feature.FeaturesHandler) error {
		kserveExtAuthzErr := feature.CreateFeature("kserve-external-authz").
			For(handler).
			ManifestSource(Templates.Files).
			Manifests(
				path.Join(Templates.ServiceMeshDir, "activator-envoyfilter.tmpl.yaml"),
				path.Join(Templates.ServiceMeshDir, "envoy-oauth-temp-fix.tmpl.yaml"),
				path.Join(Templates.ServiceMeshDir, "kserve-predictor-authorizationpolicy.tmpl.yaml"),
				path.Join(Templates.ServiceMeshDir, "z-migrations"),
			).
			WithData(servicemesh.ClusterDetails).
			Load()

		if kserveExtAuthzErr != nil {
			return kserveExtAuthzErr
		}

		temporaryFixesErr := feature.CreateFeature("kserve-temporary-fixes").
			For(handler).
			ManifestSource(Templates.Files).
			Manifests(
				path.Join(Templates.ServiceMeshDir, "grpc-envoyfilter-temp-fix.tmpl.yaml"),
			).
			WithData(servicemesh.ClusterDetails).
			Load()

		if temporaryFixesErr != nil {
			return temporaryFixesErr
		}

		return nil
	}
}

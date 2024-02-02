package kserve

import (
	"path"

	operatorv1 "github.com/openshift/api/operator/v1"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

func (k *Kserve) configureServiceMesh(dscispec *dsciv1.DSCInitializationSpec) error {
	if dscispec.ServiceMesh.ManagementState != operatorv1.Managed || k.GetManagementState() != operatorv1.Managed {
		return nil
	}

	serviceMeshInitializer := feature.ComponentFeaturesHandler(k, dscispec, k.defineServiceMeshFeatures())

	return serviceMeshInitializer.Apply()
}

func (k *Kserve) defineServiceMeshFeatures() feature.FeaturesProvider {
	return func(handler *feature.FeaturesHandler) error {
		kserveExtAuthzErr := feature.CreateFeature("configure-kserve-for-external-authz").
			For(handler).
			Manifests(
				path.Join(feature.KServeDir),
			).
			WithData(servicemesh.ClusterDetails).
			Load()

		if kserveExtAuthzErr != nil {
			return kserveExtAuthzErr
		}

		return nil
	}
}

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

	serviceMeshInitializer := feature.ComponentFeaturesInitializer(k, dscispec, k.defineServiceMeshFeatures())

	if err := serviceMeshInitializer.Prepare(); err != nil {
		return err
	}

	return serviceMeshInitializer.Apply()
}

func (k *Kserve) defineServiceMeshFeatures() feature.DefinedFeatures {
	return func(initializer *feature.FeaturesInitializer) error {
		kserve, err := feature.CreateFeature("configure-kserve-for-external-authz").
			With(initializer.DSCInitializationSpec).
			From(initializer.Source).
			Manifests(
				path.Join(feature.KServeDir),
			).
			WithData(servicemesh.ClusterDetails).
			Load()

		if err != nil {
			return err
		}

		initializer.Features = append(initializer.Features, kserve)

		return nil
	}
}

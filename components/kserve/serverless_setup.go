package kserve

import (
	"path"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/serverless"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

func (k *Kserve) configureServerlessFeatures(dsciSpec *dsciv1.DSCInitializationSpec) feature.FeaturesProvider {
	return func(registry feature.FeaturesRegistry) error {
		servingDeployment := feature.Define("serverless-serving-deployment").
			ManifestsLocation(Resources.Location).
			Manifests(
				path.Join(Resources.InstallDir),
			).
			WithData(
				serverless.FeatureData.IngressDomain.Define(&k.Serving).AsAction(),
				serverless.FeatureData.Serving.Define(&k.Serving).AsAction(),
				servicemesh.FeatureData.ControlPlane.Define(dsciSpec).AsAction(),
			).
			PreConditions(
				serverless.EnsureServerlessOperatorInstalled,
				serverless.EnsureServerlessAbsent,
				servicemesh.EnsureServiceMeshInstalled,
				feature.CreateNamespaceIfNotExists(serverless.KnativeServingNamespace),
			).
			PostConditions(
				feature.WaitForPodsToBeReady(serverless.KnativeServingNamespace),
			)

		istioSecretFiltering := feature.Define("serverless-net-istio-secret-filtering").
			ManifestsLocation(Resources.Location).
			Manifests(
				path.Join(Resources.BaseDir, "serving-net-istio-secret-filtering.patch.tmpl.yaml"),
			).
			WithData(serverless.FeatureData.Serving.Define(&k.Serving).AsAction()).
			PreConditions(serverless.EnsureServerlessServingDeployed).
			PostConditions(
				feature.WaitForPodsToBeReady(serverless.KnativeServingNamespace),
			)

		servingGateway := feature.Define("serverless-serving-gateways").
			ManifestsLocation(Resources.Location).
			Manifests(
				path.Join(Resources.GatewaysDir),
			).
			WithData(
				serverless.FeatureData.IngressDomain.Define(&k.Serving).AsAction(),
				serverless.FeatureData.CertificateName.Define(&k.Serving).AsAction(),
				serverless.FeatureData.Serving.Define(&k.Serving).AsAction(),
				servicemesh.FeatureData.ControlPlane.Define(dsciSpec).AsAction(),
			).
			WithResources(serverless.ServingCertificateResource).
			PreConditions(serverless.EnsureServerlessServingDeployed)

		return registry.Add(
			servingDeployment,
			istioSecretFiltering,
			servingGateway,
		)
	}
}

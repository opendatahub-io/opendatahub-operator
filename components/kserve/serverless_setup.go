package kserve

import (
	"path"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/serverless"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

func (k *Kserve) configureServerlessFeatures() feature.FeaturesProvider {
	return func(handler *feature.FeaturesHandler) error {
		servingDeploymentErr := feature.CreateFeature("serverless-serving-deployment").
			For(handler).
			Manifests(
				path.Join(feature.ServerlessDir, "serving-install"),
			).
			WithData(PopulateComponentSettings(k)).
			PreConditions(
				serverless.EnsureServerlessOperatorInstalled,
				serverless.EnsureServerlessAbsent,
				servicemesh.EnsureServiceMeshInstalled,
				feature.CreateNamespaceIfNotExists(serverless.KnativeServingNamespace),
			).
			PostConditions(
				feature.WaitForPodsToBeReady(serverless.KnativeServingNamespace),
			).
			Load()
		if servingDeploymentErr != nil {
			return servingDeploymentErr
		}

		servingNetIstioSecretFilteringErr := feature.CreateFeature("serverless-net-istio-secret-filtering").
			For(handler).
			Manifests(
				path.Join(feature.ServerlessDir, "serving-net-istio-secret-filtering.patch.tmpl"),
			).
			WithData(PopulateComponentSettings(k)).
			PreConditions(serverless.EnsureServerlessServingDeployed).
			PostConditions(
				feature.WaitForPodsToBeReady(serverless.KnativeServingNamespace),
			).
			Load()
		if servingNetIstioSecretFilteringErr != nil {
			return servingNetIstioSecretFilteringErr
		}

		serverlessGwErr := feature.CreateFeature("serverless-serving-gateways").
			For(handler).
			PreConditions(serverless.EnsureServerlessServingDeployed).
			WithData(
				PopulateComponentSettings(k),
				serverless.ServingDefaultValues,
				serverless.ServingIngressDomain,
			).
			WithResources(serverless.ServingCertificateResource).
			Manifests(
				path.Join(feature.ServerlessDir, "serving-istio-gateways"),
			).
			Load()
		if serverlessGwErr != nil {
			return serverlessGwErr
		}

		return nil
	}
}

func PopulateComponentSettings(k *Kserve) feature.Action {
	return func(f *feature.Feature) error {
		f.Spec.Serving = &k.Serving
		return nil
	}
}

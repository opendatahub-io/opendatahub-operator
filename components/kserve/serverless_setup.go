package kserve

import (
	"path"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/serverless"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/gvr"
)

const (
	knativeServingNamespace = "knative-serving"
	templatesDir            = "templates/serverless"
)

func (k *Kserve) configureServerlessFeatures() feature.DefinedFeatures {
	return func(initializer *feature.FeaturesInitializer) error {
		servingDeployment, err := feature.CreateFeature("serverless-serving-deployment").
			With(initializer.DSCInitializationSpec).
			From(initializer.Source).
			Manifests(
				path.Join(templatesDir, "serving-install"),
			).
			WithData(PopulateComponentSettings(k)).
			PreConditions(
				serverless.EnsureServerlessOperatorInstalled,
				serverless.EnsureServerlessAbsent,
				servicemesh.EnsureServiceMeshInstalled,
				feature.CreateNamespaceIfNotExists(knativeServingNamespace),
			).
			PostConditions(
				feature.WaitForPodsToBeReady(knativeServingNamespace),
			).
			Load()
		if err != nil {
			return err
		}
		initializer.Features = append(initializer.Features, servingDeployment)

		servingIstioGateways, err := feature.CreateFeature("serverless-serving-gateways").
			With(initializer.DSCInitializationSpec).
			From(initializer.Source).
			PreConditions(
				// Check serverless is installed
				feature.WaitForResourceToBeCreated(knativeServingNamespace, gvr.KnativeServing)).
			WithData(
				serverless.ServingDefaultValues,
				serverless.ServingIngressDomain,
				PopulateComponentSettings(k),
			).
			WithResources(serverless.ServingCertificateResource).
			Manifests(
				path.Join(templatesDir, "serving-istio-gateways"),
			).
			Load()
		if err != nil {
			return err
		}
		initializer.Features = append(initializer.Features, servingIstioGateways)
		return nil
	}
}

func PopulateComponentSettings(k *Kserve) feature.Action {
	return func(f *feature.Feature) error {
		f.Spec.Serving = &k.Serving
		return nil
	}
}

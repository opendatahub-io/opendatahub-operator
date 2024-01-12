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

func (k *Kserve) configureServerlessFeatures(s *feature.FeaturesInitializer) error {
	servingDeployment, err := feature.CreateFeature("serverless-serving-deployment").
		For(s.DSCInitializationSpec).
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
	s.Features = append(s.Features, servingDeployment)

	servingIstioGateways, err := feature.CreateFeature("serverless-serving-gateways").
		For(s.DSCInitializationSpec).
		PreConditions(
			// Check serverless is installed
			feature.WaitForResourceToBeCreated(knativeServingNamespace, gvr.KnativeServing),
		).
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
	s.Features = append(s.Features, servingIstioGateways)

	return nil
}

func PopulateComponentSettings(k *Kserve) feature.Action {
	return func(f *feature.Feature) error {
		f.Spec.Serving = &k.Serving
		return nil
	}
}

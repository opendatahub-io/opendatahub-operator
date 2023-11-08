package serverless

import (
	"path"
	"path/filepath"

	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/gvr"
)

const (
	knativeServingNamespace = "knative-serving"
	templatesDir            = "templates/serverless"
)

var log = ctrlLog.Log.WithName("features")

func ConfigureServerlessFeatures(s *feature.FeaturesInitializer) error {
	var rootDir = filepath.Join(feature.BaseOutputDir, s.DSCInitializationSpec.ApplicationsNamespace)
	if err := feature.CopyEmbeddedFiles(templatesDir, rootDir); err != nil {
		return err
	}

	servingDeployment, err := feature.CreateFeature("serverless-serving-deployment").
		For(s.DSCInitializationSpec).
		Manifests(
			path.Join(rootDir, templatesDir, "serving-install"),
		).
		PreConditions(
			EnsureServerlessOperatorInstalled,
			EnsureServerlessAbsent,
			servicemesh.EnsureServiceMeshInstalled,
			feature.CreateNamespace(knativeServingNamespace),
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
		WithData(ServingDefaultValues, ServingIngressDomain).
		WithResources(ServingCertificateResource).
		Manifests(
			path.Join(rootDir, templatesDir, "serving-istio-gateways"),
		).
		Load()
	if err != nil {
		return err
	}
	s.Features = append(s.Features, servingIstioGateways)

	return nil
}

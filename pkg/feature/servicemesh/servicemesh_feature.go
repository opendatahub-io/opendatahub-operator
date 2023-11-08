package servicemesh

import (
	"path"
	"path/filepath"

	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

const templatesDir = "templates/servicemesh"

var log = ctrlLog.Log.WithName("features")

func ConfigureServiceMeshFeatures(s *feature.FeaturesInitializer) error {
	var rootDir = filepath.Join(feature.BaseOutputDir, s.DSCInitializationSpec.ApplicationsNamespace)
	if err := feature.CopyEmbeddedFiles(templatesDir, rootDir); err != nil {
		return err
	}

	serviceMeshSpec := s.ServiceMesh

	smcpCreation, errSmcp := feature.CreateFeature("mesh-control-plane-creation").
		For(s.DSCInitializationSpec).
		Manifests(
			path.Join(rootDir, templatesDir, "/base"),
		).
		PreConditions(
			EnsureServiceMeshOperatorInstalled,
			feature.CreateNamespace(serviceMeshSpec.ControlPlane.Namespace),
		).
		PostConditions(
			feature.WaitForPodsToBeReady(serviceMeshSpec.ControlPlane.Namespace),
		).
		Load()
	if errSmcp != nil {
		return errSmcp
	}
	s.Features = append(s.Features, smcpCreation)

	if serviceMeshSpec.ControlPlane.MetricsCollection == "Istio" {
		metricsCollection, errMetrics := feature.CreateFeature("mesh-metrics-collection").
			For(s.DSCInitializationSpec).
			Manifests(
				path.Join(rootDir, templatesDir, "metrics-collection"),
			).
			PreConditions(
				EnsureServiceMeshInstalled,
			).
			Load()
		if errMetrics != nil {
			return errMetrics
		}
		s.Features = append(s.Features, metricsCollection)
	}

	return nil
}

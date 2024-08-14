package kserve

import (
	"context"
	"fmt"
	"path"

	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/manifest"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/serverless"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

func (k *Kserve) configureServerlessFeatures(ctx context.Context, cli client.Client, dsciSpec *dsciv1.DSCInitializationSpec) feature.FeaturesProvider {
	return func(registry feature.FeaturesRegistry) error {
		serving, errServing := serverless.FeatureData.Serving.Create(ctx, cli, &k.Serving)
		if errServing != nil {
			return fmt.Errorf("failed to create serving feature data: %w", errServing)
		}

		controlPlane, errControlPlane := servicemesh.FeatureData.ControlPlane.Create(ctx, cli, dsciSpec)
		if errControlPlane != nil {
			return fmt.Errorf("failed to create control plane feature data: %w", errControlPlane)
		}

		servingDeployment := feature.Define("serverless-serving-deployment").
			Manifests(
				manifest.Location(Resources.Location).
					Include(
						path.Join(Resources.InstallDir),
					),
			).
			WithData(serving, controlPlane).
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
			Manifests(
				manifest.Location(Resources.Location).
					Include(
						path.Join(Resources.BaseDir, "serving-net-istio-secret-filtering.patch.tmpl.yaml"),
					),
			).
			WithData(serving).
			PreConditions(serverless.EnsureServerlessServingDeployed).
			PostConditions(
				feature.WaitForPodsToBeReady(serverless.KnativeServingNamespace),
			)

		servingGateway := feature.Define("serverless-serving-gateways").
			Manifests(
				manifest.Location(Resources.Location).
					Include(
						path.Join(Resources.GatewaysDir),
					),
			).
			WithData(serving, controlPlane).
			WithResources(serverless.ServingCertificateResource).
			PreConditions(serverless.EnsureServerlessServingDeployed)

		return registry.Add(
			servingDeployment,
			istioSecretFiltering,
			servingGateway,
		)
	}
}

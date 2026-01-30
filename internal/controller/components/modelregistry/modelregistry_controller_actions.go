package modelregistry

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = []odhtypes.ManifestInfo{
		baseManifestInfo(BaseManifestsSourcePath),
		extraManifestInfo(BaseManifestsSourcePath),
	}

	return nil
}

func customizeManifests(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	mr, ok := rr.Instance.(*componentApi.ModelRegistry)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelRegistry)", rr.Instance)
	}

	// Compute ODH domain/subdomain params from GatewayConfig
	extraParamsMap, err := computeKustomizeVariable(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("failed to compute gateway domain: %w", err)
	}

	// Add registries namespace to params
	extraParamsMap["REGISTRIES_NAMESPACE"] = mr.Spec.RegistriesNamespace

	// update params.env with all computed values
	if err := odhdeploy.ApplyParams(rr.Manifests[0].String(), "params.env", nil, extraParamsMap); err != nil {
		return fmt.Errorf("failed to update params on path %s: %w", rr.Manifests[0].String(), err)
	}

	return nil
}

// updateParamsHashAnnotations updates both the component instance annotation and the Deployment pod template
// annotation with params hash. This action must run AFTER kustomize action so that rr.Resources is populated.
func updateParamsHashAnnotations(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	mr, ok := rr.Instance.(*componentApi.ModelRegistry)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelRegistry", rr.Instance)
	}

	// Compute params (same logic as customizeManifests)
	extraParamsMap, err := computeKustomizeVariable(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("failed to compute gateway domain: %w", err)
	}

	// Add registries namespace to params
	extraParamsMap["REGISTRIES_NAMESPACE"] = mr.Spec.RegistriesNamespace

	// Update ModelRegistry annotation with params hash to invalidate kustomize cache
	// when params change. This ensures the ConfigMap gets updated.
	resources.UpdateParamsHashAnnotation(ctx, rr.Client, rr.Instance, extraParamsMap, ModelRegistryParamsHashAnnotation)

	// Update Deployment pod template annotation with params hash to trigger pod restart
	// when params change. This ensures pods pick up the new ConfigMap values.
	if err := resources.UpdateDeploymentPodTemplateAnnotation(
		ctx,
		rr.Resources,
		labels.ODH.Component(LegacyComponentName),
		extraParamsMap,
		ModelRegistryDeploymentParamsHashAnnotation,
	); err != nil {
		return fmt.Errorf("failed to update deployment pod template annotation: %w", err)
	}

	return nil
}

// computeKustomizeVariable computes the ODH domain/subdomain from GatewayConfig
// for use in model-registry-operator-parameters ConfigMap.
func computeKustomizeVariable(ctx context.Context, cli client.Client) (map[string]string, error) {
	// Get the gateway domain from GatewayConfig
	gatewayDomain, err := gateway.GetGatewayDomain(ctx, cli)
	if err != nil {
		return nil, fmt.Errorf("error getting gateway domain: %w", err)
	}

	return map[string]string{
		"GATEWAY_DOMAIN": gatewayDomain,
	}, nil
}

func configureDependencies(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	mr, ok := rr.Instance.(*componentApi.ModelRegistry)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelRegistry)", rr.Instance)
	}

	// Namespace
	if err := rr.AddResources(
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: mr.Spec.RegistriesNamespace,
			},
		},
	); err != nil {
		return fmt.Errorf("failed to add namespace %s to manifests: %w", mr.Spec.RegistriesNamespace, err)
	}

	return nil
}

func updateStatus(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	mr, ok := rr.Instance.(*componentApi.ModelRegistry)
	if !ok {
		return errors.New("instance is not of type *odhTypes.ModelRegistry")
	}

	mr.Status.RegistriesNamespace = mr.Spec.RegistriesNamespace

	return nil
}

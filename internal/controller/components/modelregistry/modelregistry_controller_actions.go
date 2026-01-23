package modelregistry

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
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

	extraParamsMap, err := computeKustomizeVariable(rr)
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

func computeKustomizeVariable(rr *odhtypes.ReconciliationRequest) (map[string]string, error) {
	mr, ok := rr.Instance.(*componentApi.ModelRegistry)
	if !ok {
		return nil, errors.New("instance is not a ModelRegistry")
	}

	// Use domain from ModelRegistry.Spec.Gateway.Domain (synced from GatewayConfig.Status.Domain by DSC controller)
	var domain string
	if mr.Spec.Gateway != nil {
		domain = mr.Spec.Gateway.Domain
	}
	if domain == "" {
		return nil, errors.New(
			"gateway domain is missing for ModelRegistry; the Data Science Gateway may not be ready yet—check that " +
				"GatewayConfig exists and its status reports a domain")
	}

	return map[string]string{
		"GATEWAY_DOMAIN": domain,
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

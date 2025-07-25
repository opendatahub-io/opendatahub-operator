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
	mr, ok := rr.Instance.(*componentApi.ModelRegistry)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelRegistry)", rr.Instance)
	}

	rr.Manifests = []odhtypes.ManifestInfo{
		baseManifestInfo(BaseManifestsSourcePath),
		extraManifestInfo(BaseManifestsSourcePath),
	}

	df := mr.GetDevFlags()

	if df == nil {
		return nil
	}
	if len(df.Manifests) == 0 {
		return nil
	}
	if len(df.Manifests) > 1 {
		return fmt.Errorf("unexpected number of manifests found: %d, expected 1)", len(df.Manifests))
	}

	if err := odhdeploy.DownloadManifests(ctx, ComponentName, df.Manifests[0]); err != nil {
		return err
	}

	if df.Manifests[0].SourcePath != "" {
		rr.Manifests = []odhtypes.ManifestInfo{
			baseManifestInfo(df.Manifests[0].SourcePath),
			extraManifestInfo(df.Manifests[0].SourcePath),
		}
	}

	return nil
}

func customizeManifests(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	mr, ok := rr.Instance.(*componentApi.ModelRegistry)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelRegistry)", rr.Instance)
	}

	// update registries namespace in manifests
	if err := odhdeploy.ApplyParams(rr.Manifests[0].String(), nil, map[string]string{
		"REGISTRIES_NAMESPACE": mr.Spec.RegistriesNamespace,
	}); err != nil {
		return fmt.Errorf("failed to update params on path %s: %w", rr.Manifests[0].String(), err)
	}
	return nil
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

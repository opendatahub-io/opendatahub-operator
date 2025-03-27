package kueue

import (
	"context"
	"fmt"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	MultiKueueCRDMessage = "Kueue CRDs MultiKueueConfig v1alpha1 and/or MultiKueueCluster v1alpha1 exist, please remove them to proceed"
)

func checkPreConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rConfig, err := cluster.HasCRD(ctx, rr.Client, gvk.MultiKueueConfigV1Alpha1)
	if err != nil {
		return odherrors.NewStopError("failed to check %s CRDs version: %w", gvk.MultiKueueConfigV1Alpha1, err)
	}

	rCluster, err := cluster.HasCRD(ctx, rr.Client, gvk.MultikueueClusterV1Alpha1)
	if err != nil {
		return odherrors.NewStopError("failed to check %s CRDs version: %w", gvk.MultikueueClusterV1Alpha1, err)
	}

	if rConfig || rCluster {
		return odherrors.NewStopError(MultiKueueCRDMessage)
	}

	return nil
}

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = append(rr.Manifests, manifestsPath())
	return nil
}

func extraInitialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	// Add specific manifests if OCP is greater or equal 4.17.
	rr.Manifests = append(rr.Manifests, extramanifestsPath())
	return nil
}

func devFlags(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	kueue, ok := rr.Instance.(*componentApi.Kueue)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kueue)", rr.Instance)
	}

	if kueue.Spec.DevFlags == nil {
		return nil
	}

	// Implement devflags support logic
	// If dev flags are set, update default manifests path
	if len(kueue.Spec.DevFlags.Manifests) != 0 {
		manifestConfig := kueue.Spec.DevFlags.Manifests[0]
		if err := odhdeploy.DownloadManifests(ctx, ComponentName, manifestConfig); err != nil {
			return err
		}

		if manifestConfig.SourcePath != "" {
			rr.Manifests[0].Path = odhdeploy.DefaultManifestPath
			rr.Manifests[0].ContextDir = ComponentName
			rr.Manifests[0].SourcePath = manifestConfig.SourcePath
		}
	}

	return nil
}

func customizeResources(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	for i := range rr.Resources {
		if rr.Resources[i].GroupVersionKind() == gvk.ValidatingAdmissionPolicyBinding {
			// admin can update this resource
			resources.SetAnnotation(&rr.Resources[i], annotations.ManagedByODHOperator, "false")
			break // fast exist function
		}
	}
	return nil
}

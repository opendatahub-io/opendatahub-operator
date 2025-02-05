package kueue

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func checkPreConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	k, ok := rr.Instance.(*componentApi.Kueue)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kueue)", rr.Instance)
	}

	rConfig, eConfig := cluster.HasCRDWithVersion(ctx, rr.Client, gvk.MultiKueueConfigV1Alpha1.GroupKind(), gvk.MultiKueueConfigV1Alpha1.Version)
	rCluster, eCluster := cluster.HasCRDWithVersion(ctx, rr.Client, gvk.MultikueueClusterV1Alpha1.GroupKind(), gvk.MultikueueClusterV1Alpha1.Version)
	if eConfig != nil || eCluster != nil {
		return odherrors.NewStopError("failed to check CRDs version: %v, %v", eConfig, eCluster)
	}
	if rConfig || rCluster {
		s := k.GetStatus()
		s.Phase = status.PhaseNotReady
		meta.SetStatusCondition(&s.Conditions, metav1.Condition{
			Type:               status.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             status.MultiKueueCRDReason,
			Message:            status.MultiKueueCRDMessage,
			ObservedGeneration: s.ObservedGeneration,
		})
		return odherrors.NewStopError(status.MultiKueueCRDMessage)
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

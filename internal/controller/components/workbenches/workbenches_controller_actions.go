package workbenches

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = []odhtypes.ManifestInfo{
		notebookControllerManifestInfo(notebookControllerManifestSourcePath),
		kfNotebookControllerManifestInfo(kfNotebookControllerManifestSourcePath),
		notebookImagesManifestInfo(notebookImagesManifestSourcePath),
	}

	return nil
}

func configureDependencies(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	workbench, ok := rr.Instance.(*componentApi.Workbenches)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Workbenches", rr.Instance)
	}

	wbNS := &corev1.Namespace{}
	wbNS.Labels = map[string]string{
		labels.ODH.OwnedNamespace: "true",
	}

	if workbench.Spec.WorkbenchNamespace != "" || len(workbench.Spec.WorkbenchNamespace) > 0 {
		wbNS.Name = workbench.Spec.WorkbenchNamespace
	} else {
		switch rr.Release.Name {
		case cluster.SelfManagedRhoai, cluster.ManagedRhoai:
			wbNS.Name = cluster.DefaultNotebooksNamespaceRHOAI
		case cluster.OpenDataHub:
			wbNS.Name = cluster.DefaultNotebooksNamespaceODH
		}
	}

	err := rr.AddResources(wbNS)
	if err != nil {
		return fmt.Errorf("failed to create namespace for workbenches: %w", err)
	}
	return nil
}

func updateStatus(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	workbench, ok := rr.Instance.(*componentApi.Workbenches)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Workbenches", rr.Instance)
	}
	workbench.Status.WorkbenchNamespace = workbench.Spec.WorkbenchNamespace

	return nil
}

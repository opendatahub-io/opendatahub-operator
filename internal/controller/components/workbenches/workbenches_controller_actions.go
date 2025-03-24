package workbenches

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
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

func devFlags(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	workbenches, ok := rr.Instance.(*componentApi.Workbenches)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Workbenches)", rr.Instance)
	}

	if workbenches.Spec.DevFlags == nil || len(workbenches.Spec.DevFlags.Manifests) == 0 {
		return nil
	}

	// Download manifests if defined by devflags
	// Go through each manifest and set the overlays if defined
	// first on odh-notebook-controller and kf-notebook-controller last to notebook-images
	nbcSourcePath := notebookControllerManifestSourcePath
	kfNbcSourcePath := kfNotebookControllerManifestSourcePath
	nbImgsSourcePath := notebookImagesManifestSourcePath

	for _, subcomponent := range workbenches.Spec.DevFlags.Manifests {
		if strings.Contains(subcomponent.ContextDir, "components/odh-notebook-controller") {
			// Download subcomponent
			if err := odhdeploy.DownloadManifests(ctx, notebookControllerContextDir, subcomponent); err != nil {
				return err
			}
			// If overlay is defined, update paths
			if subcomponent.SourcePath != "" {
				nbcSourcePath = subcomponent.SourcePath
			}
		}

		if strings.Contains(subcomponent.ContextDir, "components/notebook-controller") {
			// Download subcomponent
			if err := odhdeploy.DownloadManifests(ctx, kfNotebookControllerContextDir, subcomponent); err != nil {
				return err
			}
			// If overlay is defined, update paths
			if subcomponent.SourcePath != "" {
				kfNbcSourcePath = subcomponent.SourcePath
			}
		}

		if strings.Contains(subcomponent.URI, notebooksPath) {
			// Download subcomponent
			if err := odhdeploy.DownloadManifests(ctx, notebookContextDir, subcomponent); err != nil {
				return err
			}
			// If overlay is defined, update paths
			if subcomponent.SourcePath != "" {
				nbImgsSourcePath = subcomponent.SourcePath
			}
		}
	}

	rr.Manifests = []odhtypes.ManifestInfo{
		notebookControllerManifestInfo(nbcSourcePath),
		kfNotebookControllerManifestInfo(kfNbcSourcePath),
		notebookImagesManifestInfo(nbImgsSourcePath),
	}

	return nil
}

func configureDependencies(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	_, ok := rr.Instance.(*componentApi.Workbenches)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Workbenches)", rr.Instance)
	}

	platform := rr.Release.Name
	if platform == cluster.SelfManagedRhoai || platform == cluster.ManagedRhoai {
		// Intentionally leaving the ownership unset for this namespace.
		// Specifying this label triggers its deletion when the operator is uninstalled.
		if err := rr.AddResources(&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: cluster.DefaultNotebooksNamespace,
				Labels: map[string]string{
					labels.ODH.OwnedNamespace: "true",
				},
			},
		}); err != nil {
			return fmt.Errorf("failed to add namespace %s to manifests", cluster.DefaultNotebooksNamespace)
		}
	}

	return nil
}

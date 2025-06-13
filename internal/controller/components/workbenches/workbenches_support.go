package workbenches

import (
	"path"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.WorkbenchesComponentName

	ReadyConditionType = componentApi.WorkbenchesKind + status.ReadySuffix

	notebooksPath                    = "notebooks"
	notebookImagesManifestSourcePath = "overlays/additional"

	notebookControllerPath               = "odh-notebook-controller"
	notebookControllerManifestSourcePath = "base"

	kfNotebookControllerPath               = "kf-notebook-controller"
	kfNotebookControllerManifestSourcePath = "overlays/openshift"
)

var (
	notebookControllerContextDir   = path.Join(ComponentName, notebookControllerPath)
	kfNotebookControllerContextDir = path.Join(ComponentName, kfNotebookControllerPath)
	notebookContextDir             = path.Join(ComponentName, notebooksPath)
)

var (
	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

// manifests for nbc in ODH and RHOAI + downstream use it for imageparams.
func notebookControllerManifestInfo(sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: notebookControllerContextDir,
		SourcePath: sourcePath,
	}
}

// manifests for ODH nbc + downstream use it for imageparams.
func kfNotebookControllerManifestInfo(sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: kfNotebookControllerContextDir,
		SourcePath: sourcePath,
	}
}

// notebook image manifests.
func notebookImagesManifestInfo(sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: notebookContextDir,
		SourcePath: sourcePath,
	}
}

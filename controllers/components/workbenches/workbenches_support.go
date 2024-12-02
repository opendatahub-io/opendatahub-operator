package workbenches

import (
	componentsv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName          = componentsv1alpha1.WorkbenchesComponentName
	DependentComponentName = "notebooks"

	notebookControllerManifestSourcePath = "base"
	notebookControllerManifestContextDir = "odh-notebook-controller/odh-notebook-controller"

	kfNotebookControllerManifestSourcePath = "overlays/openshift"
	kfNotebookControllerManifestContextDir = "odh-notebook-controller/kf-notebook-controller"

	notebookImagesManifestSourcePath = "overlays/additional"

	nbcServiceAccountName = "notebook-controller-service-account"
)

var serviceAccounts = map[cluster.Platform][]string{
	cluster.SelfManagedRhods: {nbcServiceAccountName},
	cluster.ManagedRhods:     {nbcServiceAccountName},
	cluster.OpenDataHub:      {nbcServiceAccountName},
	cluster.Unknown:          {nbcServiceAccountName},
}

// manifests for nbc in ODH and RHOAI + downstream use it for imageparams.
func notebookControllerManifestInfo(sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: notebookControllerManifestContextDir,
		SourcePath: sourcePath,
	}
}

// manifests for ODH nbc + downstream use it for imageparams.
func kfNotebookControllerManifestInfo(sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: kfNotebookControllerManifestContextDir,
		SourcePath: sourcePath,
	}
}

// notebook image manifests.
func notebookImagesManifestInfo(sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: DependentComponentName,
		SourcePath: sourcePath,
	}
}

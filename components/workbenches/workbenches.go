// Package workbenches provides utility functions to config Workbenches to secure Jupyter Notebook in Kubernetes environments with support for OAuth
// +groupName=datasciencecluster.opendatahub.io
package workbenches

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

var (
	ComponentName          = "workbenches"
	DependentComponentName = "notebooks"
	// manifests for nbc in ODH and RHOAI + downstream use it for imageparams.
	notebookControllerPath = deploy.DefaultManifestPath + "/odh-notebook-controller/odh-notebook-controller/base"
	// manifests for ODH nbc + downstream use it for imageparams.
	kfnotebookControllerPath = deploy.DefaultManifestPath + "/odh-notebook-controller/kf-notebook-controller/overlays/openshift"
	// notebook image manifests.
	notebookImagesPath = deploy.DefaultManifestPath + "/notebooks/overlays/additional"
)

// Verifies that Workbench implements ComponentInterface.
var _ components.ComponentInterface = (*Workbenches)(nil)

// Workbenches struct holds the configuration for the Workbenches component.
// +kubebuilder:object:generate=true
type Workbenches struct {
	components.Component `json:""`
}

func (w *Workbenches) Init(ctx context.Context, _ cluster.Platform) error {
	log := logf.FromContext(ctx).WithName(ComponentName)

	var imageParamMap = map[string]string{
		"odh-notebook-controller-image":    "RELATED_IMAGE_ODH_NOTEBOOK_CONTROLLER_IMAGE",
		"odh-kf-notebook-controller-image": "RELATED_IMAGE_ODH_KF_NOTEBOOK_CONTROLLER_IMAGE",
	}

	// for kf-notebook-controller image
	if err := deploy.ApplyParams(notebookControllerPath, imageParamMap); err != nil {
		log.Error(err, "failed to update image", "path", notebookControllerPath)
	}
	// for odh-notebook-controller image
	if err := deploy.ApplyParams(kfnotebookControllerPath, imageParamMap); err != nil {
		log.Error(err, "failed to update image", "path", kfnotebookControllerPath)
	}

	return nil
}

func (w *Workbenches) OverrideManifests(ctx context.Context, platform cluster.Platform) error {
	// Download manifests if defined by devflags
	// Go through each manifest and set the overlays if defined
	// first on odh-notebook-controller and kf-notebook-controller last to notebook-images
	for _, subcomponent := range w.DevFlags.Manifests {
		if strings.Contains(subcomponent.ContextDir, "components/odh-notebook-controller") {
			// Download subcomponent
			if err := deploy.DownloadManifests(ctx, "odh-notebook-controller/odh-notebook-controller", subcomponent); err != nil {
				return err
			}
			// If overlay is defined, update paths
			defaultKustomizePathNbc := "base"
			if subcomponent.SourcePath != "" {
				defaultKustomizePathNbc = subcomponent.SourcePath
			}
			notebookControllerPath = filepath.Join(deploy.DefaultManifestPath, "odh-notebook-controller/odh-notebook-controller", defaultKustomizePathNbc)
		}

		if strings.Contains(subcomponent.ContextDir, "components/notebook-controller") {
			// Download subcomponent
			if err := deploy.DownloadManifests(ctx, "odh-notebook-controller/kf-notebook-controller", subcomponent); err != nil {
				return err
			}
			// If overlay is defined, update paths
			defaultKustomizePathKfNbc := "overlays/openshift"
			if subcomponent.SourcePath != "" {
				defaultKustomizePathKfNbc = subcomponent.SourcePath
			}
			kfnotebookControllerPath = filepath.Join(deploy.DefaultManifestPath, "odh-notebook-controller/kf-notebook-controller", defaultKustomizePathKfNbc)
		}
		if strings.Contains(subcomponent.URI, DependentComponentName) {
			// Download subcomponent
			if err := deploy.DownloadManifests(ctx, DependentComponentName, subcomponent); err != nil {
				return err
			}
			// If overlay is defined, update paths
			defaultKustomizePath := "overlays/additional"
			if subcomponent.SourcePath != "" {
				defaultKustomizePath = subcomponent.SourcePath
			}
			notebookImagesPath = filepath.Join(deploy.DefaultManifestPath, DependentComponentName, defaultKustomizePath)
		}
	}
	return nil
}

func (w *Workbenches) GetComponentName() string {
	return ComponentName
}

func (w *Workbenches) ReconcileComponent(ctx context.Context, cli client.Client,
	owner metav1.Object, dscispec *dsciv1.DSCInitializationSpec, platform cluster.Platform, _ bool) error {
	l := logf.FromContext(ctx)
	// Set default notebooks namespace
	// Create rhods-notebooks namespace in managed platforms
	enabled := w.GetManagementState() == operatorv1.Managed
	monitoringEnabled := common.IsMonitoringEnabled(dscispec.Monitoring)
	if enabled {
		if w.DevFlags != nil {
			// Download manifests and update paths
			if err := w.OverrideManifests(ctx, platform); err != nil {
				return err
			}
		}
		if platform == cluster.SelfManagedRhoai || platform == cluster.ManagedRhoai {
			// Intentionally leaving the ownership unset for this namespace.
			// Specifying this label triggers its deletion when the operator is uninstalled.
			_, err := cluster.CreateNamespace(ctx, cli, cluster.DefaultNotebooksNamespace, cluster.WithLabels(labels.ODH.OwnedNamespace, "true"))
			if err != nil {
				return err
			}
		}
		// Update Default rolebinding
		err := cluster.UpdatePodSecurityRolebinding(ctx, cli, dscispec.ApplicationsNamespace, "notebook-controller-service-account")
		if err != nil {
			return err
		}
	}

	if err := deploy.DeployManifestsFromPath(ctx, cli, owner,
		notebookControllerPath,
		dscispec.ApplicationsNamespace,
		ComponentName, enabled); err != nil {
		return fmt.Errorf("failed to apply manifests %s: %w", notebookControllerPath, err)
	}
	l.WithValues("Path", notebookControllerPath).Info("apply manifests done notebook controller done")

	if err := deploy.DeployManifestsFromPath(ctx, cli, owner,
		kfnotebookControllerPath,
		dscispec.ApplicationsNamespace,
		ComponentName, enabled); err != nil {
		return fmt.Errorf("failed to apply manifests %s: %w", kfnotebookControllerPath, err)
	}
	l.WithValues("Path", kfnotebookControllerPath).Info("apply manifests done kf-notebook controller done")

	if err := deploy.DeployManifestsFromPath(ctx, cli, owner,
		notebookImagesPath,
		dscispec.ApplicationsNamespace,
		ComponentName, enabled); err != nil {
		return err
	}
	l.WithValues("Path", notebookImagesPath).Info("apply manifests done notebook image done")

	// Wait for deployment available
	if enabled {
		if err := cluster.WaitForDeploymentAvailable(ctx, cli, ComponentName, dscispec.ApplicationsNamespace, 10, 2); err != nil {
			return fmt.Errorf("deployments for %s are not ready to server: %w", ComponentName, err)
		}
	}

	// CloudService Monitoring handling
	if platform == cluster.ManagedRhoai {
		if err := w.UpdatePrometheusConfig(cli, l, enabled && monitoringEnabled, ComponentName); err != nil {
			return err
		}
		if err := deploy.DeployManifestsFromPath(ctx, cli, owner,
			filepath.Join(deploy.DefaultManifestPath, "monitoring", "prometheus", "apps"),
			dscispec.Monitoring.Namespace,
			"prometheus", true); err != nil {
			return err
		}
		l.Info("updating SRE monitoring done")
	}
	return nil
}

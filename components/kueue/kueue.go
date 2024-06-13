// +groupName=datasciencecluster.opendatahub.io
package kueue

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/go-logr/logr"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

var (
	ComponentName = "kueue"
	Path          = deploy.DefaultManifestPath + "/" + ComponentName + "/rhoai" // same path for both odh and rhoai
)

var (
	RayClusterCRD  = "rayclusters.ray.io"
	RayJobCRD      = "rayjobs.ray.io"
	MPIJobsCRD     = "mpijobs.kubeflow.org"
	MXJobsCRD      = "mxjobs.kubeflow.org"
	PaddleJobsCRD  = "paddlejobs.kubeflow.org"
	PyTorchJobsCRD = "pytorchjobs.kubeflow.org"
	TFJobsCRD      = "tfjobs.kubeflow.org"
	XGBoostJobsCRD = "xgboostjobs.kubeflow.org"
)

// Verifies that Kueue implements ComponentInterface.
var _ components.ComponentInterface = (*Kueue)(nil)

// Kueue struct holds the configuration for the Kueue component.
// +kubebuilder:object:generate=true
type Kueue struct {
	components.Component `json:""`
}

func (k *Kueue) OverrideManifests(_ string) error {
	// If devflags are set, update default manifests path
	if len(k.DevFlags.Manifests) != 0 {
		manifestConfig := k.DevFlags.Manifests[0]
		if err := deploy.DownloadManifests(ComponentName, manifestConfig); err != nil {
			return err
		}
		// If overlay is defined, update paths
		defaultKustomizePath := "rhoai"
		if manifestConfig.SourcePath != "" {
			defaultKustomizePath = manifestConfig.SourcePath
		}
		Path = filepath.Join(deploy.DefaultManifestPath, ComponentName, defaultKustomizePath)
	}

	return nil
}

func (k *Kueue) GetComponentName() string {
	return ComponentName
}

func (k *Kueue) ReconcileComponent(ctx context.Context, cli client.Client, logger logr.Logger,
	owner metav1.Object, dscispec *dsciv1.DSCInitializationSpec, _ bool) error {
	l := k.ConfigComponentLogger(logger, ComponentName, dscispec)
	var imageParamMap = map[string]string{
		"odh-kueue-controller-image": "RELATED_IMAGE_ODH_KUEUE_CONTROLLER_IMAGE", // new kueue image
	}

	enabled := k.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed
	platform, err := cluster.GetPlatform(cli)
	if err != nil {
		return err
	}

	if enabled {
		if k.DevFlags != nil {
			// Download manifests and update paths
			if err = k.OverrideManifests(string(platform)); err != nil {
				return err
			}
		}
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (k.DevFlags == nil || len(k.DevFlags.Manifests) == 0) {
			if err := deploy.ApplyParams(Path, imageParamMap, true); err != nil {
				return fmt.Errorf("failed to update image from %s : %w", Path, err)
			}
		}
	}
	// Deploy Kueue Operator
	if err := deploy.DeployManifestsFromPath(cli, owner, Path, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
		return fmt.Errorf("failed to apply manifests %s: %w", Path, err)
	}
	l.Info("apply manifests done")
	// CloudService Monitoring handling
	if platform == cluster.ManagedRhods {
		if enabled {
			// first check if the service is up, so prometheus won't fire alerts when it is just startup
			if err := cluster.WaitForDeploymentAvailable(ctx, cli, ComponentName, dscispec.ApplicationsNamespace, 20, 2); err != nil {
				return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentName, err)
			}
			l.Info("deployment is done, updating monitoring rules")
		}
		if err := k.UpdatePrometheusConfig(cli, enabled && monitoringEnabled, ComponentName); err != nil {
			return err
		}
		if err = deploy.DeployManifestsFromPath(cli, owner,
			filepath.Join(deploy.DefaultManifestPath, "monitoring", "prometheus", "apps"),
			dscispec.Monitoring.Namespace,
			"prometheus", true); err != nil {
			return err
		}
		l.Info("updating SRE monitoring done")
	}

	return nil
}

func CRDsExist(ctx context.Context, cli client.Client, crdNames []string) (bool, error) {
	for _, crdName := range crdNames {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := cli.Get(ctx, client.ObjectKey{Name: crdName}, crd); err != nil {
			if apierrs.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("failed to get existing CRDs %s : %v", crdName, err)
		}
	}
	return true, nil
}

// DeleteKueuePod deletes the Kueue pod based on specific labels within the provided namespace.
func (k *Kueue) DeleteKueuePod(ctx context.Context, cli client.Client, logger logr.Logger, dscispec *dsciv1.DSCInitializationSpec) error {
	l := k.ConfigComponentLogger(logger, ComponentName, dscispec)
	l.Info("Restarting Kueue pod")
	// Define the label selector for the Kueue pods
	listOpts := []client.ListOption{
		client.InNamespace(dscispec.ApplicationsNamespace),
		client.MatchingLabels{
			labels.ODH.Component(ComponentName): "true",
			"app.kubernetes.io/name":            "kueue",
		},
	}
	// List all pods that match the labels in the specified namespace
	podList := &corev1.PodList{}
	if err := cli.List(ctx, podList, listOpts...); err != nil {
		return fmt.Errorf("failed to list Kueue pod for deletion: %v", err)
	}
	// Delete each pod found
	for _, pod := range podList.Items {
		if err := cli.Delete(ctx, &pod); err != nil {
			return fmt.Errorf("failed to delete Kueue pod %s: %v", pod.Name, err)
		}
	}
	l.Info("Kueue pod restarted successfully.")
	return nil
}

func RayCRDsName() []string {
	return []string{RayClusterCRD, RayJobCRD}
}

func TrainingOperatorCRDsName() []string {
	return []string{MPIJobsCRD, MXJobsCRD, PaddleJobsCRD, PyTorchJobsCRD, TFJobsCRD, XGBoostJobsCRD}
}

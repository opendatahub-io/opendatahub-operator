// Package trustyai provides utility functions to config TrustyAI, a bias/fairness and explainability toolkit
package trustyai

import (
	"context"
	"fmt"
	"path/filepath"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/monitoring"
)

var (
	ComponentName = "trustyai"
	Path          = deploy.DefaultManifestPath + "/" + "trustyai-service-operator/base"
)

// Verifies that TrustyAI implements ComponentInterface.
var _ components.ComponentInterface = (*TrustyAI)(nil)

// TrustyAI struct holds the configuration for the TrustyAI component.
// +kubebuilder:object:generate=true
type TrustyAI struct {
	components.Component `json:""`
}

func (t *TrustyAI) OverrideManifests(_ string) error {
	// If devflags are set, update default manifests path
	if len(t.DevFlags.Manifests) != 0 {
		manifestConfig := t.DevFlags.Manifests[0]
		if err := deploy.DownloadManifests(ComponentName, manifestConfig); err != nil {
			return err
		}
		// If overlay is defined, update paths
		defaultKustomizePath := "base"
		if manifestConfig.SourcePath != "" {
			defaultKustomizePath = manifestConfig.SourcePath
		}
		Path = filepath.Join(deploy.DefaultManifestPath, ComponentName, defaultKustomizePath)
	}
	return nil
}

func (t *TrustyAI) GetComponentName() string {
	return ComponentName
}

func (t *TrustyAI) ReconcileComponent(ctx context.Context, cli client.Client, resConf *rest.Config, owner metav1.Object, dscispec *dsciv1.DSCInitializationSpec, _ bool) error {
	var imageParamMap = map[string]string{
		"trustyaiServiceImage":  "RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_IMAGE",
		"trustyaiOperatorImage": "RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_OPERATOR_IMAGE",
	}
	enabled := t.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed

	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}

	if enabled {
		if t.DevFlags != nil {
			// Download manifests and update paths
			if err = t.OverrideManifests(string(platform)); err != nil {
				return err
			}
		}
		if dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "" {
			if err := deploy.ApplyParams(Path, t.SetImageParamsMap(imageParamMap), false); err != nil {
				return err
			}
		}
	}
	// Deploy TrustyAI Operator
	if err := deploy.DeployManifestsFromPath(cli, owner, Path, dscispec.ApplicationsNamespace, t.GetComponentName(), enabled); err != nil {
		return err
	}

	// CloudService Monitoring handling
	if platform == deploy.ManagedRhods {
		if enabled {
			if err := monitoring.WaitForDeploymentAvailable(ctx, resConf, ComponentName, dscispec.ApplicationsNamespace, 10, 1); err != nil {
				return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentName, err)
			}
			fmt.Printf("deployment for %s is done, updating monitoring rules\n", ComponentName)
		}
		if err := t.UpdatePrometheusConfig(cli, enabled && monitoringEnabled, ComponentName); err != nil {
			return err
		}
		if err = deploy.DeployManifestsFromPath(cli, owner,
			filepath.Join(deploy.DefaultManifestPath, "monitoring", "prometheus", "apps"),
			dscispec.Monitoring.Namespace,
			"prometheus", true); err != nil {
			return err
		}
	}
	return nil
}

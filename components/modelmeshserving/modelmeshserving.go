// Package modelmeshserving provides utility functions to config MoModelMesh, a general-purpose model serving management/routing layer
package modelmeshserving

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/monitoring"
)

var (
	ComponentName          = "model-mesh"
	Path                   = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/odh"
	DependentComponentName = "odh-model-controller"
	DependentPath          = deploy.DefaultManifestPath + "/" + DependentComponentName + "/base"
)

// Verifies that Dashboard implements ComponentInterface.
var _ components.ComponentInterface = (*ModelMeshServing)(nil)

// ModelMeshServing struct holds the configuration for the ModelMeshServing component.
// +kubebuilder:object:generate=true
type ModelMeshServing struct {
	components.Component `json:""`
}

func (m *ModelMeshServing) OverrideManifests(_ string) error {
	// Go through each manifests and set the overlays if defined
	for _, subcomponent := range m.DevFlags.Manifests {
		if strings.Contains(subcomponent.URI, DependentComponentName) {
			// Download subcomponent
			if err := deploy.DownloadManifests(DependentComponentName, subcomponent); err != nil {
				return err
			}
			// If overlay is defined, update paths
			defaultKustomizePath := "base"
			if subcomponent.SourcePath != "" {
				defaultKustomizePath = subcomponent.SourcePath
			}
			DependentPath = filepath.Join(deploy.DefaultManifestPath, DependentComponentName, defaultKustomizePath)
		}

		if strings.Contains(subcomponent.URI, ComponentName) {
			// Download subcomponent
			if err := deploy.DownloadManifests(ComponentName, subcomponent); err != nil {
				return err
			}
			// If overlay is defined, update paths
			defaultKustomizePath := "overlays/odh"
			if subcomponent.SourcePath != "" {
				defaultKustomizePath = subcomponent.SourcePath
			}
			Path = filepath.Join(deploy.DefaultManifestPath, ComponentName, defaultKustomizePath)
		}
	}
	return nil
}

func (m *ModelMeshServing) GetComponentName() string {
	return ComponentName
}

func (m *ModelMeshServing) ReconcileComponent(ctx context.Context,
	cli client.Client,
	resConf *rest.Config,
	owner metav1.Object,
	dscispec *dsciv1.DSCInitializationSpec,
	_ bool,
) error {
	var imageParamMap = map[string]string{
		"odh-mm-rest-proxy":             "RELATED_IMAGE_ODH_MM_REST_PROXY_IMAGE",
		"odh-modelmesh-runtime-adapter": "RELATED_IMAGE_ODH_MODELMESH_RUNTIME_ADAPTER_IMAGE",
		"odh-modelmesh":                 "RELATED_IMAGE_ODH_MODELMESH_IMAGE",
		"odh-modelmesh-controller":      "RELATED_IMAGE_ODH_MODELMESH_CONTROLLER_IMAGE",
		"odh-model-controller":          "RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE",
	}

	// odh-model-controller to use
	var dependentImageParamMap = map[string]string{
		"odh-model-controller": "RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE",
	}

	enabled := m.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}

	// Update Default rolebinding
	if enabled {
		if m.DevFlags != nil {
			// Download manifests and update paths
			if err = m.OverrideManifests(string(platform)); err != nil {
				return err
			}
		}

		if err := cluster.UpdatePodSecurityRolebinding(cli, dscispec.ApplicationsNamespace,
			"modelmesh",
			"modelmesh-controller",
			"odh-prometheus-operator",
			"prometheus-custom"); err != nil {
			return err
		}
		// Update image parameters
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (m.DevFlags == nil || len(m.DevFlags.Manifests) == 0) {
			if err := deploy.ApplyParams(Path, m.SetImageParamsMap(imageParamMap), false); err != nil {
				return err
			}
		}
	}

	err = deploy.DeployManifestsFromPath(cli, owner, Path, dscispec.ApplicationsNamespace, ComponentName, enabled)
	if err != nil {
		return err
	}

	// For odh-model-controller
	if enabled {
		if err := cluster.UpdatePodSecurityRolebinding(cli, dscispec.ApplicationsNamespace,
			"odh-model-controller"); err != nil {
			return err
		}
		// Update image parameters for odh-model-controller
		if dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "" {
			if err := deploy.ApplyParams(DependentPath, m.SetImageParamsMap(dependentImageParamMap), false); err != nil {
				return err
			}
		}
	}
	if err := deploy.DeployManifestsFromPath(cli, owner, DependentPath, dscispec.ApplicationsNamespace, m.GetComponentName(), enabled); err != nil {
		// explicitly ignore error if error contains keywords "spec.selector" and "field is immutable" and return all other error.
		if !strings.Contains(err.Error(), "spec.selector") || !strings.Contains(err.Error(), "field is immutable") {
			return err
		}
	}

	// CloudService Monitoring handling
	if platform == deploy.ManagedRhods {
		if enabled {
			// first check if service is up, so prometheus wont fire alerts when it is just startup
			if err := monitoring.WaitForDeploymentAvailable(ctx, resConf, ComponentName, dscispec.ApplicationsNamespace, 20, 2); err != nil {
				return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentName, err)
			}
			fmt.Printf("deployment for %s is done, updating monitoring rules\n", ComponentName)
		}
		// first model-mesh rules
		if err := m.UpdatePrometheusConfig(cli, enabled && monitoringEnabled, ComponentName); err != nil {
			return err
		}
		// then odh-model-controller rules
		if err := m.UpdatePrometheusConfig(cli, enabled && monitoringEnabled, DependentComponentName); err != nil {
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

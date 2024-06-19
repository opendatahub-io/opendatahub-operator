// Package kserve provides utility functions to config Kserve as the Controller for serving ML models on arbitrary frameworks
// +groupName=datasciencecluster.opendatahub.io
package kserve

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

var (
	ComponentName          = "kserve"
	Path                   = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/odh"
	DependentComponentName = "odh-model-controller"
	DependentPath          = deploy.DefaultManifestPath + "/" + DependentComponentName + "/base"
	ServiceMeshOperator    = "servicemeshoperator"
	ServerlessOperator     = "serverless-operator"
)

// Verifies that Kserve implements ComponentInterface.
var _ components.ComponentInterface = (*Kserve)(nil)

// +kubebuilder:validation:Pattern=`^(Serverless|RawDeployment)$`
type DefaultDeploymentMode string

var (
	// Serverless will be used as the default deployment mode for Kserve. This requires Serverless and ServiceMesh operators configured as dependencies.
	Serverless DefaultDeploymentMode = "Serverless"
	// RawDeployment will be used as the default deployment mode for Kserve.
	RawDeployment DefaultDeploymentMode = "RawDeployment"
)

// Kserve struct holds the configuration for the Kserve component.
// +kubebuilder:object:generate=true
type Kserve struct {
	components.Component `json:""`
	// Serving configures the KNative-Serving stack used for model serving. A Service
	// Mesh (Istio) is prerequisite, since it is used as networking layer.
	Serving infrav1.ServingSpec `json:"serving,omitempty"`
	// Configures the default deployment mode for Kserve. This can be set to 'Serverless' or 'RawDeployment'.
	// The value specified in this field will be used to set the default deployment mode in the 'inferenceservice-config' configmap for Kserve
	// If no default deployment mode is specified, Kserve will use Serverless mode
	// +kubebuilder:validation:Enum=Serverless;RawDeployment
	DefaultDeploymentMode DefaultDeploymentMode `json:"defaultDeploymentMode,omitempty"`
}

func (k *Kserve) OverrideManifests(_ string) error {
	// Download manifests if defined by devflags
	// Go through each manifest and set the overlays if defined
	for _, subcomponent := range k.DevFlags.Manifests {
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

func (k *Kserve) GetComponentName() string {
	return ComponentName
}

func (k *Kserve) ReconcileComponent(ctx context.Context, cli client.Client,
	logger logr.Logger, owner metav1.Object, dscispec *dsciv1.DSCInitializationSpec, platform cluster.Platform, _ bool) error {
	l := k.ConfigComponentLogger(logger, ComponentName, dscispec)
	// paramMap for Kserve to use.
	var imageParamMap = map[string]string{}

	// dependentParamMap for odh-model-controller to use.
	var dependentParamMap = map[string]string{
		"odh-model-controller": "RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE",
	}

	enabled := k.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed

	if !enabled {
		if err := k.removeServerlessFeatures(dscispec); err != nil {
			return err
		}
	} else {
		// Configure dependencies
		if err := k.configureServerless(cli, dscispec); err != nil {
			return err
		}
		if k.DevFlags != nil {
			// Download manifests and update paths
			if err := k.OverrideManifests(string(platform)); err != nil {
				return err
			}
		}

		// Update image parameters only when we do not have customized manifests set
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (k.DevFlags == nil || len(k.DevFlags.Manifests) == 0) {
			if err := deploy.ApplyParams(Path, imageParamMap, false); err != nil {
				return fmt.Errorf("failed to update image from %s : %w", Path, err)
			}
		}
	}

	if err := k.configureServiceMesh(cli, dscispec); err != nil {
		return fmt.Errorf("failed configuring service mesh while reconciling kserve component. cause: %w", err)
	}

	if err := deploy.DeployManifestsFromPath(cli, owner, Path, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
		return fmt.Errorf("failed to apply manifests from %s : %w", Path, err)
	}

	l.WithValues("Path", Path).Info("apply manifests done for kserve")

	if enabled {
		if err := k.setupKserveConfig(ctx, cli, dscispec); err != nil {
			return err
		}

		// For odh-model-controller
		if err := cluster.UpdatePodSecurityRolebinding(ctx, cli, dscispec.ApplicationsNamespace, "odh-model-controller"); err != nil {
			return err
		}
		// Update image parameters for odh-model-controller
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (k.DevFlags == nil || len(k.DevFlags.Manifests) == 0) {
			if err := deploy.ApplyParams(DependentPath, dependentParamMap, false); err != nil {
				return fmt.Errorf("failed to update image %s: %w", DependentPath, err)
			}
		}
	}

	if err := deploy.DeployManifestsFromPath(cli, owner, DependentPath, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
		if !strings.Contains(err.Error(), "spec.selector") || !strings.Contains(err.Error(), "field is immutable") {
			// explicitly ignore error if error contains keywords "spec.selector" and "field is immutable" and return all other error.
			return err
		}
	}
	l.WithValues("Path", Path).Info("apply manifests done for odh-model-controller")
	// CloudService Monitoring handling
	if platform == cluster.ManagedRhods {
		if enabled {
			// first check if the service is up, so prometheus won't fire alerts when it is just startup
			if err := cluster.WaitForDeploymentAvailable(ctx, cli, ComponentName, dscispec.ApplicationsNamespace, 20, 2); err != nil {
				return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentName, err)
			}
			l.Info("deployment is done, updating monitoing rules")
		}
		// kesrve rules
		if err := k.UpdatePrometheusConfig(cli, enabled && monitoringEnabled, ComponentName); err != nil {
			return err
		}
		l.Info("updating SRE monitoring done")
	}

	return nil
}

func (k *Kserve) Cleanup(cli client.Client, instance *dsciv1.DSCInitializationSpec) error {
	if removeServerlessErr := k.removeServerlessFeatures(instance); removeServerlessErr != nil {
		return removeServerlessErr
	}

	return k.removeServiceMeshConfigurations(cli, instance)
}

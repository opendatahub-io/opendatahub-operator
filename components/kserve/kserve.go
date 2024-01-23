// Package kserve provides utility functions to config Kserve as the Controller for serving ML models on arbitrary frameworks
package kserve

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/monitoring"
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

// Kserve struct holds the configuration for the Kserve component.
// +kubebuilder:object:generate=true
type Kserve struct {
	components.Component `json:""`
	// Serving configures the KNative-Serving stack used for model serving. A Service
	// Mesh (Istio) is prerequisite, since it is used as networking layer.
	Serving infrav1.ServingSpec `json:"serving,omitempty"`
}

func (k *Kserve) OverrideManifests(_ string) error {
	// Download manifests if defined by devflags
	// Go through each manifests and set the overlays if defined
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

func (k *Kserve) ReconcileComponent(ctx context.Context, cli client.Client, resConf *rest.Config, owner metav1.Object, dscispec *dsciv1.DSCInitializationSpec, _ bool) error {
	// paramMap for Kserve to use.
	var imageParamMap = map[string]string{}

	// dependentParamMap for odh-model-controller to use.
	var dependentParamMap = map[string]string{
		"odh-model-controller": "RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE",
	}

	enabled := k.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}

	if !enabled {
		if err := k.removeServerlessFeatures(dscispec); err != nil {
			return err
		}
	} else {
		if k.DevFlags != nil {
			// Download manifests and update paths
			if err = k.OverrideManifests(string(platform)); err != nil {
				return err
			}
		}
		// check on dependent operators if all installed in cluster
		// dependent operators set in checkRequiredOperatorsInstalled()
		if err := checkRequiredOperatorsInstalled(cli); err != nil {
			return err
		}

		if err := k.configureServerless(dscispec); err != nil {
			return err
		}

		// Update image parameters only when we do not have customized manifests set
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (k.DevFlags == nil || len(k.DevFlags.Manifests) == 0) {
			if err := deploy.ApplyParams(Path, k.SetImageParamsMap(imageParamMap), false); err != nil {
				return err
			}
		}
	}

	if err := deploy.DeployManifestsFromPath(cli, owner, Path, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
		return err
	}

	// For odh-model-controller
	if enabled {
		if err := cluster.UpdatePodSecurityRolebinding(cli, dscispec.ApplicationsNamespace, "odh-model-controller"); err != nil {
			return err
		}
		// Update image parameters for odh-model-controller
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (k.DevFlags == nil || len(k.DevFlags.Manifests) == 0) {
			if err := deploy.ApplyParams(DependentPath, k.SetImageParamsMap(dependentParamMap), false); err != nil {
				return err
			}
		}
	}

	if err := deploy.DeployManifestsFromPath(cli, owner, DependentPath, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
		if !strings.Contains(err.Error(), "spec.selector") || !strings.Contains(err.Error(), "field is immutable") {
			// explicitly ignore error if error contains keywords "spec.selector" and "field is immutable" and return all other error.
			return err
		}
	}
	// CloudService Monitoring handling
	if platform == deploy.ManagedRhods {
		if enabled {
			// first check if the service is up, so prometheus wont fire alerts when it is just startup
			if err := monitoring.WaitForDeploymentAvailable(ctx, resConf, ComponentName, dscispec.ApplicationsNamespace, 20, 2); err != nil {
				return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentName, err)
			}
			fmt.Printf("deployment for %s is done, updating monitoing rules", ComponentName)
		}
		// kesrve rules
		if err := k.UpdatePrometheusConfig(cli, enabled && monitoringEnabled, ComponentName); err != nil {
			return err
		}
	}
	return nil
}

func (k *Kserve) Cleanup(_ client.Client, instance *dsciv1.DSCInitializationSpec) error {
	return k.removeServerlessFeatures(instance)
}

func (k *Kserve) configureServerless(instance *dsciv1.DSCInitializationSpec) error {
	switch k.Serving.ManagementState {
	case operatorv1.Unmanaged: // Bring your own CR
		fmt.Println("Serverless CR is not configured by the operator, we won't do anything")

	case operatorv1.Removed: // we remove serving CR
		fmt.Println("existing ServiceMesh CR (owned by operator) will be removed")
		if err := k.removeServerlessFeatures(instance); err != nil {
			return err
		}

	case operatorv1.Managed: // standard workflow to create CR
		switch instance.ServiceMesh.ManagementState {
		case operatorv1.Unmanaged, operatorv1.Removed:
			return fmt.Errorf("ServiceMesh is need to set to 'Managaed' in DSCI CR, it is required by KServe serving field")
		}
		serverlessInitializer := feature.NewFeaturesInitializer(instance, k.configureServerlessFeatures)

		if err := serverlessInitializer.Prepare(); err != nil {
			return err
		}

		if err := serverlessInitializer.Apply(); err != nil {
			return err
		}
	}
	return nil
}

func (k *Kserve) removeServerlessFeatures(instance *dsciv1.DSCInitializationSpec) error {
	serverlessInitializer := feature.NewFeaturesInitializer(instance, k.configureServerlessFeatures)

	if err := serverlessInitializer.Prepare(); err != nil {
		return err
	}

	return serverlessInitializer.Delete()
}

func checkRequiredOperatorsInstalled(cli client.Client) error {
	var multiErr *multierror.Error

	checkAndAppendError := func(operatorName string) {
		if found, err := deploy.OperatorExists(cli, operatorName); err != nil {
			multiErr = multierror.Append(multiErr, err)
		} else if !found {
			err = fmt.Errorf("operator %s not found. Please install the operator before enabling %s component",
				operatorName, ComponentName)
			multiErr = multierror.Append(multiErr, err)
		}
	}

	checkAndAppendError(ServiceMeshOperator)
	checkAndAppendError(ServerlessOperator)

	return multiErr.ErrorOrNil()
}

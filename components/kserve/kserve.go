// Package kserve provides utility functions to config Kserve as the Controller for serving ML models on arbitrary frameworks
package kserve

import (
	"fmt"
	"path/filepath"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

var (
	ComponentName          = "kserve"
	Path                   = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/odh"
	DependentComponentName = "odh-model-controller"
	DependentPath          = deploy.DefaultManifestPath + "/" + DependentComponentName + "/base"
	ServiceMeshOperator    = "servicemeshoperator"
	ServerlessOperator     = "serverless-operator"
)

type Kserve struct {
	components.Component `json:""`
	// Serving configures the KNative-Serving stack used for model serving. A Service
	// Mesh (Istio) is prerequisite, since it is used as networking layer.
	Serving infrav1.ServingSpec `json:"serving,omitempty"`
}

func (k *Kserve) OverrideManifests(_ string) error {
	// Download manifests if defined by devflags
	if len(k.DevFlags.Manifests) != 0 {
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
	}
	return nil
}

func (k *Kserve) GetComponentName() string {
	return ComponentName
}

// Verifies that Kserve implements ComponentInterface.
var _ components.ComponentInterface = (*Kserve)(nil)

func (k *Kserve) ReconcileComponent(cli client.Client, owner metav1.Object, dscispec *dsciv1.DSCInitializationSpec, _ bool) error {
	// paramMap for Kserve to use.
	var imageParamMap = map[string]string{}

	// dependentParamMap for odh-model-controller to use.
	var dependentParamMap = map[string]string{
		"odh-model-controller": "RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE",
	}

	enabled := k.GetManagementState() == operatorv1.Managed
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}

	if !enabled {
		if err := k.removeServerlessFeatures(dscispec); err != nil {
			return err
		}
	}

	if enabled {
		// Download manifests and update paths
		if err = k.OverrideManifests(string(platform)); err != nil {
			return err
		}

		// check on dependent operators
		if found, err := deploy.OperatorExists(cli, ServiceMeshOperator); err != nil {
			return err
		} else if !found {
			return fmt.Errorf("operator %s not found. Please install the operator before enabling %s component",
				ServiceMeshOperator, ComponentName)
		}

		// check on dependent operators might be in multiple namespaces
		if found, err := deploy.OperatorExists(cli, ServerlessOperator); err != nil {
			return err
		} else if !found {
			return fmt.Errorf("operator %s not found. Please install the operator before enabling %s component",
				ServerlessOperator, ComponentName)
		}

		if err := k.configureServerless(dscispec); err != nil {
			return err
		}

		// Update image parameters only when we do not have customized manifests set
		if dscispec.DevFlags.ManifestsUri == "" && len(k.DevFlags.Manifests) == 0 {
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
		if dscispec.DevFlags.ManifestsUri == "" && len(k.DevFlags.Manifests) == 0 {
			if err := deploy.ApplyParams(DependentPath, k.SetImageParamsMap(dependentParamMap), false); err != nil {
				return err
			}
		}
	}

	if err := deploy.DeployManifestsFromPath(cli, owner, DependentPath, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
		if strings.Contains(err.Error(), "spec.selector") && strings.Contains(err.Error(), "field is immutable") {
			// ignore this error
		} else {
			return err
		}
	}

	return nil
}

func (k *Kserve) DeepCopyInto(target *Kserve) {
	*target = *k
	target.Component = k.Component
}

func (k *Kserve) Cleanup(_ client.Client, instance *dsciv1.DSCInitializationSpec) error {
	return k.removeServerlessFeatures(instance)
}

func (k *Kserve) configureServerless(instance *dsciv1.DSCInitializationSpec) error {
	if k.Serving.ManagementState == operatorv1.Managed {
		if instance.ServiceMesh.ManagementState != operatorv1.Managed {
			return fmt.Errorf("service mesh is not configure in DataScienceInitialization cluster but required by KServe serving")
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

	if err := serverlessInitializer.Delete(); err != nil {
		return err
	}
	return nil
}

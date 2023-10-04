// Package trustyai provides utility functions to config TrustyAI, a bias/fairness and explainability toolkit
package trustyai

import (
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"path/filepath"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ComponentName = "trustyai"
	Path          = deploy.DefaultManifestPath + "/" + "trustyai-service-operator/base"
)

var imageParamMap = map[string]string{
	"trustyaiServiceImage":  "RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_IMAGE",
	"trustyaiOperatorImage": "RELATED_IMAGE_ODH_TRUSTYAI_OPERATOR_IMAGE",
}

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

func (t *TrustyAI) SetImageParamsMap(imageMap map[string]string) map[string]string {
	imageParamMap = imageMap
	return imageParamMap
}

// Verifies that TrustyAI implements ComponentInterface
var _ components.ComponentInterface = (*TrustyAI)(nil)

func (t *TrustyAI) ReconcileComponent(cli client.Client, owner metav1.Object, dscispec *dsci.DSCInitializationSpec) error {
	enabled := t.GetManagementState() == operatorv1.Managed

	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}

	if enabled {
		// Download manifests and update paths
		if err = t.OverrideManifests(string(platform)); err != nil {
			return err
		}

		if dscispec.DevFlags.ManifestsUri == "" {
			if err := deploy.ApplyImageParams(Path, imageParamMap); err != nil {
				return err
			}
		}
	}
	// Deploy TrustyAI Operator
	err = deploy.DeployManifestsFromPath(cli, owner, Path, dscispec.ApplicationsNamespace, t.GetComponentName(), enabled)
	return err
}

func (t *TrustyAI) DeepCopyInto(target *TrustyAI) {
	*target = *t
	target.Component = t.Component
}

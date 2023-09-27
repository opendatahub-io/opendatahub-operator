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
	Path          = deploy.DefaultManifestPath + "/trustyai-service-operator/base"
)

var imageParamMap = map[string]string{
	"trustyaiServiceImageName":  "RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_IMAGE_NAME",
	"trustyaiServiceImageTag":   "RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_IMAGE_TAG",
	"trustyaiOperatorImageName": "RELATED_IMAGE_ODH_TRUSTYAI_OPERATOR_IMAGE_NAME",
	"trustyaiOperatorImageTag":  "RELATED_IMAGE_ODH_TRUSTYAI_OPERATOR_IMAGE_TAG",
}

type TrustyAI struct {
	components.Component `json:""`
}

func (m *TrustyAI) GetComponentDevFlags() components.DevFlags {
	return m.DevFlags
}

func (m *TrustyAI) OverrideManifests(_ string) error {
	// If devflags are set, update default manifests path
	if len(m.DevFlags.Manifests) != 0 {
		manifestConfig := m.DevFlags.Manifests[0]
		if err := deploy.DownloadManifests(ComponentName, manifestConfig); err != nil {
			return err
		}
		// If overlay is defined, update paths
		defaultKustomizePath := "operator/base"
		if manifestConfig.SourcePath != "" {
			defaultKustomizePath = manifestConfig.SourcePath
		}
		Path = filepath.Join(deploy.DefaultManifestPath, ComponentName, defaultKustomizePath)
	}
	return nil
}

func (m *TrustyAI) GetComponentName() string {
	return ComponentName
}

func (m *TrustyAI) SetImageParamsMap(imageMap map[string]string) map[string]string {
	imageParamMap = imageMap
	return imageParamMap
}

// Verifies that TrustyAI implements ComponentInterface
var _ components.ComponentInterface = (*TrustyAI)(nil)

func (m *TrustyAI) ReconcileComponent(cli client.Client, owner metav1.Object, dscispec *dsci.DSCInitializationSpec) error {
	enabled := m.GetManagementState() == operatorv1.Managed

	if enabled {
		if dscispec.DevFlags.ManifestsUri == "" {
			if err := deploy.ApplyImageParams(Path, imageParamMap); err != nil {
				return err
			}
		}
	}
	// Deploy TrustyAI Operator
	err := deploy.DeployManifestsFromPath(cli, owner, Path, dscispec.ApplicationsNamespace, m.GetComponentName(), enabled)
	return err
}

func (in *TrustyAI) DeepCopyInto(out *TrustyAI) {
	*out = *in
	out.Component = in.Component
}

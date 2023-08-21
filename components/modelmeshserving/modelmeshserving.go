package modelmeshserving

import (
	"context"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName  = "model-mesh"
	Path           = deploy.DefaultManifestPath + "/" + ComponentName + "/base"
	monitoringPath = deploy.DefaultManifestPath + "/" + "modelmesh-monitoring/base"
)

var imageParamMap = map[string]string{
	"odh-mm-rest-proxy":             "RELATED_IMAGE_ODH_MM_REST_PROXY_IMAGE",
	"odh-modelmesh-runtime-adapter": "RELATED_IMAGE_ODH_MODELMESH_RUNTIME_ADAPTER_IMAGE",
	"odh-modelmesh":                 "RELATED_IMAGE_ODH_MODELMESH_IMAGE",
	"odh-modelmesh-controller":      "RELATED_IMAGE_ODH_MODELMESH_CONTROLLER_IMAGE",
	"odh-model-controller":          "RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE",
}

type ModelMeshServing struct {
	components.Component `json:""`
}

func (m *ModelMeshServing) GetComponentName() string {
	return ComponentName
}

func (m *ModelMeshServing) SetImageParamsMap(imageMap map[string]string) map[string]string {
	imageParamMap = imageMap
	return imageParamMap
}

// Verifies that Dashboard implements ComponentInterface
var _ components.ComponentInterface = (*ModelMeshServing)(nil)

func (m *ModelMeshServing) IsEnabled() bool {
	return m.Enabled
}

func (m *ModelMeshServing) SetEnabled(enabled bool) {
	m.Enabled = enabled
}

func (m *ModelMeshServing) ReconcileComponent(owner metav1.Object, cli client.Client, scheme *runtime.Scheme, enabled bool, namespace string) error {

	// Update Default rolebinding
	if enabled {
		err := common.UpdatePodSecurityRolebinding(cli, []string{"modelmesh", "modelmesh-controller", "odh-model-controller", "odh-prometheus-operator", "prometheus-custom"}, namespace)
		if err != nil {
			return err
		}
	}
	// Update image parameters
	if err := deploy.ApplyImageParams(Path, imageParamMap); err != nil {
		return err
	}

	err := deploy.DeployManifestsFromPath(owner, cli, ComponentName,
		Path,
		namespace,
		scheme, enabled)

	if err != nil {
		return err
	}

	// Get monitoring namespace
	dscInit := &dsci.DSCInitialization{}
	err = cli.Get(context.TODO(), client.ObjectKey{
		Name: "default",
	}, dscInit)
	if err != nil {
		return err
	}
	var monitoringNamespace string
	if dscInit.Spec.Monitoring.Namespace != "" {
		monitoringNamespace = dscInit.Spec.Monitoring.Namespace
	} else {
		monitoringNamespace = namespace
	}

	// If modelmesh is deployed successfully, deploy modelmesh-monitoring
	err = deploy.DeployManifestsFromPath(owner, cli, ComponentName,
		monitoringPath,
		monitoringNamespace,
		scheme, enabled)

	return err
}

func (in *ModelMeshServing) DeepCopyInto(out *ModelMeshServing) {
	*out = *in
	out.Component = in.Component
}

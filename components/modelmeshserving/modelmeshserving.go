// Package modelmeshserving provides utility functions to config MoModelMesh, a general-purpose model serving management/routing layer
package modelmeshserving

import (
	"context"
	operatorv1 "github.com/openshift/api/operator/v1"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func (m *ModelMeshServing) ReconcileComponent(cli client.Client, owner metav1.Object, dscispec *dsci.DSCInitializationSpec) error {
	enabled := m.GetManagementState() == operatorv1.Managed

	// Update Default rolebinding
	if enabled {
		err := common.UpdatePodSecurityRolebinding(cli, []string{"modelmesh", "modelmesh-controller", "odh-model-controller", "odh-prometheus-operator", "prometheus-custom"}, dscispec.ApplicationsNamespace)
		if err != nil {
			return err
		}
		// Update image parameters
		if dscispec.DevFlags.ManifestsUri == "" {
			if err := deploy.ApplyImageParams(Path, imageParamMap); err != nil {
				return err
			}
		}
	}

	err := deploy.DeployManifestsFromPath(owner, cli, ComponentName,
		Path,
		dscispec.ApplicationsNamespace,
		cli.Scheme(), enabled)

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
		monitoringNamespace = dscispec.ApplicationsNamespace
	}

	// If modelmesh is deployed successfully, deploy modelmesh-monitoring
	err = deploy.DeployManifestsFromPath(owner, cli, ComponentName,
		monitoringPath,
		monitoringNamespace,
		cli.Scheme(), enabled)

	return err
}

func (m *ModelMeshServing) DeepCopyInto(target *ModelMeshServing) {
	*target = *m
	target.Component = m.Component
}

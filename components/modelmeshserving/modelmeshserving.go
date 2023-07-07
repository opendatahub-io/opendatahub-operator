package modelmeshserving

import (
	"github.com/opendatahub-io/opendatahub-operator/components"
	"github.com/opendatahub-io/opendatahub-operator/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/pkg/deploy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName = "model-mesh"
	Path          = "/opt/odh-manifests/model-mesh/base"
)

type ModelMeshServing struct {
	components.Component `json:""`
}

// Verifies that Dashboard implements ComponentInterface
var _ components.ComponentInterface = (*ModelMeshServing)(nil)

func (m *ModelMeshServing) ReconcileComponent(owner metav1.Object, cli client.Client, scheme *runtime.Scheme, enabled bool, namespace string) error {

	// Update Default rolebinding
	err := common.UpdatePodSecurityRolebinding(cli, []string{"modelmesh", "modelmesh-controller", "odh-model-controller"}, namespace)
	if err != nil {
		return err
	}
	err = deploy.DeployManifestsFromPath(owner, cli,
		Path,
		namespace,
		scheme, enabled)
	return err
}

func (in *ModelMeshServing) DeepCopyInto(out *ModelMeshServing) {
	*out = *in
	out.Component = in.Component
}

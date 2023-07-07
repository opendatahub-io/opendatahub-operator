package workbenches

import (
	"github.com/opendatahub-io/opendatahub-operator/components"
	"github.com/opendatahub-io/opendatahub-operator/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/pkg/deploy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName      = "workbenches"
	Path               = "/opt/odh-manifests/odh-notebook-controller/base"
	notebookImagesPath = "/opt/odh-manifests/notebook-images/overlays/additional"
)

type Workbenches struct {
	components.Component `json:""`
}

// Verifies that Dashboard implements ComponentInterface
var _ components.ComponentInterface = (*Workbenches)(nil)

func (m *Workbenches) ReconcileComponent(owner metav1.Object, cli client.Client, scheme *runtime.Scheme, enabled bool, namespace string) error {
	// Update Default rolebinding
	err := common.UpdatePodSecurityRolebinding(cli, []string{"notebook-controller-service-account"}, namespace)
	if err != nil {
		return err
	}

	err = deploy.DeployManifestsFromPath(owner, cli,
		Path,
		namespace,
		scheme, enabled)
	if err != nil {
		return err
	}
	err = deploy.DeployManifestsFromPath(owner, cli,
		notebookImagesPath,
		namespace,
		scheme, enabled)
	return err

}

func (in *Workbenches) DeepCopyInto(out *Workbenches) {
	*out = *in
	out.Component = in.Component
}

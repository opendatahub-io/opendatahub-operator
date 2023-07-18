package workbenches

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName          = "workbenches"
	notebookControllerPath = deploy.DefaultManifestPath + "/odh-notebook-controller/base"
	notebookImagesPath     = deploy.DefaultManifestPath + "/notebook-images/overlays/additional"
)

type Workbenches struct {
	components.Component `json:""`
}

// Verifies that Dashboard implements ComponentInterface
var _ components.ComponentInterface = (*Workbenches)(nil)

func (d *Workbenches) IsEnabled() bool {
	return d.Enabled
}

func (d *Workbenches) SetEnabled(enabled bool) {
	d.Enabled = enabled
}

func (m *Workbenches) ReconcileComponent(owner metav1.Object, cli client.Client, scheme *runtime.Scheme, enabled bool, namespace string) error {
	// Update Default rolebinding
	err := common.UpdatePodSecurityRolebinding(cli, []string{"notebook-controller-service-account"}, namespace)
	if err != nil {
		return err
	}

	err = deploy.DeployManifestsFromPath(owner, cli,
		notebookControllerPath,
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

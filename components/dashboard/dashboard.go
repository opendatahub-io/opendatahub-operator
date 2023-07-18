package dashboard

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName = "odh-dashboard"
	Path          = deploy.DefaultManifestPath + "/" + ComponentName + "/base"
)

type Dashboard struct {
	components.Component `json:""`
}

// Verifies that Dashboard implements ComponentInterface
var _ components.ComponentInterface = (*Dashboard)(nil)

func (d *Dashboard) ReconcileComponent(owner metav1.Object, cli client.Client, scheme *runtime.Scheme, enabled bool, namespace string) error {

	// TODO: Add any additional tasks if required when reconciling component
	// Update Default rolebinding
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}
	if platform == deploy.OpenDataHub {
		err := common.UpdatePodSecurityRolebinding(cli, []string{"odh-dashboard"}, namespace)
		if err != nil {
			return err
		}
	} else {
		err := common.UpdatePodSecurityRolebinding(cli, []string{"rhods-dashboard"}, namespace)
		if err != nil {
			return err
		}
	}

	err = deploy.DeployManifestsFromPath(owner, cli,
		Path,
		namespace,
		scheme, enabled)
	return err

}

func (in *Dashboard) DeepCopyInto(out *Dashboard) {
	*out = *in
	out.Component = in.Component
}

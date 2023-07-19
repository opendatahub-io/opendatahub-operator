package kserve

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = "kserve"
	Path          = deploy.DefaultManifestPath + "/" + ComponentName + "/base"
)

type Kserve struct {
	components.Component `json:""`
}

// Verifies that Kserve implements ComponentInterface
var _ components.ComponentInterface = (*Kserve)(nil)

func (d *Kserve) IsEnabled() bool {
	return d.Enabled
}

func (d *Kserve) SetEnabled(enabled bool) {
	d.Enabled = enabled
}

func (m *Kserve) ReconcileComponent(owner metav1.Object, cli client.Client, scheme *runtime.Scheme, enabled bool, namespace string) error {

	// Update Default rolebinding
	err := common.UpdatePodSecurityRolebinding(cli, []string{"kserve-controller-manager"}, namespace)
	if err != nil {
		return err
	}
	err = deploy.DeployManifestsFromPath(owner, cli,
		Path,
		namespace,
		scheme, enabled)
	return err
}

func (in *Kserve) DeepCopyInto(out *Kserve) {
	*out = *in
	out.Component = in.Component
}

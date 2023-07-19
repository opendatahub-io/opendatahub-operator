package datasciencepipelines

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName = "data-science-pipelines-operator"
	Path          = deploy.DefaultManifestPath + "/" + ComponentName + "/base"
)

type DataSciencePipelines struct {
	components.Component `json:""`
}

// Verifies that Dashboard implements ComponentInterface
var _ components.ComponentInterface = (*DataSciencePipelines)(nil)

func (d *DataSciencePipelines) IsEnabled() bool {
	return d.Enabled
}

func (d *DataSciencePipelines) SetEnabled(enabled bool) {
	d.Enabled = enabled
}

func (d *DataSciencePipelines) ReconcileComponent(owner metav1.Object, client client.Client, scheme *runtime.Scheme, enabled bool, namespace string) error {

	err := deploy.DeployManifestsFromPath(owner, client,
		Path,
		namespace,
		scheme, enabled)
	return err

}

func (in *DataSciencePipelines) DeepCopyInto(out *DataSciencePipelines) {
	*out = *in
	out.Component = in.Component
}

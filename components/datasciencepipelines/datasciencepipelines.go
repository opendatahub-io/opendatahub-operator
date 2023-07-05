package datasciencepipelines

import (
	"github.com/opendatahub-io/opendatahub-operator/components"
	"github.com/opendatahub-io/opendatahub-operator/pkg/deploy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName = "data-science-pipelines-operator"
	Path          = "/opt/odh-manifests/data-science-pipelines-operator/base"
)

type DataSciencePipelines struct {
	components.Component `json:""`
}

// Verifies that Dashboard implements ComponentInterface
var _ components.ComponentInterface = (*DataSciencePipelines)(nil)

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

package status

import (
	"context"
	"fmt"

	apihelpers "k8s.io/apiextensions-apiserver/pkg/apihelpers"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// GPUOperatorPrerequisite reports whether the NVIDIA GPU Operator ClusterPolicy
// CRD is installed. cli is the cached client; the CRD watches enqueue
// reconciliation after the informer has the object. NotFound is a confirmed
// absence. A CRD that does not serve v1, is not established, or is terminating
// is present but not usable. Any other read error is Unknown, and the
// condition message includes that error.
func GPUOperatorPrerequisite(ctx context.Context, cli client.Reader) (metav1.ConditionStatus, string, string) {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	err := cli.Get(ctx, client.ObjectKey{Name: gvk.GPUOperatorClusterPolicyCRDName}, crd)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return metav1.ConditionFalse, GPUOperatorPrerequisiteMissingReason, GPUOperatorPrerequisiteMissingMessage
		}

		logf.FromContext(ctx).Error(err, "failed to read GPU Operator ClusterPolicy CRD")
		return metav1.ConditionUnknown, GPUOperatorPrerequisiteUnknownReason, fmt.Sprintf("%s: %s", GPUOperatorPrerequisiteUnknownMessage, err)
	}

	if !gpuOperatorClusterPolicyServed(crd) {
		return metav1.ConditionFalse, GPUOperatorPrerequisiteNotUsableReason, GPUOperatorPrerequisiteNotUsableMessage
	}

	return metav1.ConditionTrue, GPUOperatorPrerequisiteMetReason, GPUOperatorPrerequisiteMetMessage
}

func gpuOperatorClusterPolicyServed(crd *apiextensionsv1.CustomResourceDefinition) bool {
	if apihelpers.IsCRDConditionTrue(crd, apiextensionsv1.Terminating) {
		return false
	}

	for _, version := range crd.Spec.Versions {
		if version.Name == gvk.GPUOperatorClusterPolicy.Version && version.Served {
			return apihelpers.IsCRDConditionTrue(crd, apiextensionsv1.Established)
		}
	}

	return false
}

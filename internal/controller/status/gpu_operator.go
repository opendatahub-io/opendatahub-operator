package status

import (
	"context"
	"fmt"
	"time"

	apihelpers "k8s.io/apiextensions-apiserver/pkg/apihelpers"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// GPUOperatorPrerequisiteRequeueAfter is the delay both controllers use when
// the ClusterPolicy CRD read fails. Confirmed absence stays on the CRD watch.
const GPUOperatorPrerequisiteRequeueAfter = 30 * time.Second

// GPUOperatorPrerequisite reports whether the NVIDIA GPU Operator ClusterPolicy
// CRD is installed. The read is unstructured so it uses the CRD informer the
// watches already populate. A typed Get on the raw manager client would start
// a second informer. NotFound is a confirmed absence. A CRD that does not
// serve v1, is not established, or is terminating is present but not usable,
// and the message names that check. Any other read error is Unknown, and the
// condition message includes that error.
func GPUOperatorPrerequisite(ctx context.Context, cli client.Reader) (metav1.ConditionStatus, string, string) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk.CustomResourceDefinition)
	err := cli.Get(ctx, client.ObjectKey{Name: gvk.GPUOperatorClusterPolicyCRDName}, obj)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return metav1.ConditionFalse, GPUOperatorPrerequisiteMissingReason, GPUOperatorPrerequisiteMissingMessage
		}

		logf.FromContext(ctx).Error(err, "failed to read GPU Operator ClusterPolicy CRD")
		return metav1.ConditionUnknown, GPUOperatorPrerequisiteUnknownReason, fmt.Sprintf("%s: %s", GPUOperatorPrerequisiteUnknownMessage, err)
	}

	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, crd); err != nil {
		logf.FromContext(ctx).Error(err, "failed to convert GPU Operator ClusterPolicy CRD")
		return metav1.ConditionUnknown, GPUOperatorPrerequisiteUnknownReason, fmt.Sprintf("%s: %s", GPUOperatorPrerequisiteUnknownMessage, err)
	}

	if message := gpuOperatorClusterPolicyNotUsable(crd); message != "" {
		return metav1.ConditionFalse, GPUOperatorPrerequisiteNotUsableReason, message
	}

	return metav1.ConditionTrue, GPUOperatorPrerequisiteMetReason, GPUOperatorPrerequisiteMetMessage
}

// gpuOperatorClusterPolicyNotUsable reports the first failed usability check.
// Terminating wins over a missing served version, which wins over Established.
// An empty result means the CRD serves v1 and is established.
func gpuOperatorClusterPolicyNotUsable(crd *apiextensionsv1.CustomResourceDefinition) string {
	if apihelpers.IsCRDConditionTrue(crd, apiextensionsv1.Terminating) {
		return GPUOperatorPrerequisiteTerminatingMessage
	}
	if !gpuOperatorClusterPolicyServed(crd) {
		return GPUOperatorPrerequisiteNotServingMessage
	}
	if !apihelpers.IsCRDConditionTrue(crd, apiextensionsv1.Established) {
		return GPUOperatorPrerequisiteNotEstablishedMessage
	}

	return ""
}

// gpuOperatorClusterPolicyServed reports whether any spec.versions entry
// serves the ClusterPolicy version this operator reads. The served entry does
// not have to be first.
func gpuOperatorClusterPolicyServed(crd *apiextensionsv1.CustomResourceDefinition) bool {
	for _, version := range crd.Spec.Versions {
		if version.Name == gvk.GPUOperatorClusterPolicy.Version && version.Served {
			return true
		}
	}

	return false
}

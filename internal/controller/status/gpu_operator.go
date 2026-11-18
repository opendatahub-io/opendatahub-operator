package status

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// GPUOperatorPrerequisite reports ClusterPolicy CRD presence for DSC and DSCI.
// Detection uses API discovery only. A missing CRD and a discovery failure are
// both returned as conditions; callers publish the condition and continue.
func GPUOperatorPrerequisite(ctx context.Context, cli client.Client) (metav1.ConditionStatus, string, string) {
	hasCRD, err := cluster.HasCRD(ctx, cli, gvk.GPUOperatorClusterPolicy)
	if err != nil {
		logf.FromContext(ctx).Error(err, "failed to detect GPU Operator ClusterPolicy CRD")
		return metav1.ConditionUnknown, GPUOperatorPrerequisiteUnknownReason, GPUOperatorPrerequisiteUnknownMessage
	}

	if hasCRD {
		return metav1.ConditionTrue, GPUOperatorPrerequisiteMetReason, GPUOperatorPrerequisiteMetMessage
	}

	return metav1.ConditionFalse, GPUOperatorPrerequisiteMissingReason, GPUOperatorPrerequisiteMissingMessage
}

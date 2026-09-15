package common

import (
	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	// RHCLOperatorNamespace is the default namespace where the rhcl-operator
	// chart deploys the RHCL/Kuadrant operator Deployments. User-configurable
	// via RHCLDependency.Configuration.OperatorNamespace; use
	// deps.RHCL.GetOperatorNamespace() to resolve the effective value.
	RHCLOperatorNamespace = ccmcommon.DefaultNamespaceRHCLOperator

	// RHCLOperandNamespace is the default namespace where the rhcl-operator
	// chart creates the Kuadrant custom resource. User-configurable via
	// RHCLDependency.Configuration.OperandNamespace; use
	// deps.RHCL.GetOperandNamespace() to resolve the effective value.
	RHCLOperandNamespace = ccmcommon.DefaultNamespaceRHCLOperand
)

var (
	LWSOperatorCR = types.OperatorCR{
		GVK:  gvk.LeaderWorkerSetOperatorV1,
		Name: "cluster",
	}

	SailOperatorCR = types.OperatorCR{
		GVK:  gvk.Istio,
		Name: "default",
	}
)

// NewRHCLOperatorCR returns the Kuadrant custom resource created by the
// rhcl-operator chart (templates/kuadrant.yaml). RHCL's operand is
// namespace-scoped, so construct this value for each reconciliation instead of
// mutating shared package state when a custom namespace is configured.
func NewRHCLOperatorCR(operandNamespace string) types.OperatorCR {
	return types.OperatorCR{
		GVK:       gvk.Kuadrantv1beta1,
		Name:      "kuadrant",
		Namespace: operandNamespace,
	}
}

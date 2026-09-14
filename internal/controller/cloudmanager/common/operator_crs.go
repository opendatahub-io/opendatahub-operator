package common

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	// RHCLOperatorNamespace is the namespace where the rhcl-operator chart deploys
	// the RHCL/Kuadrant operator Deployments, matching the chart's default
	// operatorNamespace value (charts/dependencies/rhcl-operator/values.yaml).
	RHCLOperatorNamespace = "kuadrant-operators"

	// RHCLOperandNamespace is the namespace where the rhcl-operator chart creates
	// the Kuadrant custom resource, matching the chart's default operandNamespace
	// value (charts/dependencies/rhcl-operator/values.yaml).
	RHCLOperandNamespace = "kuadrant-system"
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

	// RHCLOperatorCR is the Kuadrant custom resource created by the rhcl-operator
	// chart (templates/kuadrant.yaml). Unlike LWSOperatorCR/SailOperatorCR, it is
	// namespace-scoped, matching the chart's default operandNamespace.
	RHCLOperatorCR = types.OperatorCR{
		GVK:       gvk.Kuadrantv1beta1,
		Name:      "kuadrant",
		Namespace: RHCLOperandNamespace,
	}
)

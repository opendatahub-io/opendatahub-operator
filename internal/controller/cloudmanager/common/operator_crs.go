package common

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

var (
	CertManagerOperatorCR = types.OperatorCR{
		GVK:  gvk.CertManagerV1Alpha1,
		Name: "cluster",
	}

	LWSOperatorCR = types.OperatorCR{
		GVK:  gvk.LeaderWorkerSetOperatorV1,
		Name: "cluster",
	}

	SailOperatorCR = types.OperatorCR{
		GVK:  gvk.Istio,
		Name: "default",
	}
)

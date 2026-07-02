package cloudmanager

import (
	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/cleanup"
)

// IstioCleanupTarget returns the cleanup.Target for the Istio CR.
// Foreground deletion ensures K8s waits for children with
// blockOwnerDeletion=true (e.g. IstioRevision) before removing the CR.
func IstioCleanupTarget() cleanup.Target {
	return cleanup.Target{
		GVK:  ccmcommon.SailOperatorCR.GVK,
		Name: ccmcommon.SailOperatorCR.Name,
	}
}

// FinalizerCleanupTargets returns the cleanup targets used by the finalizer
// action during parent CR deletion. These ensure dependency CRs are deleted
// before the cascade removes their operators.
//
// Note: the CertManager/cluster target is intentionally omitted here because
// certmanager.Bootstrap already registers its own cleanup finalizer for it.
func FinalizerCleanupTargets() []cleanup.Target {
	return []cleanup.Target{
		IstioCleanupTarget(),
	}
}

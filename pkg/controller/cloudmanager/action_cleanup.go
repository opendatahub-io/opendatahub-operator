package cloudmanager

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/cleanup"
	certmanager "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
)

// IstioCleanupTarget returns the cleanup.Target for the Istio CR.
// Foreground deletion ensures K8s waits for children with
// blockOwnerDeletion=true (e.g. IstioRevision) before removing the CR.
func IstioCleanupTarget() cleanup.Target {
	return cleanup.Target{
		GVK:  gvk.Istio,
		Name: "default",
	}
}

// FinalizerCleanupTargets returns the cleanup targets used by the finalizer
// action during parent CR deletion. These ensure dependency CRs are deleted
// before the cascade removes their operators.
func FinalizerCleanupTargets() []cleanup.Target {
	return []cleanup.Target{
		certmanager.BootstrapCleanupTarget(),
		IstioCleanupTarget(),
	}
}

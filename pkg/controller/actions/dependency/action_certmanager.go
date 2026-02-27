package dependency

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// certManagerCRD* are the Kubernetes resource names of the three core cert-manager CRDs,
// in the form <plural>.<group>.
const (
	certManagerCertificateCRD   = "certificates.cert-manager.io"
	certManagerIssuerCRD        = "issuers.cert-manager.io"
	certManagerClusterIssuerCRD = "clusterissuers.cert-manager.io"
)

// MonitorCertManagerCRDs returns an ActionOpts that checks whether the three core cert-manager
// CRDs (Certificate, Issuer, ClusterIssuer) are registered on the cluster. If any CRD is absent,
// DependenciesAvailable is set to False. Include this in any reconciler pipeline that requires
// cert-manager to be installed before processing cert-manager resources.
func MonitorCertManagerCRDs() ActionOpts {
	return func(a *Action) {
		a.crdConfigs = append(a.crdConfigs,
			CRDConfig{GVK: gvk.CertManagerCertificate},
			CRDConfig{GVK: gvk.CertManagerIssuer},
			CRDConfig{GVK: gvk.CertManagerClusterIssuer},
		)
	}
}

// CertManagerCRDPredicate returns a predicate that matches CustomResourceDefinition events for
// the three core cert-manager CRDs. Use with Watches(&extv1.CustomResourceDefinition{}, ...) to
// trigger reconciliation when cert-manager is installed or uninstalled on the cluster.
func CertManagerCRDPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		switch obj.GetName() {
		case certManagerCertificateCRD, certManagerIssuerCRD, certManagerClusterIssuerCRD:
			return true
		}
		return false
	})
}

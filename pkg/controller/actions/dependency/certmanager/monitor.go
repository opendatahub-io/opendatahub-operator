package certmanager

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency"
)

// certManagerCRD* are the Kubernetes resource names of the three core cert-manager CRDs,
// in the form <plural>.<group>.
const (
	certManagerCertificateCRD   = "certificates.cert-manager.io"
	certManagerIssuerCRD        = "issuers.cert-manager.io"
	certManagerClusterIssuerCRD = "clusterissuers.cert-manager.io"
)

// MonitorCRDs returns an ActionOpts that checks whether the three core cert-manager
// CRDs (Certificate, Issuer, ClusterIssuer) are registered on the cluster. If any CRD
// is absent, DependenciesAvailable is set to False.
func MonitorCRDs() dependency.ActionOpts {
	return dependency.Combine(
		dependency.MonitorCRD(dependency.CRDConfig{GVK: gvk.CertManagerCertificate}),
		dependency.MonitorCRD(dependency.CRDConfig{GVK: gvk.CertManagerIssuer}),
		dependency.MonitorCRD(dependency.CRDConfig{GVK: gvk.CertManagerClusterIssuer}),
	)
}

// CRDPredicate returns a predicate that matches CustomResourceDefinition events for
// the three core cert-manager CRDs. Use with Watches(&extv1.CustomResourceDefinition{}, ...)
// to trigger reconciliation when cert-manager is installed or uninstalled on the cluster.
func CRDPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		switch obj.GetName() {
		case certManagerCertificateCRD, certManagerIssuerCRD, certManagerClusterIssuerCRD:
			return true
		}
		return false
	})
}

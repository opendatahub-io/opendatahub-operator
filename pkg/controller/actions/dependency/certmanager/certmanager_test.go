package certmanager_test

// cert-manager CRD plural and singular names used for test CRD registration.
// These are derived from cert-manager's published CRD naming and must be updated
// if cert-manager changes its CRD naming in a future version.
const (
	certManagerCertificatePlural     = "certificates"
	certManagerCertificateSingular   = "certificate"
	certManagerIssuerPlural          = "issuers"
	certManagerIssuerSingular        = "issuer"
	certManagerClusterIssuerPlural   = "clusterissuers"
	certManagerClusterIssuerSingular = "clusterissuer"
)

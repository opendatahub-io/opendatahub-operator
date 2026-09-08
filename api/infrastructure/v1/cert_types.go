package v1

type CertType string

const (
	// SelfSigned requests an operator-managed TLS certificate. When cert-manager is available
	// on the cluster the certificate is issued (and auto-renewed) by cert-manager using the
	// resolved issuer (see IssuerRef); otherwise the operator generates a self-signed certificate.
	SelfSigned CertType = "SelfSigned"
	Provided   CertType = "Provided"
	// OpenshiftDefaultIngress uses the cluster's default ingress certificate (OpenShift only).
	OpenshiftDefaultIngress CertType = "OpenshiftDefaultIngress"
)

// IssuerRef references the cert-manager issuer used to sign the certificate.
// It is only consulted when cert-manager is available on the cluster; otherwise it is ignored.
// When unset (or with empty fields), the platform default issuer is used — resolved from the
// operator's RHAI_ISSUER_REF_* environment variables (e.g. rhai-ca-issuer on RHOAI).
type IssuerRef struct {
	// Name of the cert-manager issuer. When empty, the platform default issuer name is used.
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
	// Kind of the cert-manager issuer.
	// +kubebuilder:validation:Enum=Issuer;ClusterIssuer
	// +kubebuilder:default=ClusterIssuer
	Kind string `json:"kind,omitempty"`
}

// CertificateSpec represents the specification of the certificate securing communications of
// an Istio Gateway.
type CertificateSpec struct {
	// SecretName specifies the name of the Kubernetes Secret resource that contains a
	// TLS certificate secure HTTP communications for the KNative network.
	SecretName string `json:"secretName,omitempty"`
	// Type specifies if the TLS certificate should be generated automatically, or if the certificate
	// is provided by the user. Allowed values are:
	// * SelfSigned: A certificate is going to be generated using an own private key.
	// * Provided: Pre-existence of the TLS Secret (see SecretName) with a valid certificate is assumed.
	// * OpenshiftDefaultIngress: Uses the cluster's default ingress certificate (OpenShift only).
	// +kubebuilder:validation:Enum=SelfSigned;Provided;OpenshiftDefaultIngress
	// +kubebuilder:default=OpenshiftDefaultIngress
	Type CertType `json:"type,omitempty"`
	// IssuerRef optionally overrides the cert-manager issuer used to sign the certificate.
	// It only takes effect when cert-manager is available on the cluster (the SelfSigned type).
	// When cert-manager is not installed, certificate generation falls back to an operator-managed
	// self-signed certificate and this field is ignored.
	// +optional
	IssuerRef *IssuerRef `json:"issuerRef,omitempty"`
}

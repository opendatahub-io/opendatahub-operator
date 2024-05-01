package v1

type CertType string

const (
	SelfSigned     CertType = "SelfSigned"
	Provided       CertType = "Provided"
	DefaultIngress CertType = "DefaultIngress"
)

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
	// * DefaultIngress: Default ingress certificate configured for OpenShift
	// +kubebuilder:validation:Enum=SelfSigned;Provided;DefaultIngress
	// +kubebuilder:default=DefaultIngress
	Type CertType `json:"type,omitempty"`
}

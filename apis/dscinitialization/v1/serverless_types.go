package v1

import operatorv1 "github.com/openshift/api/operator/v1"

type CertType string

const (
	SelfSigned CertType = "SelfSigned"
	Provided   CertType = "Provided"
)

// ServerlessSpec configures KNative components used in Open Data Hub. Specifically,
// KNative is used to enable single model serving (KServe).
type ServerlessSpec struct {
	// +kubebuilder:validation:Enum=Managed;Removed
	// +kubebuilder:default=Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
	// Serving configures the KNative-Serving stack used for model serving. A Service
	// Mesh (Istio) is prerequisite, since it is used as networking layer.
	Serving ServingSpec `json:"serving,omitempty"`
}

// ServingSpec specifies the configuration for the KNative Serving components and their
// bindings with the Service Mesh.
type ServingSpec struct {
	// Name specifies the name of the KNativeServing resource that is going to be
	// created to instruct the KNative Operator to deploy KNative serving components.
	// +kubebuilder:default=knative-serving
	Name string `json:"name,omitempty"`
	// Namespace specifies the namespace where the KNativeServing resource is going
	// to be created.
	// +kubebuilder:default=knative-serving
	Namespace string `json:"namespace,omitempty"`
	// LocalGatewayServiceName allows to customize the name of the Kubernetes Service that
	// is going to be created for intra-cluster requests. The service is created in the
	// Service Mesh namespace.
	// +kubebuilder:default=knative-local-gateway
	LocalGatewayServiceName string `json:"localGatewayServiceName,omitempty"`
	// IngressGateway allows to customize some parameters for the Istio Ingress Gateway
	// that is bound to KNative-Serving.
	IngressGateway IngressGatewaySpec `json:"ingressGateway,omitempty"`
}

// IngressGatewaySpec represents the configuration of the KNative Ingress Gateway.
type IngressGatewaySpec struct {
	// GatewaySelector specifies the label selector to choose the Istio Ingress Gateway to use
	// for intercepting incoming requests. If unset, the selector knative=ingressgateway is used.
	// GatewaySelector map[string]string `json:"selector,omitempty"`

	// Domain specifies the DNS name for intercepting ingress requests coming from
	// outside the cluster. Most likely, you will want to use a wildcard name,
	// like *.example.com. If not set, the domain of the OpenShift Ingress is used.
	// If you choose to generate a certificate, this is the domain used for the certificate request.
	Domain string `json:"domain,omitempty"`
	// Certificate specifies configuration about the location of the TLS certificate and
	// if a certificate would be generated.
	Certificate CertificateSpec `json:"certificate,omitempty"`
}

// CertificateSpec represents the specification of the certificate securing communications of
// the Istio Ingress Gateway for the KNative network.
type CertificateSpec struct {
	// SecretName specifies the name of the Kubernetes Secret resource that contains a
	// TLS certificate secure HTTP communications for the KNative network.
	// +kubebuilder:default=knative-serving-cert
	SecretName string `json:"secretName,omitempty"`
	// Type specifies if the TLS certificate should be generated automatically, or if the certificate
	// is provided by the user. Allowed values are:
	// * SelfSigned: A certificate is going to be generated using an own private key.
	// * Provided: Pre-existence of the TLS Secret (see SecretName) with a valid certificate is assumed.
	// +kubebuilder:validation:Enum=SelfSigned;Provided
	// +kubebuilder:default=SelfSigned
	Type CertType `json:"type,omitempty"`
}

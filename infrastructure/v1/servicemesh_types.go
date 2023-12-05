package v1

import operatorv1 "github.com/openshift/api/operator/v1"

// ServiceMeshSpec configures Service Mesh.
type ServiceMeshSpec struct {
	// +kubebuilder:validation:Enum=Managed;Removed
	// +kubebuilder:default=Managed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
	// ControlPlane holds configuration of Service Mesh used by Opendatahub.
	ControlPlane ControlPlaneSpec `json:"controlPlane,omitempty"`
}

type ControlPlaneSpec struct {
	// Name is a name Service Mesh Control Plane. Defaults to "data-science-smcp".
	// +kubebuilder:default=data-science-smcp
	Name string `json:"name,omitempty"`
	// Namespace is a namespace where Service Mesh is deployed. Defaults to "istio-system".
	// +kubebuilder:default=istio-system
	Namespace string `json:"namespace,omitempty"`
	// MetricsCollection specifies if metrics from components on the Mesh namespace
	// should be collected. Setting the value to "Istio" will collect metrics from the
	// control plane and any proxies on the Mesh namespace (like gateway pods). Setting
	// to "None" will disable metrics collection.
	// +kubebuilder:validation:Enum=Istio;None
	// +kubebuilder:default=Istio
	MetricsCollection string `json:"metricsCollection,omitempty"`
}

// IngressGatewaySpec represents the configuration of the Ingress Gateways.
type IngressGatewaySpec struct {
	// Domain specifies the DNS name for intercepting ingress requests coming from
	// outside the cluster. Most likely, you will want to use a wildcard name,
	// like *.example.com. If not set, the domain of the OpenShift Ingress is used.
	// If you choose to generate a certificate, this is the domain used for the certificate request.
	Domain string `json:"domain,omitempty"`
	// Certificate specifies configuration of the TLS certificate securing communications of
	// the for Ingress Gateway.
	Certificate CertificateSpec `json:"certificate,omitempty"`
}

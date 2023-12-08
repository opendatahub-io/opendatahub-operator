package v1

import (
	operatorv1 "github.com/openshift/api/operator/v1"
)

// ServingSpec specifies the configuration for the KNative Serving components and their
// bindings with the Service Mesh.
type ServingSpec struct {
	// +kubebuilder:validation:Enum=Managed;Unmanaged;Removed
	// +kubebuilder:default=Managed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
	// Name specifies the name of the KNativeServing resource that is going to be
	// created to instruct the KNative Operator to deploy KNative serving components.
	// This resource is created in the "knative-serving" namespace.
	// +kubebuilder:default=knative-serving
	Name string `json:"name,omitempty"`
	// IngressGateway allows to customize some parameters for the Istio Ingress Gateway
	// that is bound to KNative-Serving.
	IngressGateway IngressGatewaySpec `json:"ingressGateway,omitempty"`
}

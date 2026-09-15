package common

import (
	apicommon "github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

// ManagementPolicy defines the policy for managing a cloud manager dependency.
// +kubebuilder:validation:Enum=Managed;Unmanaged
type ManagementPolicy string

const (
	// Managed means the operator installs and actively reconciles the dependency.
	Managed ManagementPolicy = "Managed"
	// Unmanaged means the operator does not install or manage the dependency.
	// The user is responsible for ensuring the dependency is available.
	Unmanaged ManagementPolicy = "Unmanaged"
)

// Default namespaces for cloud manager dependencies.
const (
	DefaultNamespaceCertManagerOperator = "cert-manager-operator"
	DefaultNamespaceCertManagerOperand  = "cert-manager"
	DefaultNamespaceLWSOperator         = "openshift-lws-operator"
	DefaultNamespaceSailOperator        = "istio-system"
	DefaultNamespaceRHCLOperator        = "kuadrant-operators"
	DefaultNamespaceRHCLOperand         = "kuadrant-system"
)

// Namespace represents a Kubernetes namespace name (RFC 1123 DNS label).
// +kubebuilder:validation:Pattern="^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$"
// +kubebuilder:validation:MaxLength=63
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="namespace is immutable"
type Namespace string

// Deprecated: cert-manager configuration is no longer used by the Cloud Controller Manager.
// This type is retained for backwards compatibility.
// +kubebuilder:object:generate=true
type CertManagerConfiguration struct{}

// LWSConfiguration defines the configuration for the LeaderWorkerSet (LWS) operator dependency.
// +kubebuilder:object:generate=true
type LWSConfiguration struct {
	// Namespace is the namespace where the LWS operator is deployed.
	// +kubebuilder:default=openshift-lws-operator
	Namespace Namespace `json:"namespace,omitempty"`
}

// SailOperatorConfiguration defines the configuration for the Sail operator (Istio) dependency.
// +kubebuilder:object:generate=true
type SailOperatorConfiguration struct {
	// Namespace is the namespace where the Sail operator (Istio) is deployed.
	// +kubebuilder:default=istio-system
	Namespace Namespace `json:"namespace,omitempty"`
}

// GatewayAPIConfiguration defines the configuration for the Gateway API dependency.
// +kubebuilder:object:generate=true
type GatewayAPIConfiguration struct{}

// RHCLConfiguration defines the configuration for the RHCL (Red Hat Connectivity
// Link / Kuadrant) operator dependency.
// +kubebuilder:object:generate=true
type RHCLConfiguration struct {
	// OperatorNamespace is the namespace where the RHCL/Kuadrant operator
	// Deployments (kuadrant-operator, authorino-operator, limitador-operator,
	// dns-operator) are deployed.
	// +kubebuilder:default=kuadrant-operators
	OperatorNamespace Namespace `json:"operatorNamespace,omitempty"`

	// OperandNamespace is the namespace where the Kuadrant custom resource
	// (the RHCL operand) is created.
	// +kubebuilder:default=kuadrant-system
	OperandNamespace Namespace `json:"operandNamespace,omitempty"`
}

// Deprecated: cert-manager is no longer a Cloud Controller Manager dependency.
// This type is retained for backwards compatibility.
// +kubebuilder:object:generate=true
type CertManagerDependency struct {
	// Deprecated: cert-manager installation is no longer managed by the Cloud
	// Controller Manager. This field has no runtime effect.
	// +kubebuilder:validation:XValidation:rule="self != 'Managed' || self == oldSelf",message="cert-manager managementPolicy cannot be set to Managed"
	ManagementPolicy ManagementPolicy `json:"managementPolicy,omitempty"`

	// Deprecated: cert-manager configuration is no longer used by the Cloud
	// Controller Manager. This field has no runtime effect.
	// +optional
	Configuration CertManagerConfiguration `json:"configuration,omitempty"`
}

// LWSDependency defines the LeaderWorkerSet operator dependency.
// +kubebuilder:object:generate=true
type LWSDependency struct {
	// ManagementPolicy determines whether the operator manages this dependency.
	// Managed: the operator installs and reconciles the dependency.
	// Unmanaged: the operator does not manage the dependency; the user is responsible.
	// +kubebuilder:default=Managed
	ManagementPolicy ManagementPolicy `json:"managementPolicy,omitempty"`

	// Configuration for the LWS operator.
	// +optional
	// +kubebuilder:default={}
	Configuration LWSConfiguration `json:"configuration,omitempty"`
}

// GetNamespace returns the namespace where the LWS operator is deployed,
// falling back to DefaultNamespaceLWSOperator if empty.
func (d *LWSDependency) GetNamespace() string {
	if d.Configuration.Namespace != "" {
		return string(d.Configuration.Namespace)
	}

	return DefaultNamespaceLWSOperator
}

// SailOperatorDependency defines the Sail operator (Istio) dependency.
// +kubebuilder:object:generate=true
type SailOperatorDependency struct {
	// ManagementPolicy determines whether the operator manages this dependency.
	// Managed: the operator installs and reconciles the dependency.
	// Unmanaged: the operator does not manage the dependency; the user is responsible.
	// +kubebuilder:default=Managed
	ManagementPolicy ManagementPolicy `json:"managementPolicy,omitempty"`

	// Configuration for the Sail operator.
	// +optional
	// +kubebuilder:default={}
	Configuration SailOperatorConfiguration `json:"configuration,omitempty"`
}

// GetNamespace returns the namespace where the Sail operator is deployed,
// falling back to DefaultNamespaceSailOperator if empty.
func (d *SailOperatorDependency) GetNamespace() string {
	if d.Configuration.Namespace != "" {
		return string(d.Configuration.Namespace)
	}

	return DefaultNamespaceSailOperator
}

// GatewayAPIDependency defines the Gateway API dependency.
// +kubebuilder:object:generate=true
type GatewayAPIDependency struct {
	// ManagementPolicy determines whether the operator manages this dependency.
	// Managed: the operator installs and reconciles the dependency.
	// Unmanaged: the operator does not manage the dependency; the user is responsible.
	// +kubebuilder:default=Managed
	ManagementPolicy ManagementPolicy `json:"managementPolicy,omitempty"`

	// Configuration for the Gateway API.
	// +optional
	Configuration GatewayAPIConfiguration `json:"configuration,omitempty"`
}

// RHCLDependency defines the RHCL (Red Hat Connectivity Link / Kuadrant) operator dependency.
// +kubebuilder:object:generate=true
type RHCLDependency struct {
	// ManagementPolicy determines whether the operator manages this dependency.
	// Managed: the operator installs and reconciles the dependency.
	// Unmanaged: the operator does not manage the dependency; the user is responsible.
	// Defaults to Unmanaged, unlike the other CCM dependencies: RHCL's chart is
	// significantly heavier (~30 CRDs, several Deployments) and is an opt-in,
	// specialized capability rather than something every xKS cluster needs.
	// +kubebuilder:default=Unmanaged
	ManagementPolicy ManagementPolicy `json:"managementPolicy,omitempty"`

	// Configuration for the RHCL operator.
	// +optional
	// +kubebuilder:default={}
	Configuration RHCLConfiguration `json:"configuration,omitempty"`
}

// GetOperatorNamespace returns the namespace where the RHCL/Kuadrant operator
// Deployments are deployed, falling back to DefaultNamespaceRHCLOperator if empty.
func (d *RHCLDependency) GetOperatorNamespace() string {
	if d.Configuration.OperatorNamespace != "" {
		return string(d.Configuration.OperatorNamespace)
	}

	return DefaultNamespaceRHCLOperator
}

// GetOperandNamespace returns the namespace where the Kuadrant custom resource
// is created, falling back to DefaultNamespaceRHCLOperand if empty.
func (d *RHCLDependency) GetOperandNamespace() string {
	if d.Configuration.OperandNamespace != "" {
		return string(d.Configuration.OperandNamespace)
	}

	return DefaultNamespaceRHCLOperand
}

// KubernetesEngineInstance is implemented by CCM CR types that expose their Dependencies.
type KubernetesEngineInstance interface {
	apicommon.PlatformObject
	GetDependencies() Dependencies
}

// Dependencies defines the dependency configurations for cloud manager operators.
// +kubebuilder:object:generate=true
type Dependencies struct {
	// Deprecated: cert-manager is no longer a Cloud Controller Manager dependency.
	// The XKS chart owns its installation. This field has no runtime effect and
	// is retained for backwards compatibility.
	// +optional
	CertManager CertManagerDependency `json:"certManager,omitempty"`

	// LWS defines the LeaderWorkerSet operator dependency.
	// +optional
	LWS LWSDependency `json:"lws,omitempty"`

	// SailOperator defines the Sail operator (Istio) dependency.
	// +optional
	SailOperator SailOperatorDependency `json:"sailOperator,omitempty"`

	// GatewayAPI defines the Gateway API dependency.
	// +optional
	GatewayAPI GatewayAPIDependency `json:"gatewayAPI,omitempty"`

	// RHCL defines the RHCL (Red Hat Connectivity Link / Kuadrant) operator dependency.
	// +optional
	RHCL RHCLDependency `json:"rhcl,omitempty"`
}

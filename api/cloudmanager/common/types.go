package common

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
	DefaultNamespaceLWSOperator  = "openshift-lws-operator"
	DefaultNamespaceSailOperator = "istio-system"
)

// CertManagerConfiguration defines the configuration for the cert-manager operator dependency.
// +kubebuilder:object:generate=true
type CertManagerConfiguration struct{}

// LWSConfiguration defines the configuration for the LeaderWorkerSet (LWS) operator dependency.
// +kubebuilder:object:generate=true
type LWSConfiguration struct {
	// Namespace is the namespace where the LWS operator is deployed.
	// +kubebuilder:default=openshift-lws-operator
	// +kubebuilder:validation:Pattern="^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$"
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="namespace is immutable"
	Namespace string `json:"namespace,omitempty"`
}

// SailOperatorConfiguration defines the configuration for the Sail operator (Istio) dependency.
// +kubebuilder:object:generate=true
type SailOperatorConfiguration struct {
	// Namespace is the namespace where the Sail operator (Istio) is deployed.
	// +kubebuilder:default=istio-system
	// +kubebuilder:validation:Pattern="^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$"
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="namespace is immutable"
	Namespace string `json:"namespace,omitempty"`
}

// GatewayAPIConfiguration defines the configuration for the Gateway API dependency.
// +kubebuilder:object:generate=true
type GatewayAPIConfiguration struct{}

// CertManagerDependency defines the cert-manager operator dependency.
// +kubebuilder:object:generate=true
type CertManagerDependency struct {
	// ManagementPolicy determines whether the operator manages this dependency.
	// Managed: the operator installs and reconciles the dependency.
	// Unmanaged: the operator does not manage the dependency; the user is responsible.
	// +kubebuilder:default=Managed
	ManagementPolicy ManagementPolicy `json:"managementPolicy,omitempty"`

	// Configuration for the cert-manager operator.
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
		return d.Configuration.Namespace
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
		return d.Configuration.Namespace
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

// Dependencies defines the dependency configurations for cloud manager operators.
// +kubebuilder:object:generate=true
type Dependencies struct {
	// CertManager defines the cert-manager operator dependency.
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
}

package common

import (
	fwapi "github.com/opendatahub-io/operator-actions-framework/api"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/operator-framework/api/pkg/lib/version"
)

// ManagementSpec struct defines the component's management configuration.
// +kubebuilder:object:generate=true
type ManagementSpec struct {
	// Set to one of the following values:
	//
	// - "Managed" : the operator is actively managing the component and trying to keep it active.
	//               It will only upgrade the component if it is safe to do so
	//
	// - "Removed" : the operator is actively managing the component and will not install it,
	//               or if it is installed, the operator will try to remove it
	//
	// +kubebuilder:validation:Enum=Managed;Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
}

// GatewayOIDCSpec is a minimal OIDC projection for component workloads (e.g. issuer URL for
// discovery/JWKS). Heavier platform OIDC client settings remain on GatewayConfig.
// +kubebuilder:object:generate=true
type GatewayOIDCSpec struct {
	// IssuerURL is the OIDC issuer URL (for example "https://keycloak.example.com/realms/myorg").
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:Format=uri
	// +kubebuilder:validation:Pattern=`^https://\S+$`
	IssuerURL string `json:"issuerURL"`
}

// GatewaySpec defines gateway-related settings for components (domain, OIDC projections, etc.).
// This is a shared type used across Dashboard, ModelRegistry, and potentially others.
// +kubebuilder:object:generate=true
type GatewaySpec struct {
	// Domain is the fully qualified domain name for the gateway.
	// Example: "rhods-dashboard.apps.example.com"
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$`
	Domain string `json:"domain"`
}

type ConditionSeverity = fwapi.ConditionSeverity

const (
	ConditionSeverityError = fwapi.ConditionSeverityError
	ConditionSeverityInfo  = fwapi.ConditionSeverityInfo

	ConditionReasonError = fwapi.ConditionReasonError
)

type Condition = fwapi.Condition

type Status = fwapi.Status

// ComponentRelease represents the detailed status of a component release.
// +kubebuilder:object:generate=true
type ComponentRelease struct {
	// +required
	// +kubebuilder:validation:Required
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
	RepoURL string `yaml:"repoUrl,omitempty" json:"repoUrl,omitempty"`
}

// ComponentReleaseStatus tracks the list of component releases, including their name, version, and repository URL.
// +kubebuilder:object:generate=true
type ComponentReleaseStatus struct {
	// +patchMergeKey=name
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=name
	Releases []ComponentRelease `yaml:"releases,omitempty" json:"releases,omitempty"`
}

type WithStatus = fwapi.WithStatus

type ConditionsAccessor = fwapi.ConditionsAccessor

type WithReleases interface {
	GetReleaseStatus() *[]ComponentRelease
	SetReleaseStatus(status []ComponentRelease)
}

type PlatformObject = fwapi.PlatformObject

type Platform = fwapi.Platform

// Release includes information on operator version and platform
// +kubebuilder:object:generate=true
type Release struct {
	Name    Platform                `json:"name,omitempty"`
	Version version.OperatorVersion `json:"version,omitempty"`
}

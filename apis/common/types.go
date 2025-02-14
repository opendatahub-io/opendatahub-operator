package common

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/operator-framework/api/pkg/lib/version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// DevFlags defines list of fields that can be used by developers to test customizations. This is not recommended
// to be used in production environment.
// +kubebuilder:object:generate=true
type DevFlags struct {
	// List of custom manifests for the given component
	// +optional
	Manifests []ManifestsConfig `json:"manifests,omitempty"`
}

// DevFlagsSpec struct defines the component's dev flags configuration.
// +kubebuilder:object:generate=true
type DevFlagsSpec struct {
	// Add developer fields
	// +optional
	DevFlags *DevFlags `json:"devFlags,omitempty"`
}

type ManifestsConfig struct {
	// uri is the URI point to a git repo with tag/branch. e.g.  https://github.com/org/repo/tarball/<tag/branch>
	// +optional
	// +kubebuilder:default:=""
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=1
	URI string `json:"uri,omitempty"`

	// contextDir is the relative path to the folder containing manifests in a repository, default value "manifests"
	// +optional
	// +kubebuilder:default:="manifests"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=2
	ContextDir string `json:"contextDir,omitempty"`

	// sourcePath is the subpath within contextDir where kustomize builds start. Examples include any sub-folder or path: `base`, `overlays/dev`, `default`, `odh` etc.
	// +optional
	// +kubebuilder:default:=""
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=3
	SourcePath string `json:"sourcePath,omitempty"`
}

// +kubebuilder:object:generate=true
type Status struct {
	Phase              string `json:"phase,omitempty"`
	ObservedGeneration int64  `json:"observedGeneration,omitempty"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type WithStatus interface {
	GetStatus() *Status
}

type WithDevFlags interface {
	GetDevFlags() *DevFlags
}

type PlatformObject interface {
	client.Object
	WithStatus
}

type Platform string

// Release includes information on operator version and platform
// +kubebuilder:object:generate=true
type Release struct {
	Name    Platform                `json:"name,omitempty"`
	Version version.OperatorVersion `json:"version,omitempty"`
}

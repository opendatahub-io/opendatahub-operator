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
	// +kubebuilder:default=""
	URI string `json:"uri,omitempty"`

	// contextDir is the relative path to the folder containing manifests in a repository, default value "manifests"
	// +optional
	// +kubebuilder:default="manifests"
	ContextDir string `json:"contextDir,omitempty"`

	// sourcePath is the subpath within contextDir where kustomize builds start. Examples include any sub-folder or path: `base`, `overlays/dev`, `default`, `odh` etc.
	// +optional
	// +kubebuilder:default=""
	SourcePath string `json:"sourcePath,omitempty"`
}

// ConditionSeverity expresses the severity of a Condition Type failing.
type ConditionSeverity string

const (
	ConditionSeverityError   ConditionSeverity = ""
	ConditionSeverityWarning ConditionSeverity = "Warning"
	ConditionSeverityInfo    ConditionSeverity = "Info"

	ConditionReasonError = "Error"
)

// +kubebuilder:object:generate=true
type Condition struct {
	// type of condition in CamelCase or in foo.example.com/CamelCase.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$`
	// +kubebuilder:validation:MaxLength=316
	Type string `json:"type"`

	// status of the condition, one of True, False, Unknown.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=True;False;Unknown
	Status metav1.ConditionStatus `json:"status"`

	// observedGeneration represents the .metadata.generation that the condition was set based upon.
	// For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration
	// is 9, the condition is out of date with respect to the current state of the instance.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=0
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// lastTransitionTime is the last time the condition transitioned from one status to another.
	// This should be when the underlying condition changed.
	// If that is not known, then using the time when the API field changed is acceptable.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`

	// reason contains a programmatic identifier indicating the reason for the condition's last transition.
	// The value should be a CamelCase string.
	//
	// +optional
	// +kubebuilder:validation:Optional
	Reason string `json:"reason,omitempty"`

	// message is a human-readable message indicating details about the transition.
	// +optional
	// +kubebuilder:validation:Optional
	Message string `json:"message,omitempty"`

	// Severity with which to treat failures of this type of condition.
	// When this is not specified, it defaults to Error.
	// +optional
	// +kubebuilder:validation:Optional
	Severity ConditionSeverity `json:"severity,omitempty"`

	// The last time we got an update on a given condition, this should not be set and is
	// present only for backward compatibility reasons
	//
	// +optional
	// +kubebuilder:validation:Optional
	LastHeartbeatTime *metav1.Time `json:"lastHeartbeatTime,omitempty"`
}

// +kubebuilder:object:generate=true
type Status struct {
	Phase string `json:"phase,omitempty"`

	// The generation observed by the resource controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=atomic
	Conditions []Condition `json:"conditions,omitempty"`
}

func (s *Status) GetConditions() []Condition {
	return s.Conditions
}

func (s *Status) SetConditions(conditions []Condition) {
	s.Conditions = append(conditions[:0:0], conditions...)
}

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

type WithStatus interface {
	GetStatus() *Status
}

type WithDevFlags interface {
	GetDevFlags() *DevFlags
}

type ConditionsAccessor interface {
	GetConditions() []Condition
	SetConditions([]Condition)
}

type WithReleases interface {
	GetReleaseStatus() *[]ComponentRelease
	SetReleaseStatus(status []ComponentRelease)
}

type PlatformObject interface {
	client.Object
	WithStatus
	ConditionsAccessor
}

type Platform string

// Release includes information on operator version and platform
// +kubebuilder:object:generate=true
type Release struct {
	Name    Platform                `json:"name,omitempty"`
	Version version.OperatorVersion `json:"version,omitempty"`
}

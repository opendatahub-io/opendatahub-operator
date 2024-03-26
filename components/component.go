// +groupName=datasciencecluster.opendatahub.io
package components

import (
	"context"

	"github.com/go-logr/logr"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	ctrlogger "github.com/opendatahub-io/opendatahub-operator/v2/pkg/logger"
)

// Component struct defines the basis for each OpenDataHub component configuration.
// +kubebuilder:object:generate=true
type Component struct {
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
	// Add any other common fields across components below

	// Add developer fields
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=2
	DevFlags *DevFlags `json:"devFlags,omitempty"`
}

func (c *Component) GetManagementState() operatorv1.ManagementState {
	return c.ManagementState
}

func (c *Component) Cleanup(_ client.Client, _ *dsciv1.DSCInitializationSpec) error {
	// noop
	return nil
}

// DevFlags defines list of fields that can be used by developers to test customizations. This is not recommended
// to be used in production environment.
// +kubebuilder:object:generate=true
type DevFlags struct {
	// List of custom manifests for the given component
	// +optional
	Manifests []ManifestsConfig `json:"manifests,omitempty"`
}

type ManifestsConfig struct {
	// uri is the URI point to a git repo with tag/branch. e.g.  https://github.com/org/repo/tarball/<tag/branch>
	// +optional
	// +kubebuilder:default:=""
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=1
	URI string `json:"uri,omitempty"`

	// contextDir is the relative path to the folder containing manifests in a repository
	// +optional
	// +kubebuilder:default:=""
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=2
	ContextDir string `json:"contextDir,omitempty"`

	// sourcePath is the subpath within contextDir where kustomize builds start. Examples include any sub-folder or path: `base`, `overlays/dev`, `default`, `odh` etc.
	// +optional
	// +kubebuilder:default:=""
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=3
	SourcePath string `json:"sourcePath,omitempty"`
}

type ComponentInterface interface {
	ReconcileComponent(ctx context.Context, cli client.Client, logger logr.Logger,
		owner metav1.Object, DSCISpec *dsciv1.DSCInitializationSpec, currentComponentStatus bool) error
	Cleanup(cli client.Client, DSCISpec *dsciv1.DSCInitializationSpec) error
	GetComponentName() string
	GetManagementState() operatorv1.ManagementState
	OverrideManifests(platform string) error
	ConfigComponentLogger(logger logr.Logger, component string, dscispec *dsciv1.DSCInitializationSpec) logr.Logger
}

// extend origal ConfigLoggers to include component name.
func (c *Component) ConfigComponentLogger(logger logr.Logger, component string, dscispec *dsciv1.DSCInitializationSpec) logr.Logger {
	if dscispec.DevFlags != nil {
		return ctrlogger.ConfigLoggers(dscispec.DevFlags.LogMode).WithName("DSC.Components." + component)
	}
	return logger.WithName("DSC.Components." + component)
}

package modules

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// CRState represents the lifecycle state of a module CR on the cluster.
type CRState int

const (
	// CRStateAbsent means the CR does not exist (or the CRD is not installed).
	CRStateAbsent CRState = iota
	// CRStateAlive means the CR exists and has no deletionTimestamp.
	CRStateAlive
	// CRStateDeleting means the CR exists but has a deletionTimestamp set
	// (the module operator is still processing finalizers).
	CRStateDeleting
)

// ModuleHandler defines the interface for managing a modular component.
// Each module handler manages an out-of-tree module operator and its CR.
//
// Module teams typically embed BaseHandler and only implement IsEnabled
// and BuildModuleCR; the remaining methods have default implementations
// driven by ModuleConfig.
//
//nolint:interfacebloat
type ModuleHandler interface {
	// GetName returns the unique identifier for this module.
	GetName() string

	// IsEnabled returns whether the module should be deployed based on platform
	// configuration.
	IsEnabled(platform *PlatformContext) bool

	// GetGVK returns the GroupVersionKind of the module CR that this handler
	// manages. Used for dynamic watch registration so module CR status changes
	// requeue the DSC controller.
	GetGVK() schema.GroupVersionKind

	// GetOperatorManifests returns the manifest descriptors for deploying this
	// module's operator resources (Deployment, RBAC, CRD). Handlers return
	// either HelmCharts or Manifests (or both). The returned entries are
	// appended to rr.HelmCharts and rr.Manifests respectively, then rendered
	// by the standard action pipeline (helm/kustomize render + deploy).
	//
	// PlatformContext is provided so the base implementation can inject
	// runtime values (e.g. operatorNamespace) into Helm chart values.
	GetOperatorManifests(platform *PlatformContext) OperatorManifests

	// PopulatePlatformModule sets this module's management state on the
	// PlatformModules struct, derived from DSC/DSCI spec.
	// Component-modules read from dscCtx.DSC, service-modules from dscCtx.DSCI.
	// Called by the DSC/DSCI controller to project module enablement into
	// the Platform CR.
	PopulatePlatformModule(pm *configv1alpha1.PlatformModules, dscCtx *DSCContext)

	// BuildModuleCR constructs the module CR as an unstructured object.
	// Called by DSC/DSCI controllers (not the platform controller) to create
	// module CRs with full spec from DSC/DSCI. On xKS users create CRs
	// manually and this method is not called.
	// Component-modules read from dscCtx.DSC, service-modules from dscCtx.DSCI.
	BuildModuleCR(ctx context.Context, cli client.Client, dscCtx *DSCContext) (*unstructured.Unstructured, error)

	// GetRelatedImages returns the RELATED_IMAGE_* environment variable names
	// that the module operator needs injected into its Deployment.
	GetRelatedImages() []string

	// GetModuleStatus reads the current status from the deployed module CR
	// for aggregation into the DSC ModulesReady condition. The returned
	// ModuleStatus includes conditions and generation metadata for staleness
	// detection.
	GetModuleStatus(ctx context.Context, cli client.Client) (*ModuleStatus, error)

	// GetModuleCRState returns the lifecycle state of the module CR:
	// CRStateAbsent (not found / CRD missing), CRStateAlive (exists, no
	// deletionTimestamp), or CRStateDeleting (deletionTimestamp set, operator
	// is still processing finalizers).
	GetModuleCRState(ctx context.Context, cli client.Client) (CRState, error)

	// DeleteModuleCR deletes the module CR from the cluster. Returns nil if
	// the CR (or its CRD) does not exist, making the call idempotent.
	DeleteModuleCR(ctx context.Context, cli client.Client) error

	// DeleteOperatorResources renders the module's operator manifests and
	// deletes each resource from the cluster. Used by the two-phase cleanup
	// action after the module CR has been confirmed deleted.
	DeleteOperatorResources(ctx context.Context, cli client.Client, platform *PlatformContext) error
}

// ReadyConditionTyper allows a module handler to declare the condition type
// string used for per-module status on the DSC (e.g. "AIGatewayReady").
// All handlers embedding BaseHandler satisfy this interface automatically;
// the default derives the type from GVK.Kind + "Ready".
type ReadyConditionTyper interface {
	GetReadyConditionType() string
}

// ContainerNamer allows a module handler to override the default container
// name ("manager") used for RELATED_IMAGE_* and controller image injection.
// All handlers embedding BaseHandler satisfy this interface automatically;
// the override is only active when ModuleConfig.ContainerName is set.
type ContainerNamer interface {
	GetContainerName() string
}

// ControllerImager allows a module handler to declare a RELATED_IMAGE_* env
// var whose value replaces the operator container's image in the rendered
// Deployment. All handlers embedding BaseHandler satisfy this interface
// automatically; the override is only active when ModuleConfig.ControllerImage
// is set.
type ControllerImager interface {
	GetControllerImage() string
}

// InitContainerNamer allows a module handler to declare an init container
// whose image should be overridden with the same ControllerImage value.
// All handlers embedding BaseHandler satisfy this interface automatically;
// the override is only active when ModuleConfig.InitContainerName is set.
type InitContainerNamer interface {
	GetInitContainerName() string
}

// DeploymentNamer is an optional interface a ModuleHandler can implement to
// declare the rendered Deployment name targeted for RELATED_IMAGE_* env
// injection, when it differs from the module name. See
// BaseHandler.GetDeploymentName.
type DeploymentNamer interface {
	GetDeploymentName() string
}

// ModuleStatus holds the parsed status from a module CR. It includes the
// standard conditions, generation metadata for staleness detection, and
// the release version for the platform version handshake.
type ModuleStatus struct {
	// Conditions from .status.conditions on the module CR.
	Conditions []metav1.Condition
	// ObservedGeneration from .status.observedGeneration on the module CR.
	ObservedGeneration int64
	// Generation from .metadata.generation on the module CR.
	Generation int64
	// ReleaseVersion from .status.releases[name="platform"].version on
	// the module CR. Used for the platform version handshake — the module
	// is not considered ready for DAG progression unless this matches the
	// current platform version.
	ReleaseVersion string
}

// OperatorManifests holds the manifest descriptors returned by a module handler.
// A handler typically populates either HelmCharts or Manifests depending on
// whether its operator resources are packaged as Helm charts or Kustomize
// overlays.
type OperatorManifests struct {
	HelmCharts []types.HelmChartInfo
	Manifests  []types.ManifestInfo
}

// DSCContext holds DSC/DSCI references for handler methods called by
// DSC or DSCI controllers. Parallels PlatformContext (platform controller).
// Component-modules read DSC, service-modules read DSCI.
//
// Both fields are nullable: the DSC controller populates only DSC,
// the DSCI controller populates only DSCI. Handlers that depend on
// a nil field should no-op (for PopulatePlatformModule) or return
// an error (for BuildModuleCR).
type DSCContext struct {
	// DSC is the DataScienceCluster instance. Nil when called from the
	// DSCI controller.
	DSC *dscv2.DataScienceCluster
	// DSCI is the DSCInitialization instance. Nil when called from the
	// DSC controller.
	DSCI *dsciv2.DSCInitialization
}

// PlatformContext holds platform-level fields gathered once per reconcile
// and passed to each module handler. It centralizes the platform contract
// so handlers don't need to fetch shared resources individually.
type PlatformContext struct {
	// ApplicationsNamespace is the namespace where module operands deploy.
	ApplicationsNamespace string

	// MonitoringNamespace is the namespace from DSCI.Spec.Monitoring.Namespace.
	// Empty when monitoring is not set on main process or no DSCI(xKS).
	MonitoringNamespace string

	// GatewayDomain is the cluster ingress domain from GatewayConfig.Status.Domain.
	// Empty if GatewayConfig is not yet provisioned.
	GatewayDomain string

	// Release identifies the platform (ODH/RHOAI) and version.
	Release common.Release

	// Platform is the Platform CR instance. Handlers read per-module
	// ManagementSpec from Platform.Spec.Modules. On OpenShift, DSC/DSCI
	// controllers project enablement into Platform CR via SSA.
	Platform *configv1alpha1.Platform

	// ChartsBasePath is the base directory for locally-bundled Helm charts.
	ChartsBasePath string

	// ManifestsBasePath is the base directory for locally-bundled Kustomize
	// manifests. Handlers using ManifestDir resolve their overlay paths
	// relative to this directory.
	ManifestsBasePath string
}

// RegistrationOption configures optional orchestration metadata when adding
// a module to the registry.
type RegistrationOption func(*registryEntry)

// WithRunlevel sets the runlevel for DAG-based ordering. Lower runlevels
// are provisioned first; all nodes in a runlevel must be Ready before the
// next runlevel begins. Use the pre-defined constants in the dag package
// (e.g. dag.RL(20)). Modules without an explicit runlevel default
// to dag.RL(99) (provisioned last).
func WithRunlevel(level dag.Runlevel) RegistrationOption {
	return func(e *registryEntry) {
		e.runlevel = level
	}
}

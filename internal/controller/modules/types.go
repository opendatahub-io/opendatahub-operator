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
type ModuleHandler interface {
	// GetName returns the unique identifier for this module.
	GetName() string

	// IsEnabled returns whether the module should be deployed based on platform
	// configuration. Component modules check platform.DSC; service modules
	// check platform.DSCI.
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

	// BuildModuleCR constructs the module CR as an unstructured object with
	// platform fields projected from PlatformContext. The returned object is
	// added to rr.Resources and applied by deploy.NewAction alongside operator
	// resources. This is the single isolation point for the platform-to-module-CR
	// field mapping.
	BuildModuleCR(ctx context.Context, cli client.Client, platform *PlatformContext) (*unstructured.Unstructured, error)

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

// ModuleStatus holds the parsed status from a module CR. It includes the
// standard conditions and generation metadata needed for staleness detection
// per the onboarding guide's PlatformObject contract.
type ModuleStatus struct {
	// Conditions from .status.conditions on the module CR.
	Conditions []metav1.Condition
	// ObservedGeneration from .status.observedGeneration on the module CR.
	ObservedGeneration int64
	// Generation from .metadata.generation on the module CR.
	Generation int64
}

// OperatorManifests holds the manifest descriptors returned by a module handler.
// A handler typically populates either HelmCharts or Manifests depending on
// whether its operator resources are packaged as Helm charts or Kustomize
// overlays.
type OperatorManifests struct {
	HelmCharts []types.HelmChartInfo
	Manifests  []types.ManifestInfo
}

// PlatformContext holds platform-level fields gathered once per reconcile
// and passed to each module handler's BuildModuleCR. It centralizes the
// platform contract so handlers don't need to fetch shared resources
// individually.
type PlatformContext struct {
	// ApplicationsNamespace is the namespace where module operands deploy.
	ApplicationsNamespace string

	// GatewayDomain is the cluster ingress domain from GatewayConfig.Status.Domain.
	// Empty if GatewayConfig is not yet provisioned.
	GatewayDomain string

	// Release identifies the platform (ODH/RHOAI) and version.
	Release common.Release

	// DSC is the DataScienceCluster instance. Handlers read their
	// module-specific component stanza from it (e.g., DSC.Spec.Components.MyModule).
	// Nil in standalone mode (xKS) where no DSC CRD is installed.
	DSC *dscv2.DataScienceCluster

	// DSCI is the DSCInitialization instance. Service-type modules read
	// their configuration from it (e.g., DSCI.Spec.Monitoring).
	// Nil in standalone mode (xKS) where no DSCI CRD is installed.
	DSCI *dsciv2.DSCInitialization

	// Platform is the Platform CR instance. Non-nil only in standalone
	// mode (xKS) where DSC/DSCI are suppressed. Handlers use it to read
	// per-module ManagementSpec from Platform.Spec.Modules.
	Platform *configv1alpha1.Platform

	// ChartsBasePath is the base directory for locally-bundled Helm charts.
	ChartsBasePath string
}

// RegistrationOption configures optional orchestration metadata when adding
// a module to the registry.
type RegistrationOption func(*registryEntry)

// WithRunlevel sets the runlevel for DAG-based ordering. Lower runlevels
// are processed first. Planned -- will be enforced by DAG-aware ordering
// in a follow-up; currently stored but not used for iteration order.
func WithRunlevel(level int) RegistrationOption {
	return func(e *registryEntry) {
		e.runlevel = level
	}
}

// WithDependencies declares module names this module depends on. The
// registry ensures dependencies are processed before dependents.
// Planned -- will be enforced by DAG-aware ordering in a follow-up;
// currently stored but not used for iteration order.
func WithDependencies(deps ...string) RegistrationOption {
	return func(e *registryEntry) {
		e.dependencies = deps
	}
}

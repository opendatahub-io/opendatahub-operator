package modules

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
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

	// IsEnabled returns whether the module should be deployed based on DSC config.
	IsEnabled(dsc *dscv2.DataScienceCluster) bool

	// GetGVK returns the GroupVersionKind of the module CR that this handler
	// manages. Used for dynamic watch registration so module CR status changes
	// requeue the DSC controller.
	GetGVK() schema.GroupVersionKind

	// GetOperatorManifests returns the manifest descriptors for deploying this
	// module's operator resources (Deployment, RBAC, CRD). Handlers return
	// either HelmCharts or Manifests (or both). The returned entries are
	// appended to rr.HelmCharts and rr.Manifests respectively, then rendered
	// by the standard action pipeline (helm/kustomize render + deploy).
	GetOperatorManifests() OperatorManifests

	// BuildModuleCR constructs the module CR as an unstructured object with
	// platform fields projected from PlatformContext. The returned object is
	// added to rr.Resources and applied by deploy.NewAction alongside operator
	// resources. This is the single isolation point for the platform-to-module-CR
	// field mapping.
	BuildModuleCR(ctx context.Context, cli client.Client, platform *PlatformContext) (*unstructured.Unstructured, error)

	// GetModuleStatus reads the current status from the deployed module CR
	// for aggregation into the DSC ModulesReady condition. The returned
	// ModuleStatus includes conditions and generation metadata for staleness
	// detection.
	GetModuleStatus(ctx context.Context, cli client.Client) (*ModuleStatus, error)
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
	DSC *dscv2.DataScienceCluster
}

// RegistrationOption configures optional orchestration metadata when adding
// a module to the registry.
type RegistrationOption func(*registryEntry)

// WithRunlevel sets the runlevel for DAG-based ordering (Step 2 Track A).
// Not enforced by the current implementation.
func WithRunlevel(level int) RegistrationOption {
	return func(e *registryEntry) {
		e.runlevel = level
	}
}

// WithDependencies declares module names this module depends on.
// Not enforced by the current implementation.
func WithDependencies(deps ...string) RegistrationOption {
	return func(e *registryEntry) {
		e.dependencies = deps
	}
}

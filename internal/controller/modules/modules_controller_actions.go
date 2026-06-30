package modules

import (
	"context"
	"fmt"
	"strings"

	"github.com/operator-framework/api/pkg/lib/version"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/flags"
)

func checkUpgradeGates(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	reg := DefaultRegistry()
	if !reg.HasEntries() {
		return nil
	}

	platformCtx, err := buildPlatformContext(ctx, rr)
	if err != nil {
		return err
	}

	if !reg.AnyEnabled(platformCtx) {
		return nil
	}

	return provision.CheckUpgradeGates(ctx, rr.Client, common.Release{
		Name:    rr.Release.Name,
		Version: version.OperatorVersion{Version: rr.Release.Version},
	}, rr.Conditions, rr.GateEntries)
}

// initializeModules fetches DSCI once per reconcile and stores it on the
// ReconciliationRequest so downstream actions can build PlatformContext
// without redundant API calls.
//
// In platform mode (xKS), DSCI is suppressed via flags; the fetch is
// skipped entirely and rr.DSCI remains nil.
func initializeModules(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	if !flags.IsDSCIEnabled() {
		return nil
	}

	dsci, err := cluster.GetDSCI(ctx, rr.Client)
	if err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			odhtype.SetDSCI(rr, nil)
			return nil
		}
		return fmt.Errorf("failed to get DSCI for module reconciler: %w", err)
	}

	odhtype.SetDSCI(rr, dsci)

	return nil
}

// dscFromInstance safely extracts the DataScienceCluster from the reconcile
// instance. Returns nil when the primary resource is not a DSC (standalone mode).
func dscFromInstance(rr *odhtype.ReconciliationRequest) *dscv2.DataScienceCluster {
	if dsc, ok := rr.Instance.(*dscv2.DataScienceCluster); ok {
		return dsc
	}
	return nil
}

// platformFromInstance safely extracts the Platform CR from the reconcile
// instance. Returns nil when the primary resource is not a Platform (DSC mode).
func platformFromInstance(rr *odhtype.ReconciliationRequest) *configv1alpha1.Platform {
	if p, ok := rr.Instance.(*configv1alpha1.Platform); ok {
		return p
	}
	return nil
}

// dsciOrNil returns the DSCI from the reconcile request, or nil if absent.
func dsciOrNil(rr *odhtype.ReconciliationRequest) *dsciv2.DSCInitialization {
	return odhtype.GetDSCI(rr)
}

// enableModulesFromPlatform reads spec.modules from the Platform CR and
// enables only those modules in the registry. This action is only used in
// platform mode (xKS); DSC mode derives enablement from the DSC spec.
//
// Safety: this mutates the package-level registry. It is safe because the
// controller uses the default MaxConcurrentReconciles=1, so only one
// reconcile is in-flight at a time. Do not increase concurrency without
// adding synchronization to the registry.
func enableModulesFromPlatform(_ context.Context, rr *odhtype.ReconciliationRequest) error {
	p := platformFromInstance(rr)
	if p == nil {
		return nil
	}

	EnableFromList(p.Spec.Modules.EnabledModules())

	return nil
}

// buildPlatformContext constructs a PlatformContext for the current reconcile
// cycle. Works in both DSC and standalone modes.
func buildPlatformContext(ctx context.Context, rr *odhtype.ReconciliationRequest) (*PlatformContext, error) {
	appNS, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve application namespace: %w", err)
	}

	return &PlatformContext{
		ApplicationsNamespace: appNS,
		Release: common.Release{
			Name:    rr.Release.Name,
			Version: version.OperatorVersion{Version: rr.Release.Version},
		},
		DSC:               dscFromInstance(rr),
		DSCI:              dsciOrNil(rr),
		Platform:          platformFromInstance(rr),
		ChartsBasePath:    rr.ChartsBasePath,
		ManifestsBasePath: rr.ManifestsBasePath,
	}, nil
}

// cleanupDisabledModules implements a two-phase cleanup for modules that have
// been disabled (either by the user setting Removed or by CLI suppression).
// Cleanup iterates in reverse unified DAG order (higher runlevels first).
//
// Phase 1: The module CR still exists on the cluster. We explicitly delete it
// and keep the module operator Deployment running so it can process any
// finalizer on the CR. A requeue is requested so Phase 2 runs after the
// operator has finished cleanup.
//
// Phase 2: The module CR is confirmed gone. It is now safe to delete the
// module operator's Deployment, RBAC, and other chart resources.
func cleanupDisabledModules(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	reg := DefaultRegistry()
	if !reg.HasEntries() {
		return nil
	}

	log := logf.FromContext(ctx)

	platformCtx, err := buildPlatformContext(ctx, rr)
	if err != nil {
		return err
	}

	cleanupOne := func(handler ModuleHandler) error {
		if handler.IsEnabled(platformCtx) && reg.IsEnabled(handler.GetName()) {
			return nil
		}

		crState, err := handler.GetModuleCRState(ctx, rr.Client)
		if err != nil {
			return err
		}

		switch crState {
		case CRStateAbsent:
			log.Info("module CR gone, cleaning up operator resources", "module", handler.GetName())
			return handler.DeleteOperatorResources(ctx, rr.Client, platformCtx)

		case CRStateAlive:
			log.Info("module disabled, deleting module CR", "module", handler.GetName())
			if err := handler.DeleteModuleCR(ctx, rr.Client); err != nil {
				return err
			}
			fallthrough

		case CRStateDeleting:
			log.Info("module CR deletion in progress, keeping operator alive",
				"module", handler.GetName())

			operatorManifests := handler.GetOperatorManifests(platformCtx)
			if len(operatorManifests.HelmCharts) > 0 {
				rr.HelmCharts = append(rr.HelmCharts, operatorManifests.HelmCharts...)
			}
			if len(operatorManifests.Manifests) > 0 {
				rr.Manifests = append(rr.Manifests, operatorManifests.Manifests...)
			}

			return nil
		}

		return nil
	}

	reverseBatches, err := provision.DefaultRegistry().ReverseBatches()
	if err != nil {
		logf.FromContext(ctx).Error(err, "DAG reverse resolution failed, falling back to alphabetical cleanup order")
		return reg.ForAll(func(handler ModuleHandler, _ bool) error {
			return cleanupOne(handler)
		})
	}

	for _, batch := range reverseBatches {
		for _, entry := range provision.ModulesInBatch(batch) {
			handler := reg.Lookup(entry.GetName())
			if handler == nil {
				continue
			}
			if err := cleanupOne(handler); err != nil {
				return err
			}
		}
	}

	return nil
}

// provisionModules iterates over the unified DAG batches (which contain
// both components and modules) but only provisions entries of KindModule.
// Readiness gating uses a CompositeChecker that spans both registries, so
// a component that hasn't reached Ready blocks advancement to the next
// runlevel just like a module would.
func provisionModules(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	reg := DefaultRegistry()
	if !reg.HasEntries() {
		return nil
	}

	platformCtx, err := buildPlatformContext(ctx, rr)
	if err != nil {
		return err
	}

	gatewayDomain, err := resources.GetGatewayDomain(ctx, rr.Client)
	if err != nil {
		log.V(1).Info("gateway domain not available, modules needing it should handle empty value", "error", err)
	}
	platformCtx.GatewayDomain = gatewayDomain

	dsc := dscFromInstance(rr)

	checker := provision.NewCompositeChecker(
		cr.NewReadinessChecker(cr.DefaultRegistry(), rr.Client, dsc),
		NewReadinessChecker(reg, rr.Client, rr.Release.Version.String(),
			WithPlatformContext(platformCtx)),
	)

	var perModuleImages []odhtype.ModuleImages
	var failedModules []string

	// The DSC controller is the sole owner of the ProvisioningProgress
	// condition. Pass a no-op writer so the modules controller still
	// participates in DAG gating but doesn't clobber the DSC
	// controller's condition via the +listType=atomic SSA race.
	requeueAfter, walkErr := provision.WalkBatches(ctx, checker, moduleStuckTracker, string(rr.Instance.GetUID()), provision.NoOpConditionWriter{},
		func(batch []provision.UnifiedNode) error {
			for _, entry := range provision.ModulesInBatch(batch) {
				handler := reg.Lookup(entry.GetName())
				if handler == nil {
					continue
				}
				name := handler.GetName()

				if !handler.IsEnabled(platformCtx) {
					continue
				}

				log.Info("provisioning module", "module", name,
					"runlevel", entry.GetRunlevel())

				operatorManifests := handler.GetOperatorManifests(platformCtx)

				moduleCR, err := handler.BuildModuleCR(ctx, rr.Client, platformCtx)
				if err != nil {
					log.Error(err, "BuildModuleCR failed (programming error)", "module", name)
					failedModules = append(failedModules, name)
					continue
				}
				if moduleCR == nil {
					log.Error(nil, "BuildModuleCR returned nil without error (programming error)", "module", name)
					failedModules = append(failedModules, name)
					continue
				}

				perModuleImages = append(perModuleImages, odhtype.ModuleImages{
					DeploymentName:    deploymentNameFor(handler, operatorManifests),
					ContainerName:     containerNameFor(handler),
					ControllerImage:   controllerImageFor(handler),
					InitContainerName: initContainerNameFor(handler),
					Images:            handler.GetRelatedImages(),
				})
				if len(operatorManifests.HelmCharts) > 0 {
					rr.HelmCharts = append(rr.HelmCharts, operatorManifests.HelmCharts...)
				}
				if len(operatorManifests.Manifests) > 0 {
					rr.Manifests = append(rr.Manifests, operatorManifests.Manifests...)
				}

				rr.Resources = append(rr.Resources, *moduleCR)
			}
			return nil
		},
	)

	if walkErr != nil {
		return walkErr
	}

	if requeueAfter > 0 {
		return odherrors.NewRequeueAfterError(requeueAfter)
	}

	if len(failedModules) > 0 {
		if !cr.HasEntries() {
			rr.Conditions.SetCondition(common.Condition{
				Type:    status.ConditionTypeModulesReady,
				Status:  metav1.ConditionFalse,
				Reason:  status.ProvisioningFailedReason,
				Message: fmt.Sprintf("Provisioning failed for: %s", strings.Join(failedModules, ", ")),
			})
		}

		return fmt.Errorf("BuildModuleCR failed for modules: %s", strings.Join(failedModules, ", "))
	}

	if len(perModuleImages) > 0 || platformCtx.ApplicationsNamespace != "" {
		odhtype.SetModuleEnvInjection(rr, &odhtype.ModuleEnvInjection{
			PerModuleImages:       perModuleImages,
			ApplicationsNamespace: platformCtx.ApplicationsNamespace,
		})
	}

	return nil
}

var moduleStuckTracker = dag.NewStuckTracker()

const defaultContainerName = "manager"

func containerNameFor(h ModuleHandler) string {
	if cn, ok := h.(ContainerNamer); ok {
		return cn.GetContainerName()
	}
	return defaultContainerName
}

// deploymentNameFor resolves the Deployment name targeted for RELATED_IMAGE_*
// env injection. An explicit Config.DeploymentName (via DeploymentNamer) wins;
// otherwise it falls back to the manifest-derived name. This matters for
// kustomize modules whose rendered Deployment name (after namePrefix) differs
// from the module name.
func deploymentNameFor(h ModuleHandler, manifests OperatorManifests) string {
	if dn, ok := h.(DeploymentNamer); ok {
		if name := dn.GetDeploymentName(); name != "" {
			return name
		}
	}
	return deploymentNameFromManifests(manifests, h.GetName())
}

func controllerImageFor(h ModuleHandler) string {
	if ci, ok := h.(ControllerImager); ok {
		return ci.GetControllerImage()
	}
	return ""
}

func initContainerNameFor(h ModuleHandler) string {
	if icn, ok := h.(InitContainerNamer); ok {
		return icn.GetInitContainerName()
	}
	return ""
}

// deploymentNameFromManifests returns the expected Deployment name for a module
// based on its manifests. For Helm-based modules this is the release name; for
// Kustomize modules it falls back to the provided fallbackName.
func deploymentNameFromManifests(manifests OperatorManifests, fallbackName string) string {
	for _, chart := range manifests.HelmCharts {
		if chart.ReleaseName != "" {
			return chart.ReleaseName
		}
	}
	return fallbackName
}

// ComputeModulesStatus reads status conditions from each enabled module's CR
// and sets the aggregate ModulesReady condition on rr.Conditions.
//
// During the transition period (in-tree components exist), the DSC
// controller calls this and is the sole status writer — the modules
// controller has WithoutStatusConditions so its conditions are never
// applied. Post-migration (no in-tree components), the modules
// controller calls this via updateModuleStatus and becomes the sole
// status writer.
func ComputeModulesStatus(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	reg := DefaultRegistry()
	if !reg.HasEntries() {
		return nil
	}

	platformCtx, err := buildPlatformContext(ctx, rr)
	if err != nil {
		return err
	}

	var notReadyModules []string
	var degradedModules []string
	var enabledCount int

	err = reg.ForEach(func(handler ModuleHandler) error {
		name := handler.GetName()

		if !handler.IsEnabled(platformCtx) {
			return nil
		}

		enabledCount++

		moduleStatus, err := handler.GetModuleStatus(ctx, rr.Client)
		if err != nil {
			log.V(1).Info("failed to get module status", "module", name, "error", err)
			notReadyModules = append(notReadyModules, name)
			return nil
		}

		if moduleStatus.ObservedGeneration > 0 && moduleStatus.ObservedGeneration < moduleStatus.Generation {
			log.V(1).Info("module status is stale",
				"module", name,
				"observedGeneration", moduleStatus.ObservedGeneration,
				"generation", moduleStatus.Generation,
			)
			notReadyModules = append(notReadyModules, name+" (stale)")
			return nil
		}

		ready := false
		degraded := false

		for _, c := range moduleStatus.Conditions {
			switch c.Type {
			case status.ConditionTypeReady:
				ready = c.Status == metav1.ConditionTrue
			case status.ConditionTypeDegraded:
				degraded = c.Status == metav1.ConditionTrue
			}
		}

		if !ready {
			notReadyModules = append(notReadyModules, name)
		} else if degraded {
			degradedModules = append(degradedModules, name)
		}

		return nil
	})

	if err != nil {
		return err
	}

	switch {
	case len(notReadyModules) > 0:
		msg := fmt.Sprintf("Some modules are not ready: %s", strings.Join(notReadyModules, ", "))
		if len(degradedModules) > 0 {
			msg += fmt.Sprintf("; degraded: %s", strings.Join(degradedModules, ", "))
		}
		rr.Conditions.SetCondition(common.Condition{
			Type:    status.ConditionTypeModulesReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.NotReadyReason,
			Message: msg,
		})
	case len(degradedModules) > 0:
		rr.Conditions.SetCondition(common.Condition{
			Type:    status.ConditionTypeModulesReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.ConditionTypeDegraded,
			Message: fmt.Sprintf("Some modules are degraded: %s", strings.Join(degradedModules, ", ")),
		})
	case enabledCount == 0:
		rr.Conditions.SetCondition(common.Condition{
			Type:     status.ConditionTypeModulesReady,
			Status:   metav1.ConditionTrue,
			Severity: common.ConditionSeverityInfo,
			Reason:   status.NoManagedModulesReason,
			Message:  "All registered modules have ManagementState Removed or are not configured",
		})
	default:
		rr.Conditions.MarkTrue(status.ConditionTypeModulesReady)
	}

	return nil
}

// updateModuleStatus writes ModulesReady only when no in-tree components
// are registered, meaning the DSC controller is absent and the modules
// controller is the sole status writer. While components exist, the DSC
// controller handles ModulesReady.
func updateModuleStatus(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	if cr.HasEntries() {
		return nil
	}
	return ComputeModulesStatus(ctx, rr)
}

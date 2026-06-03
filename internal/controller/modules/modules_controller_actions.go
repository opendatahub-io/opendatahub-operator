package modules

import (
	"context"
	"fmt"
	"strings"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/flags"
)

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
			rr.DSCI = nil
			return nil
		}
		return fmt.Errorf("failed to get DSCI for module reconciler: %w", err)
	}

	rr.DSCI = dsci

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
	return rr.DSCI
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
		Release:               rr.Release,
		DSC:                   dscFromInstance(rr),
		DSCI:                  dsciOrNil(rr),
		Platform:              platformFromInstance(rr),
		ChartsBasePath:        rr.ChartsBasePath,
	}, nil
}

// cleanupDisabledModules implements a two-phase cleanup for modules that have
// been disabled (either by the user setting Removed or by CLI suppression).
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

	return reg.ForAll(func(handler ModuleHandler, registryEnabled bool) error {
		if registryEnabled && handler.IsEnabled(platformCtx) {
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
	})
}

// provisionModules iterates over enabled modules in the registry and for each:
//   - Appends the module's operator manifests to rr.HelmCharts and/or
//     rr.Manifests (rendered by helm/kustomize actions, applied by deploy.NewAction).
//   - Builds the module CR and adds it to rr.Resources (applied by
//     deploy.NewAction alongside operator resources).
//
// BuildModuleCR is a pure projection of platform data into the module CR.
// It must never fail in production. If any module fails, the pipeline is
// stopped before deploy/GC to prevent partial desired state from causing
// GC to delete healthy resources.
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

	var perModuleImages []odhtype.ModuleImages
	var failedModules []string

	reg.ForEachEnabled(func(handler ModuleHandler) {
		name := handler.GetName()

		if !handler.IsEnabled(platformCtx) {
			return
		}

		log.Info("provisioning module", "module", name)

		operatorManifests := handler.GetOperatorManifests(platformCtx)

		moduleCR, err := handler.BuildModuleCR(ctx, rr.Client, platformCtx)
		if err != nil {
			log.Error(err, "BuildModuleCR failed (programming error)", "module", name)
			failedModules = append(failedModules, name)
			return
		}
		if moduleCR == nil {
			log.Error(nil, "BuildModuleCR returned nil without error (programming error)", "module", name)
			failedModules = append(failedModules, name)
			return
		}

		perModuleImages = append(perModuleImages, odhtype.ModuleImages{
			DeploymentName: deploymentNameFromManifests(operatorManifests, handler.GetName()),
			ContainerName:  containerNameFor(handler),
			Images:         handler.GetRelatedImages(),
		})
		if len(operatorManifests.HelmCharts) > 0 {
			rr.HelmCharts = append(rr.HelmCharts, operatorManifests.HelmCharts...)
		}
		if len(operatorManifests.Manifests) > 0 {
			rr.Manifests = append(rr.Manifests, operatorManifests.Manifests...)
		}

		rr.Resources = append(rr.Resources, *moduleCR)
	})

	if len(failedModules) > 0 {
		rr.Conditions.SetCondition(common.Condition{
			Type:    status.ConditionTypeModulesReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.ProvisioningFailedReason,
			Message: fmt.Sprintf("Provisioning failed for: %s", strings.Join(failedModules, ", ")),
		})

		return fmt.Errorf("BuildModuleCR failed for modules: %s", strings.Join(failedModules, ", "))
	}

	if len(perModuleImages) > 0 || platformCtx.ApplicationsNamespace != "" {
		rr.ModuleEnvInjection = &odhtype.ModuleEnvInjection{
			PerModuleImages:       perModuleImages,
			ApplicationsNamespace: platformCtx.ApplicationsNamespace,
		}
	}

	return nil
}

const defaultContainerName = "manager"

func containerNameFor(h ModuleHandler) string {
	if cn, ok := h.(ContainerNamer); ok {
		return cn.GetContainerName()
	}
	return defaultContainerName
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

// updateModuleStatus reads status conditions from each enabled module's CR
// and maps them to the DSC status. Module conditions contribute to the
// aggregate ModulesReady condition.
//
// During the transition period where both component and module controllers
// write to DSC status, ModulesReady is not set because the DSC CRD uses
// +listType=atomic on status.conditions. With atomic lists, SSA replaces
// the entire list on each write, so two controllers race and the last
// writer's conditions overwrite the other's. Once all components have
// migrated to modules and the DSC controller is removed, the modules
// controller becomes the sole status writer and ModulesReady can be
// enabled.
func updateModuleStatus(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
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

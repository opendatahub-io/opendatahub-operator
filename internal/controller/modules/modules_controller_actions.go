package modules

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
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

	return provision.CheckUpgradeGates(ctx, rr.Client, rr.Release, rr.Conditions, rr.GateEntries)
}

// platformFromInstance extracts the Platform CR from the reconcile instance.
func platformFromInstance(rr *odhtype.ReconciliationRequest) *configv1alpha1.Platform {
	if p, ok := rr.Instance.(*configv1alpha1.Platform); ok {
		return p
	}
	return nil
}

// enableModulesFromPlatform reads spec.modules from the Platform CR and
// enables only those modules in the registry.
//
// Safety: this mutates the package-level registry. It is safe because the
// controller uses the default MaxConcurrentReconciles=1, so only one
// reconcile is in-flight at a time.
func enableModulesFromPlatform(_ context.Context, rr *odhtype.ReconciliationRequest) error {
	p := platformFromInstance(rr)
	if p == nil {
		return nil
	}

	EnableFromList(p.Spec.Modules.EnabledModules())

	return nil
}

// buildPlatformContext constructs a PlatformContext for the current reconcile
// cycle. Always reads from Platform CR.
func buildPlatformContext(ctx context.Context, rr *odhtype.ReconciliationRequest) (*PlatformContext, error) {
	appNS, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve application namespace: %w", err)
	}

	// Monitoring namespace  read directly from DSCI or set to empty when no DSCI (xKS).
	var monitoringNS string
	if rr.DSCI != nil {
		monitoringNS = rr.DSCI.Spec.Monitoring.Namespace
	}

	return &PlatformContext{
		ApplicationsNamespace: appNS,
		MonitoringNamespace:   monitoringNS,
		Release:               rr.Release,
		Platform:              platformFromInstance(rr),
		ChartsBasePath:        rr.ChartsBasePath,
		ManifestsBasePath:     rr.ManifestsBasePath,
	}, nil
}

// cleanupDisabledModules handles operator resource cleanup for disabled modules.
// CR deletion is handled by DSC/DSCI controllers (they own the module CR lifecycle).
// This action only manages operator resources:
//   - CR still deleting (finalizers in progress): keep operator alive so it can
//     process finalizers
//   - CR gone: delete operator Deployment, RBAC, and chart resources
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

		appendOperatorManifests := func() {
			operatorManifests := handler.GetOperatorManifests(platformCtx)
			if len(operatorManifests.HelmCharts) > 0 {
				rr.HelmCharts = append(rr.HelmCharts, operatorManifests.HelmCharts...)
			}
			if len(operatorManifests.Manifests) > 0 {
				rr.Manifests = append(rr.Manifests, operatorManifests.Manifests...)
			}
		}

		switch crState {
		case CRStateAbsent:
			log.Info("module CR gone, cleaning up operator resources", "module", handler.GetName())
			return handler.DeleteOperatorResources(ctx, rr.Client, platformCtx)

		case CRStateAlive:
			log.Info("module disabled but CR still exists", "module", handler.GetName())
			appendOperatorManifests()

		case CRStateDeleting:
			log.Info("module CR deleting, keeping operator alive for finalizers", "module", handler.GetName())
			appendOperatorManifests()
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

	checker := provision.NewCompositeChecker(
		cr.NewReadinessChecker(cr.DefaultRegistry(), rr.Client, rr.Release.Version.String()),
		NewReadinessChecker(reg, rr.Client, rr.Release.Version.String(),
			WithPlatformContext(platformCtx)),
	)

	var perModuleImages []odhtype.ModuleImages

	requeueAfter, walkErr := provision.WalkBatches(ctx, checker, moduleStuckTracker, string(rr.Instance.GetUID()), rr.Release.Version.String(), rr.Conditions,
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

				log.Info("provisioning module operator", "module", name,
					"runlevel", entry.GetRunlevel())

				operatorManifests := handler.GetOperatorManifests(platformCtx)

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
			}
			return nil
		},
	)

	if walkErr != nil {
		return walkErr
	}

	if len(perModuleImages) > 0 || platformCtx.ApplicationsNamespace != "" || platformCtx.MonitoringNamespace != "" {
		rr.ModuleEnvInjection = &odhtype.ModuleEnvInjection{
			PerModuleImages:       perModuleImages,
			ApplicationsNamespace: platformCtx.ApplicationsNamespace,
			MonitoringNamespace:   platformCtx.MonitoringNamespace,
			PlatformType:          platformCtx.Release.Name,
		}
	}

	if requeueAfter > 0 {
		return odherrors.NewRequeueAfterError(requeueAfter)
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

func readyConditionTypeFor(h ModuleHandler) string {
	if rct, ok := h.(ReadyConditionTyper); ok {
		return rct.GetReadyConditionType()
	}
	return h.GetGVK().Kind + status.ReadySuffix
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

type perModuleResult struct {
	condition common.Condition
	ready     bool
	degraded  bool
}

type modulesEvaluation struct {
	perModule      []perModuleResult
	notReady       []string
	degraded       []string
	pendingCleanup []string
	enabledCount   int
}

// evaluateModulesStatus reads module CRs, checks staleness and readiness,
// and returns structured results without writing any conditions.
func evaluateModulesStatus(ctx context.Context, rr *odhtype.ReconciliationRequest) (*modulesEvaluation, error) {
	log := logf.FromContext(ctx)

	reg := DefaultRegistry()
	if !reg.HasEntries() {
		return &modulesEvaluation{}, nil
	}

	platformCtx, err := buildPlatformContext(ctx, rr)
	if err != nil {
		return nil, err
	}

	eval := &modulesEvaluation{}

	err = reg.ForAll(func(handler ModuleHandler, _ bool) error {
		name := handler.GetName()
		condType := readyConditionTypeFor(handler)

		if !handler.IsEnabled(platformCtx) {
			eval.perModule = append(eval.perModule, perModuleResult{
				condition: common.Condition{
					Type:     condType,
					Status:   metav1.ConditionFalse,
					Reason:   status.RemovedReason,
					Severity: common.ConditionSeverityInfo,
					Message:  fmt.Sprintf("Module ManagementState is set to %s", status.RemovedReason),
				},
			})

			if crState, err := handler.GetModuleCRState(ctx, rr.Client); err == nil && crState != CRStateAbsent {
				eval.pendingCleanup = append(eval.pendingCleanup, name)
			}

			return nil
		}

		eval.enabledCount++

		moduleStatus, err := handler.GetModuleStatus(ctx, rr.Client)
		if err != nil {
			log.V(1).Info("failed to get module status", "module", name, "error", err)
			eval.notReady = append(eval.notReady, name)
			eval.perModule = append(eval.perModule, perModuleResult{
				condition: common.Condition{
					Type:    condType,
					Status:  metav1.ConditionFalse,
					Reason:  status.NotReadyReason,
					Message: fmt.Sprintf("Failed to get module status: %v", err),
				},
			})
			return nil
		}

		if moduleStatus.ObservedGeneration > 0 && moduleStatus.ObservedGeneration < moduleStatus.Generation {
			log.V(1).Info("module status is stale",
				"module", name,
				"observedGeneration", moduleStatus.ObservedGeneration,
				"generation", moduleStatus.Generation,
			)
			eval.notReady = append(eval.notReady, name+" (stale)")
			eval.perModule = append(eval.perModule, perModuleResult{
				condition: common.Condition{
					Type:    condType,
					Status:  metav1.ConditionFalse,
					Reason:  status.NotReadyReason,
					Message: "Module status is stale (observedGeneration < generation)",
				},
			})
			return nil
		}

		ready := false
		degraded := false
		var readyCond *metav1.Condition

		for i := range moduleStatus.Conditions {
			switch moduleStatus.Conditions[i].Type {
			case status.ConditionTypeReady:
				ready = moduleStatus.Conditions[i].Status == metav1.ConditionTrue
				readyCond = &moduleStatus.Conditions[i]
			case status.ConditionTypeDegraded:
				degraded = moduleStatus.Conditions[i].Status == metav1.ConditionTrue
			}
		}

		result := perModuleResult{ready: ready, degraded: degraded}
		if readyCond != nil {
			result.condition = common.Condition{
				Type:    condType,
				Status:  readyCond.Status,
				Reason:  readyCond.Reason,
				Message: readyCond.Message,
			}
		} else {
			result.condition = common.Condition{
				Type:    condType,
				Status:  metav1.ConditionFalse,
				Reason:  status.NotReadyReason,
				Message: "Module has not reported a Ready condition yet",
			}
		}

		eval.perModule = append(eval.perModule, result)

		if !ready {
			eval.notReady = append(eval.notReady, name)
		} else if degraded {
			eval.degraded = append(eval.degraded, name)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return eval, nil
}

// writeAggregateCondition writes the ModulesReady aggregate condition
// based on the evaluation results.
func (e *modulesEvaluation) writeAggregateCondition(conditions *conditions.Manager) {
	cleanupSuffix := ""
	if len(e.pendingCleanup) > 0 {
		cleanupSuffix = fmt.Sprintf("; pending deletion: %s", strings.Join(e.pendingCleanup, ", "))
	}

	switch {
	case len(e.notReady) > 0:
		msg := fmt.Sprintf("Some modules are not ready: %s", strings.Join(e.notReady, ", "))
		if len(e.degraded) > 0 {
			msg += fmt.Sprintf("; degraded: %s", strings.Join(e.degraded, ", "))
		}
		conditions.SetCondition(common.Condition{
			Type:    status.ConditionTypeModulesReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.NotReadyReason,
			Message: msg + cleanupSuffix,
		})
	case len(e.degraded) > 0:
		conditions.SetCondition(common.Condition{
			Type:    status.ConditionTypeModulesReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.ConditionTypeDegraded,
			Message: fmt.Sprintf("Some modules are degraded: %s", strings.Join(e.degraded, ", ")) + cleanupSuffix,
		})
	case len(e.pendingCleanup) > 0:
		conditions.SetCondition(common.Condition{
			Type:     status.ConditionTypeModulesReady,
			Status:   metav1.ConditionTrue,
			Severity: common.ConditionSeverityInfo,
			Reason:   status.RemovedReason,
			Message:  fmt.Sprintf("Modules pending deletion: %s", strings.Join(e.pendingCleanup, ", ")),
		})
	case e.enabledCount == 0:
		conditions.SetCondition(common.Condition{
			Type:     status.ConditionTypeModulesReady,
			Status:   metav1.ConditionTrue,
			Severity: common.ConditionSeverityInfo,
			Reason:   status.NoManagedModulesReason,
			Message:  "All registered modules have ManagementState Removed or are not configured",
		})
	default:
		conditions.MarkTrue(status.ConditionTypeModulesReady)
	}
}

// ComputeModulesStatusDetailed writes per-module conditions and the
// aggregate ModulesReady condition. Called by the DSC controller for
// full module status on the DSC CR.
func ComputeModulesStatusDetailed(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	eval, err := evaluateModulesStatus(ctx, rr)
	if err != nil {
		return err
	}

	for _, r := range eval.perModule {
		rr.Conditions.SetCondition(r.condition)
	}

	eval.writeAggregateCondition(rr.Conditions)

	return nil
}

// computeModulesStatusAggregate writes only the aggregate ModulesReady
// condition. Called by the Platform controller — Platform CR status
// reflects DAG orchestration state, not per-module detail.
func computeModulesStatusAggregate(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	eval, err := evaluateModulesStatus(ctx, rr)
	if err != nil {
		return err
	}

	eval.writeAggregateCondition(rr.Conditions)

	return nil
}

// updateModuleStatus writes aggregate ModulesReady to Platform CR status.
func updateModuleStatus(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	return computeModulesStatusAggregate(ctx, rr)
}

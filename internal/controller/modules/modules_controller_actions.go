package modules

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
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

	if !reg.AnyEnabled(platformCtx.Modules) {
		return nil
	}

	return provision.CheckUpgradeGates(ctx, rr.Client, rr.Release, rr.Conditions, rr.GateEntries)
}

// modulesFromInstance derives PlatformModules from whichever CR is the
// reconcile instance — Platform CR (platform controller) or DSC (DSC
// controller).
func modulesFromInstance(rr *odhtype.ReconciliationRequest) (*configv1alpha1.PlatformModules, error) {
	if p, ok := rr.Instance.(*configv1alpha1.Platform); ok {
		return &p.Spec.Modules, nil
	}
	if dsc, ok := rr.Instance.(*dscv2.DataScienceCluster); ok {
		pm := BuildPlatformModules(&DSCContext{DSC: dsc})
		return &pm, nil
	}
	return nil, fmt.Errorf("cannot derive PlatformModules from instance type %T", rr.Instance)
}

// BuildPlatformModules iterates all module handlers to derive their
// management state from DSC/DSCI, producing the PlatformModules struct.
// Empty management states are normalized to Removed so the Platform CR
// always carries valid enum values (prevents CRD validation errors on
// SSA apply where zero-value structs serialize as null).
func BuildPlatformModules(dscCtx *DSCContext) configv1alpha1.PlatformModules {
	var pm configv1alpha1.PlatformModules
	DefaultRegistry().ForAll(func(handler ModuleHandler, _ bool) error { //nolint:errcheck
		handler.PopulatePlatformModule(&pm, dscCtx)
		return nil
	})
	normalizePlatformModules(&pm)
	return pm
}

func normalizePlatformModules(pm *configv1alpha1.PlatformModules) {
	v := reflect.ValueOf(pm).Elem()
	for _, fv := range v.Fields() {
		if fv.Kind() != reflect.Struct {
			continue
		}
		ms := fv.FieldByName("ManagementState")
		if !ms.IsValid() || !ms.CanSet() {
			continue
		}
		if ms.String() == "" {
			ms.SetString(string(operatorv1.Removed))
		}
	}
}

// enableModulesFromPlatform reads spec.modules from the Platform CR and
// enables only those modules in the registry.
//
// Safety: this mutates the package-level registry. It is safe because the
// controller uses the default MaxConcurrentReconciles=1, so only one
// reconcile is in-flight at a time.
func enableModulesFromPlatform(_ context.Context, rr *odhtype.ReconciliationRequest) error {
	modules, err := modulesFromInstance(rr)
	if err != nil {
		return err
	}

	EnableFromList(modules.EnabledModules())

	return nil
}

// buildPlatformContext constructs a PlatformContext for the current reconcile
// cycle. Always reads from Platform CR.
func buildPlatformContext(ctx context.Context, rr *odhtype.ReconciliationRequest) (*PlatformContext, error) {
	appNS, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve application namespace: %w", err)
	}

	monitoringNS, err := cluster.MonitoringNamespace(ctx, rr.Client)
	if err != nil {
		logf.FromContext(ctx).V(1).Info("monitoring namespace not available, skipping MONITORING_NAMESPACE injection", "error", err)
	}

	modules, err := modulesFromInstance(rr)
	if err != nil {
		return nil, err
	}

	return &PlatformContext{
		ApplicationsNamespace: appNS,
		MonitoringNamespace:   monitoringNS,
		Release:               rr.Release,
		Modules:               modules,
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
		if handler.IsEnabled(platformCtx.Modules) && reg.IsEnabled(handler.GetName()) {
			return nil
		}

		crState, err := handler.GetModuleCRState(ctx, rr.Client)
		if err != nil {
			return err
		}

		appendOperatorManifests := func() {
			operatorManifests := handler.GetOperatorManifests(platformCtx)
			appendModuleEnvInjection(rr, platformCtx.ApplicationsNamespace, platformCtx.MonitoringNamespace, platformCtx.Release.Name, moduleImagesFor(handler, operatorManifests))
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

	reverseBatches, err := provision.ReverseBatchesAll()
	if err != nil {
		logf.FromContext(ctx).Error(err, "DAG reverse resolution failed, falling back to alphabetical cleanup order")
		if forAllErr := reg.ForAll(func(handler ModuleHandler, _ bool) error {
			return cleanupOne(handler)
		}); forAllErr != nil {
			return forAllErr
		}
	} else {
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
			WithPlatformModules(platformCtx.Modules)),
	)

	requeueAfter, walkErr := provision.WalkBatches(ctx, checker, moduleStuckTracker, string(rr.Instance.GetUID()), rr.Release.Version.String(), rr.Conditions,
		func(batch []provision.UnifiedNode) error {
			for _, entry := range provision.ModulesInBatch(batch) {
				handler := reg.Lookup(entry.GetName())
				if handler == nil {
					continue
				}
				name := handler.GetName()

				if !handler.IsEnabled(platformCtx.Modules) {
					continue
				}

				log.Info("provisioning module operator", "module", name,
					"runlevel", entry.GetRunlevel())

				operatorManifests := handler.GetOperatorManifests(platformCtx)

				appendModuleEnvInjection(rr, platformCtx.ApplicationsNamespace, platformCtx.MonitoringNamespace, platformCtx.Release.Name, moduleImagesFor(handler, operatorManifests))
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

func extraEnvFor(h ModuleHandler) map[string]string {
	if ep, ok := h.(ExtraEnvProvider); ok {
		return ep.GetExtraEnv()
	}
	return nil
}

func moduleImagesFor(h ModuleHandler, manifests OperatorManifests) odhtype.ModuleImages {
	return odhtype.ModuleImages{
		DeploymentName:    deploymentNameFor(h, manifests),
		ContainerName:     containerNameFor(h),
		ControllerImage:   controllerImageFor(h),
		InitContainerName: initContainerNameFor(h),
		Images:            h.GetRelatedImages(),
		ExtraEnv:          extraEnvFor(h),
	}
}

func appendModuleEnvInjection(
	rr *odhtype.ReconciliationRequest,
	applicationsNamespace, monitoringNamespace string,
	platformType common.Platform,
	moduleImages odhtype.ModuleImages,
) {
	mei := odhtype.GetModuleEnvInjection(rr)
	if mei == nil {
		mei = &odhtype.ModuleEnvInjection{
			ApplicationsNamespace: applicationsNamespace,
			MonitoringNamespace:   monitoringNamespace,
			PlatformType:          platformType,
		}
	} else if mei.ApplicationsNamespace == "" {
		mei.ApplicationsNamespace = applicationsNamespace
	}
	if mei.MonitoringNamespace == "" {
		mei.MonitoringNamespace = monitoringNamespace
	}
	if mei.PlatformType == "" {
		mei.PlatformType = platformType
	}

	mei.PerModuleImages = append(mei.PerModuleImages, moduleImages)
	odhtype.SetModuleEnvInjection(rr, mei)
}

// deploymentNameFor returns the expected Deployment name for a module.
// Prefer an explicit handler override, otherwise use the Helm release name,
// and finally fall back to the module name for manifest-based modules.
func deploymentNameFor(h ModuleHandler, manifests OperatorManifests) string {
	if dn, ok := h.(DeploymentNamer); ok {
		if deploymentName := dn.GetDeploymentName(); deploymentName != "" {
			return deploymentName
		}
	}
	for _, chart := range manifests.HelmCharts {
		if chart.ReleaseName != "" {
			return chart.ReleaseName
		}
	}
	return h.GetName()
}

func writeDSCLegacyStatusFields(
	ctx context.Context,
	cli client.Client,
	handler ModuleHandler,
	dsc *dscv2.DataScienceCluster,
	enabled bool,
) error {
	if dsc == nil {
		return nil
	}

	writer, ok := handler.(DSCLegacyStatusFieldsWriter)
	if !ok {
		return nil
	}

	return writer.WriteLegacyStatusFields(ctx, cli, dsc, enabled)
}

type perModuleResult struct {
	handler      ModuleHandler
	condition    common.Condition
	enabled      bool
	ready        bool
	degraded     bool
	moduleStatus *ModuleStatus
	submodules   []SubmoduleCondition
}

type modulesEvaluation struct {
	perModule      []perModuleResult
	notReady       []string
	degraded       []string
	crdAbsent      []string
	pendingCleanup []string
	enabledCount   int
}

// evaluateModulesStatus reads module CRs, checks staleness and readiness,
// and returns structured results without writing any conditions.
// The isEnabled callback lets each caller decide how to resolve enablement.
func evaluateModulesStatus(ctx context.Context, rr *odhtype.ReconciliationRequest, isEnabled func(ModuleHandler) bool) (*modulesEvaluation, error) {
	log := logf.FromContext(ctx)

	reg := DefaultRegistry()
	if !reg.HasEntries() {
		return &modulesEvaluation{}, nil
	}

	eval := &modulesEvaluation{}

	err := reg.ForAll(func(handler ModuleHandler, _ bool) error {
		name := handler.GetName()
		condType := readyConditionTypeFor(handler)
		submodules := submoduleConditionsFor(handler)
		enabled := isEnabled(handler)

		if !enabled {
			eval.perModule = append(eval.perModule, perModuleResult{
				handler: handler,
				condition: common.Condition{
					Type:     condType,
					Status:   metav1.ConditionFalse,
					Reason:   status.RemovedReason,
					Severity: common.ConditionSeverityInfo,
					Message:  fmt.Sprintf("Module ManagementState is set to %s", status.RemovedReason),
				},
				submodules: submodules,
			})

			crState, crErr := handler.GetModuleCRState(ctx, rr.Client)
			if crErr != nil {
				log.V(1).Info("failed to get module CR state for disabled module", "module", name, "error", crErr)
				eval.pendingCleanup = append(eval.pendingCleanup, name)
			} else if crState != CRStateAbsent {
				eval.pendingCleanup = append(eval.pendingCleanup, name)
			}

			return nil
		}

		eval.enabledCount++

		moduleStatus, err := handler.GetModuleStatus(ctx, rr.Client)
		if err != nil {
			log.V(1).Info("failed to get module status", "module", name, "error", err)
			eval.notReady = append(eval.notReady, name)

			if meta.IsNoMatchError(err) {
				eval.crdAbsent = append(eval.crdAbsent, name)
			}

			eval.perModule = append(eval.perModule, perModuleResult{
				handler: handler,
				enabled: true,
				condition: common.Condition{
					Type:    condType,
					Status:  metav1.ConditionFalse,
					Reason:  status.NotReadyReason,
					Message: fmt.Sprintf("Failed to get module status: %v", err),
				},
				submodules: submodules,
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
				handler: handler,
				enabled: true,
				condition: common.Condition{
					Type:    condType,
					Status:  metav1.ConditionFalse,
					Reason:  status.NotReadyReason,
					Message: "Module status is stale (observedGeneration < generation)",
				},
				submodules: submodules,
			})

			return nil
		}

		ready := false
		degraded := false
		var readyCond *common.Condition

		for i := range moduleStatus.Conditions {
			switch moduleStatus.Conditions[i].Type {
			case status.ConditionTypeReady:
				ready = moduleStatus.Conditions[i].Status == metav1.ConditionTrue
				readyCond = &moduleStatus.Conditions[i]
			case status.ConditionTypeDegraded:
				degraded = moduleStatus.Conditions[i].Status == metav1.ConditionTrue
			}
		}

		result := perModuleResult{handler: handler, enabled: true, ready: ready, degraded: degraded}

		crState, _ := handler.GetModuleCRState(ctx, rr.Client)
		switch {
		case crState == CRStateDeleting:
			result.condition = common.Condition{
				Type:    condType,
				Status:  metav1.ConditionFalse,
				Reason:  status.DeletingReason,
				Message: status.DeletingMessage,
			}
		case readyCond != nil:
			result.condition = common.Condition{
				Type:    condType,
				Status:  readyCond.Status,
				Reason:  readyCond.Reason,
				Message: readyCond.Message,
			}
		default:
			result.condition = common.Condition{
				Type:    condType,
				Status:  metav1.ConditionFalse,
				Reason:  status.NotReadyReason,
				Message: "Module has not reported a Ready condition yet",
			}
		}

		result.moduleStatus = moduleStatus
		result.submodules = submodules

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

// ComputeModulesStatusDetailed writes per-module conditions, DSC component
// status, submodule mirroring, and the aggregate ModulesReady condition.
// Called by the DSC controller for full module status on the DSC CR.
func ComputeModulesStatusDetailed(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	dsc, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("ComputeModulesStatusDetailed requires DataScienceCluster instance, got %T", rr.Instance)
	}
	dscCtx := &DSCContext{DSC: dsc}
	pm := BuildPlatformModules(dscCtx)

	eval, err := evaluateModulesStatus(ctx, rr, func(h ModuleHandler) bool {
		return h.IsEnabled(&pm)
	})
	if err != nil {
		return err
	}

	for _, r := range eval.perModule {
		rr.Conditions.SetCondition(r.condition)

		if dsc != nil && r.handler != nil {
			var releases []common.ComponentRelease
			if r.moduleStatus != nil {
				releases = r.moduleStatus.Releases
			}
			r.handler.WriteDSCComponentStatus(dsc, r.enabled, releases)
			if err := writeDSCLegacyStatusFields(ctx, rr.Client, r.handler, dsc, r.enabled); err != nil {
				log.V(1).Info("failed to write legacy status fields", "module", r.handler.GetName(), "error", err)
			}
		}

		if len(r.submodules) > 0 {
			if r.moduleStatus != nil {
				mirrorSubmoduleConditions(rr, dscCtx, r.moduleStatus, r.submodules)
			} else {
				setSubmodulesFallback(rr, dscCtx, r.submodules, !r.ready,
					r.condition.Reason, r.condition.Message)
			}
		}
	}

	eval.writeAggregateCondition(rr.Conditions)

	if len(eval.crdAbsent) > 0 {
		log.Info("module CRDs not yet available, requesting requeue",
			"modules", strings.Join(eval.crdAbsent, ", "))
		return odherrors.NewRequeueAfterError(30 * time.Second)
	}

	return nil
}

// computeModulesStatusAggregate writes only the aggregate ModulesReady
// condition. Called by the Platform controller — Platform CR status
// reflects DAG orchestration state, not per-module detail.
func computeModulesStatusAggregate(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	p, ok := rr.Instance.(*configv1alpha1.Platform)
	if !ok {
		return fmt.Errorf("computeModulesStatusAggregate requires Platform instance, got %T", rr.Instance)
	}

	eval, err := evaluateModulesStatus(ctx, rr, func(h ModuleHandler) bool {
		return h.IsEnabled(&p.Spec.Modules)
	})
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

func submoduleConditionsFor(h ModuleHandler) []SubmoduleCondition {
	if scp, ok := h.(SubmoduleConditionProvider); ok {
		return scp.GetSubmoduleConditions()
	}
	return nil
}

// mirrorSubmoduleConditions copies declared submodule conditions from the
// module CR's status onto the DSC conditions, preserving severity. These
// conditions are informational only: module readiness is defined solely by the
// module CR's Ready (and Degraded) condition — which the module already
// aggregates severity-aware internally — so mirrored submodule conditions never
// gate ModulesReady. Disabled submodules get a Removed condition.
func mirrorSubmoduleConditions(
	rr *odhtype.ReconciliationRequest,
	dscCtx *DSCContext,
	moduleStatus *ModuleStatus,
	submodules []SubmoduleCondition,
) {
	if len(submodules) == 0 {
		return
	}

	condByType := make(map[string]*common.Condition, len(moduleStatus.Conditions))
	for i := range moduleStatus.Conditions {
		condByType[moduleStatus.Conditions[i].Type] = &moduleStatus.Conditions[i]
	}

	for _, sm := range submodules {
		subEnabled := sm.IsEnabled == nil || sm.IsEnabled(dscCtx)

		writeSubmoduleComponentStatus(dscCtx, sm, subEnabled)

		if !subEnabled {
			rr.Conditions.SetCondition(common.Condition{
				Type:     sm.DSCConditionType,
				Status:   metav1.ConditionFalse,
				Reason:   status.RemovedReason,
				Severity: common.ConditionSeverityInfo,
				Message:  "Submodule ManagementState is set to Removed",
			})

			continue
		}

		source := condByType[sm.SourceConditionType]
		if source == nil {
			rr.Conditions.SetCondition(common.Condition{
				Type:    sm.DSCConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  status.AwaitingReadinessReason,
				Message: "Submodule is enabled (Managed) but the module operator has not reported its status yet",
			})

			continue
		}

		rr.Conditions.SetCondition(common.Condition{
			Type:     sm.DSCConditionType,
			Status:   source.Status,
			Reason:   source.Reason,
			Message:  source.Message,
			Severity: source.Severity,
		})
	}
}

// setSubmodulesFallback writes submodule conditions and component status when
// the parent module cannot provide real submodule status (disabled, error, or
// stale). When parentDisabled is true all submodules are marked Removed
// regardless of their individual IsEnabled state; otherwise each submodule's
// own enablement is checked.
func setSubmodulesFallback(
	rr *odhtype.ReconciliationRequest,
	dscCtx *DSCContext,
	submodules []SubmoduleCondition,
	parentDisabled bool,
	enabledReason string,
	enabledMessage string,
) {
	for _, sm := range submodules {
		subEnabled := !parentDisabled && (sm.IsEnabled == nil || sm.IsEnabled(dscCtx))
		writeSubmoduleComponentStatus(dscCtx, sm, subEnabled)

		if !subEnabled {
			rr.Conditions.SetCondition(common.Condition{
				Type:     sm.DSCConditionType,
				Status:   metav1.ConditionFalse,
				Reason:   status.RemovedReason,
				Severity: common.ConditionSeverityInfo,
				Message:  "Submodule ManagementState is set to Removed",
			})
		} else {
			rr.Conditions.SetCondition(common.Condition{
				Type:    sm.DSCConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  enabledReason,
				Message: enabledMessage,
			})
		}
	}
}

func writeSubmoduleComponentStatus(dscCtx *DSCContext, sm SubmoduleCondition, enabled bool) {
	if dscCtx == nil {
		return
	}
	setDSCComponentField(dscCtx.DSC, sm.StatusFieldName, enabled, nil)
}

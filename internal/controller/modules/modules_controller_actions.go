package modules

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// initializeModules fetches DSCI and GatewayDomain once per reconcile and
// stores the DSCI on the ReconciliationRequest so downstream actions can
// build PlatformContext without redundant API calls.
func initializeModules(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	dsci, err := cluster.GetDSCI(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("failed to get DSCI for module reconciler: %w", err)
	}

	rr.DSCI = dsci

	return nil
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

	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster", rr.Instance)
	}

	platformCtx := PlatformContext{
		ApplicationsNamespace: rr.DSCI.Spec.ApplicationsNamespace,
		Release:               rr.Release,
		DSC:                   instance,
		DSCI:                  rr.DSCI,
		ChartsBasePath:        rr.ChartsBasePath,
	}

	return reg.ForAll(func(handler ModuleHandler, registryEnabled bool) error {
		if registryEnabled && handler.IsEnabled(&platformCtx) {
			return nil
		}

		crState, err := handler.GetModuleCRState(ctx, rr.Client)
		if err != nil {
			return err
		}

		switch crState {
		case CRStateAbsent:
			log.Info("module CR gone, cleaning up operator resources", "module", handler.GetName())
			return handler.DeleteOperatorResources(ctx, rr.Client, &platformCtx)

		case CRStateAlive:
			log.Info("module disabled, deleting module CR", "module", handler.GetName())
			if err := handler.DeleteModuleCR(ctx, rr.Client); err != nil {
				return err
			}
			fallthrough

		case CRStateDeleting:
			log.Info("module CR deletion in progress, keeping operator alive",
				"module", handler.GetName())

			operatorManifests := handler.GetOperatorManifests(&platformCtx)
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
// Disabled modules are simply skipped. Since their resources were present in a
// previous reconcile, gc.NewAction detects they are missing from rr.Resources
// and removes them from the cluster. This matches provisionComponents.
func provisionModules(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster", rr.Instance)
	}

	reg := DefaultRegistry()
	if !reg.HasEntries() {
		return nil
	}

	rr.Generated = true

	gatewayDomain, err := resources.GetGatewayDomain(ctx, rr.Client)
	if err != nil {
		log.V(1).Info("gateway domain not available, modules needing it should handle empty value", "error", err)
	}

	platformCtx := PlatformContext{
		ApplicationsNamespace: rr.DSCI.Spec.ApplicationsNamespace,
		GatewayDomain:         gatewayDomain,
		Release:               rr.Release,
		DSC:                   instance,
		DSCI:                  rr.DSCI,
		ChartsBasePath:        rr.ChartsBasePath,
	}

	seen := make(map[string]bool)
	var allRelatedImages []string

	err = reg.ForEach(func(handler ModuleHandler) error {
		name := handler.GetName()

		if !handler.IsEnabled(&platformCtx) {
			return nil
		}

		log.Info("provisioning module", "module", name)

		for _, img := range handler.GetRelatedImages() {
			if !seen[img] {
				seen[img] = true
				allRelatedImages = append(allRelatedImages, img)
			}
		}

		operatorManifests := handler.GetOperatorManifests(&platformCtx)
		if len(operatorManifests.HelmCharts) > 0 {
			rr.HelmCharts = append(rr.HelmCharts, operatorManifests.HelmCharts...)
		}
		if len(operatorManifests.Manifests) > 0 {
			rr.Manifests = append(rr.Manifests, operatorManifests.Manifests...)
		}

		moduleCR, err := handler.BuildModuleCR(ctx, rr.Client, &platformCtx)
		if err != nil {
			return fmt.Errorf("failed to build module CR for %s: %w", name, err)
		}

		rr.Resources = append(rr.Resources, *moduleCR)

		return nil
	})

	if err != nil {
		rr.Conditions.SetCondition(common.Condition{
			Type:    status.ConditionTypeModulesReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.ProvisioningFailedReason,
			Message: fmt.Sprintf("Module provisioning failed: %v", err),
		})

		return fmt.Errorf("provisioning failed: %w", err)
	}

	if len(allRelatedImages) > 0 || rr.DSCI.Spec.ApplicationsNamespace != "" {
		rr.ModuleEnvInjection = &odhtype.ModuleEnvInjection{
			RelatedImages:         allRelatedImages,
			ApplicationsNamespace: rr.DSCI.Spec.ApplicationsNamespace,
		}
	}

	return nil
}

// updateModuleStatus reads status conditions from each enabled module's CR
// and maps them to the DSC status. Module conditions contribute to the
// aggregate ModulesReady condition.
func updateModuleStatus(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster", rr.Instance)
	}

	reg := DefaultRegistry()
	if !reg.HasEntries() {
		rr.Conditions.SetCondition(common.Condition{
			Type:     status.ConditionTypeModulesReady,
			Status:   metav1.ConditionTrue,
			Severity: common.ConditionSeverityInfo,
			Reason:   status.NoRegisteredModulesReason,
			Message:  status.NoRegisteredModulesReason,
		})

		return nil
	}

	platformCtx := PlatformContext{
		Release: rr.Release,
		DSC:     instance,
		DSCI:    rr.DSCI,
	}

	var notReadyModules []string
	var degradedModules []string

	err := reg.ForEach(func(handler ModuleHandler) error {
		name := handler.GetName()

		if !handler.IsEnabled(&platformCtx) {
			return nil
		}

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
	default:
		rr.Conditions.MarkTrue(status.ConditionTypeModulesReady)
	}

	return nil
}

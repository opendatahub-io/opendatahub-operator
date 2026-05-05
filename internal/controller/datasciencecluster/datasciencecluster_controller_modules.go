package datasciencecluster

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

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

	reg := modules.DefaultRegistry()
	if !reg.HasEntries() {
		return nil
	}

	rr.Generated = true

	dsci, err := cluster.GetDSCI(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("failed to get DSCI for module provisioning: %w", err)
	}

	gatewayDomain, err := resources.GetGatewayDomain(ctx, rr.Client)
	if err != nil {
		log.V(1).Info("gateway domain not available, modules needing it should handle empty value", "error", err)
	}

	platformCtx := modules.PlatformContext{
		ApplicationsNamespace: dsci.Spec.ApplicationsNamespace,
		GatewayDomain:         gatewayDomain,
		Release:               rr.Release,
		DSC:                   instance,
	}

	return reg.ForEach(func(handler modules.ModuleHandler) error {
		name := handler.GetName()

		if !handler.IsEnabled(instance) {
			return nil
		}

		log.Info("provisioning module", "module", name)

		operatorManifests := handler.GetOperatorManifests()
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
}

// updateModuleStatus reads status conditions from each enabled module's CR
// and maps them to the DSC status. Module conditions contribute to the
// aggregate ComponentsReady condition.
func updateModuleStatus(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster", rr.Instance)
	}

	reg := modules.DefaultRegistry()
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

	var notReadyModules []string
	var degradedModules []string

	err := reg.ForEach(func(handler modules.ModuleHandler) error {
		name := handler.GetName()

		if !handler.IsEnabled(instance) {
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

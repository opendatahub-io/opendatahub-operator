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
)

// provisionModules iterates over enabled modules in the registry and for each:
//   - Appends the module's operator Helm charts to rr.HelmCharts (rendered by
//     helm.NewAction, applied by deploy.NewAction).
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

	return reg.ForEach(func(handler modules.ModuleHandler) error {
		name := handler.GetName()

		if !handler.IsEnabled(instance) {
			return nil
		}

		log.Info("provisioning module", "module", name)

		rr.HelmCharts = append(rr.HelmCharts, handler.GetOperatorCharts()...)

		moduleCR, err := handler.BuildModuleCR(ctx, rr.Client, instance, dsci)
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

	err := reg.ForEach(func(handler modules.ModuleHandler) error {
		name := handler.GetName()

		if !handler.IsEnabled(instance) {
			return nil
		}

		conditions, err := handler.GetModuleStatus(ctx, rr.Client)
		if err != nil {
			log.V(1).Info("failed to get module status", "module", name, "error", err)
			notReadyModules = append(notReadyModules, name)
			return nil
		}

		ready := false
		for _, c := range conditions {
			if c.Type == status.ConditionTypeReady && c.Status == metav1.ConditionTrue {
				ready = true
				break
			}
		}

		if !ready {
			notReadyModules = append(notReadyModules, name)
		}

		return nil
	})

	if err != nil {
		return err
	}

	if len(notReadyModules) > 0 {
		rr.Conditions.SetCondition(common.Condition{
			Type:    status.ConditionTypeModulesReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.NotReadyReason,
			Message: fmt.Sprintf("Some modules are not ready: %s", strings.Join(notReadyModules, ",")),
		})
	} else {
		rr.Conditions.MarkTrue(status.ConditionTypeModulesReady)
	}

	return nil
}

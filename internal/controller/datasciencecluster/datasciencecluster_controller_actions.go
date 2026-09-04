package datasciencecluster

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// dscFieldManager is the SSA field owner used by the DSC deploy and delete
// actions. Platform ownerReferences are merged and updated separately because
// the Platform is shared with the DSCI controller.
const dscFieldManager = "datasciencecluster"

func isNilInterface(v any) bool {
	return v == nil || (reflect.ValueOf(v).Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil())
}

func checkPreConditions(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	if _, err := cluster.GetDSCI(ctx, rr.Client); err != nil {
		return fmt.Errorf("failed to get a valid DataScienceCluster instance, %w", err)
	}

	if _, err := cluster.GetDSC(ctx, rr.Client); err != nil {
		return fmt.Errorf("failed to get a valid DSCInitialization instance, %w", err)
	}

	return nil
}

func watchDataScienceClusters(ctx context.Context, cli client.Client) []reconcile.Request {
	return cluster.WatchDataScienceClusters(ctx, cli)
}

func buildDSCContext(dsc *dscv2.DataScienceCluster) *modules.DSCContext {
	return &modules.DSCContext{DSC: dsc}
}

func syncPlatformCR(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	return syncPlatformCRWithReader(rr.Client)(ctx, rr)
}

func syncPlatformCRWithReader(apiReader client.Reader) func(context.Context, *odhtype.ReconciliationRequest) error {
	return func(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
		instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
		if !ok {
			return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
		}

		platform := modules.NewPlatformCR(buildDSCContext(instance), modules.ConfigFromDSC)
		modules.SetPlatformMetadata(platform, instance, rr.Release, dscFieldManager)
		if err := resources.Apply(ctx, rr.Client, platform, client.FieldOwner(dscFieldManager), client.ForceOwnership); err != nil {
			return fmt.Errorf("failed to apply Platform CR: %w", err)
		}

		if err := modules.EnsurePlatformOwnerReference(ctx, rr.Client, apiReader, instance, rr.Client.Scheme()); err != nil {
			return fmt.Errorf("failed to update Platform owner reference: %w", err)
		}

		return nil
	}
}

// disableDSCModulesOnDelete is the DSC delete finalizer. It SSA-applies
// Removed for ConfigFromDSC modules only so the Platform controller can
// tear those operators down. modules.monitoring is DSCI-owned and is left
// untouched. The apply action chain does not run during deletion, so this
// cannot fight syncPlatformCR.
func disableDSCModulesOnDelete(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	platform := modules.NewPlatformCRRemovedForSource(buildDSCContext(instance), modules.ConfigFromDSC)
	if err := resources.Apply(ctx, rr.Client, platform, client.FieldOwner(dscFieldManager), client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to mark DSC modules Removed on Platform: %w", err)
	}

	return nil
}

func cleanupDisabledComponents(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	log := logf.FromContext(ctx)
	componentReg := cr.DefaultRegistry()

	reverseBatches, err := provision.DefaultRegistry().ReverseBatches()
	if err != nil {
		return fmt.Errorf("DAG reverse resolution failed during component cleanup: %w", err)
	}

	var errs []error

	for _, batch := range reverseBatches {
		for _, entry := range provision.ComponentsInBatch(batch) {
			handler := componentReg.Lookup(entry.GetName())
			if handler == nil {
				continue
			}
			if handler.IsEnabled(instance) {
				continue
			}
			if err := deleteComponentCR(ctx, rr.Client, handler, instance); err != nil {
				log.Error(err, "failed to delete component CR", "component", handler.GetName())
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

func deleteComponentCR(ctx context.Context, cli client.Client, handler cr.ComponentHandler, owner client.Object) error {
	componentGVK := handler.GroupVersionKind()
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(componentGVK)

	if err := cli.List(ctx, list); err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil
		}
		return err
	}

	var errs []error
	for i := range list.Items {
		if !isOwnedBy(&list.Items[i], owner) {
			continue
		}
		if err := client.IgnoreNotFound(cli.Delete(ctx, &list.Items[i])); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func isOwnedBy(obj, owner metav1.Object) bool {
	uid := owner.GetUID()
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == uid {
			return true
		}
	}
	return false
}

func cleanupDisabledModuleCRs(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	moduleReg := modules.DefaultRegistry()
	if !moduleReg.HasEntries() {
		return nil
	}

	dscCtx := buildDSCContext(instance)

	pm := modules.BuildPlatformModulesForSource(dscCtx, modules.ConfigFromDSC)
	enabledModules := make(map[string]bool)
	for _, name := range pm.EnabledModules() {
		enabledModules[name] = true
	}

	dscOwned := make(map[string]struct{})
	_ = moduleReg.ForConfigSource(modules.ConfigFromDSC, func(handler modules.ModuleHandler, _ bool) error {
		dscOwned[handler.GetName()] = struct{}{}
		return nil
	})

	log := logf.FromContext(ctx)
	reverseBatches, err := provision.DefaultRegistry().ReverseBatches()
	if err != nil {
		log.Error(err, "DAG reverse resolution failed, falling back to alphabetical module CR cleanup")
		return moduleReg.ForConfigSource(modules.ConfigFromDSC, func(handler modules.ModuleHandler, _ bool) error {
			if !enabledModules[handler.GetName()] {
				if delErr := handler.DeleteModuleCR(ctx, rr.Client); delErr != nil {
					log.Error(delErr, "DeleteModuleCR failed", "module", handler.GetName())
					return delErr
				}
			}
			return nil
		})
	}

	var errs []error
	for _, batch := range reverseBatches {
		for _, entry := range provision.ModulesInBatch(batch) {
			handler := moduleReg.Lookup(entry.GetName())
			if handler == nil {
				continue
			}
			if _, owned := dscOwned[handler.GetName()]; !owned {
				continue
			}
			if enabledModules[handler.GetName()] {
				continue
			}
			if err := handler.DeleteModuleCR(ctx, rr.Client); err != nil {
				log.Error(err, "DeleteModuleCR failed", "module", handler.GetName())
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

func provisionComponents(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	rr.Generated = true
	log := logf.FromContext(ctx)
	componentReg := cr.DefaultRegistry()
	var failedComponents []string

	if err := componentReg.ForEach(func(handler cr.ComponentHandler) error {
		name := handler.GetName()
		if !handler.IsEnabled(instance) {
			provision.Disable(name)
			return nil
		}
		provision.Enable(name)
		ci, err := handler.NewCRObject(ctx, rr.Client, instance)
		if err != nil {
			log.Error(err, "NewCRObject failed", "component", name)
			failedComponents = append(failedComponents, name)
			return nil
		}
		if isNilInterface(ci) {
			return nil
		}
		obj, ok := ci.(client.Object)
		if !ok {
			log.Error(nil, "component CR does not implement client.Object", "component", name, "type", fmt.Sprintf("%T", ci))
			failedComponents = append(failedComponents, name)
			return nil
		}
		if err := rr.AddResources(obj); err != nil {
			log.Error(err, "AddResources failed", "component", name)
			failedComponents = append(failedComponents, name)
		}
		return nil
	}); err != nil {
		return err
	}

	if len(failedComponents) > 0 {
		rr.Conditions.SetCondition(common.Condition{
			Type:    status.ConditionTypeComponentsReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.ProvisioningFailedReason,
			Message: fmt.Sprintf("Provisioning failed for: %s", strings.Join(failedComponents, ", ")),
		})
		return fmt.Errorf("provisioning failed for components: %s", strings.Join(failedComponents, ", "))
	}

	return nil
}

func provisionModuleCRs(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	moduleReg := modules.DefaultRegistry()
	if !moduleReg.HasEntries() {
		return nil
	}

	dscCtx := buildDSCContext(instance)

	pm := modules.BuildPlatformModulesForSource(dscCtx, modules.ConfigFromDSC)
	enabledModules := make(map[string]bool)
	for _, name := range pm.EnabledModules() {
		enabledModules[name] = true
	}

	appNS, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("failed to resolve application namespace: %w", err)
	}

	gatewayDomain, _ := resources.GetGatewayDomain(ctx, rr.Client)
	crCfg := &modules.ModuleCRConfig{
		ApplicationsNamespace: appNS,
		GatewayDomain:         gatewayDomain,
		Release:               rr.Release,
	}

	return moduleReg.ForConfigSource(modules.ConfigFromDSC, func(handler modules.ModuleHandler, _ bool) error {
		name := handler.GetName()
		if !enabledModules[name] {
			return nil
		}

		moduleCR, err := handler.BuildModuleCR(ctx, rr.Client, dscCtx, crCfg)
		if err != nil {
			return fmt.Errorf("BuildModuleCR failed for module %s: %w", name, err)
		}
		if moduleCR != nil {
			rr.Resources = append(rr.Resources, *moduleCR)
		}
		return nil
	})
}

func updateStatus(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	instance.Status.Release = rr.Release
	if err := computeComponentsStatus(ctx, rr, cr.DefaultRegistry()); err != nil {
		return err
	}
	if err := updateDeprecatedTrainingOperatorStatus(rr); err != nil {
		return err
	}
	return modules.ComputeModulesStatusDetailed(ctx, rr)
}

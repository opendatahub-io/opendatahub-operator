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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	// TODO: remove after https://issues.redhat.com/browse/RHOAIENG-15920
	finalizerName = "datasciencecluster.opendatahub.io/finalizer"
)

func isNilInterface(v any) bool {
	return v == nil || (reflect.ValueOf(v).Kind() == reflect.Ptr && reflect.ValueOf(v).IsNil())
}

func initialize(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	// TODO: remove after https://issues.redhat.com/browse/RHOAIENG-15920
	if controllerutil.RemoveFinalizer(instance, finalizerName) {
		if err := rr.Client.Update(ctx, instance); err != nil {
			return err
		}
	}

	return nil
}

func checkPreConditions(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	// This case should not happen, since there is a webhook that blocks the creation
	// of more than one instance of the DataScienceCluster, however one can create a
	// DataScienceCluster instance while the operator is stopped, hence this extra check

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

// syncPlatformCR projects module enablement from DSC spec into the
// Platform CR via SSA. This makes Platform CR the canonical source of
// module state, allowing the platform controller (module reconciler) to
// always read Platform CR regardless of cluster type.
func syncPlatformCR(_ context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	platform := &configv1alpha1.Platform{
		TypeMeta: metav1.TypeMeta{
			APIVersion: configv1alpha1.GroupVersion.String(),
			Kind:       configv1alpha1.PlatformKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: configv1alpha1.PlatformInstanceName,
		},
		Spec: configv1alpha1.PlatformSpec{
			Modules: instance.Spec.Components.PlatformModules(),
		},
	}

	return rr.AddResources(platform)
}

// cleanupDisabledComponents deletes component CRs for disabled components
// in reverse batch order. Higher-RL components are cleaned up before
// lower-RL ones to respect dependency ordering.
func cleanupDisabledComponents(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	log := logf.FromContext(ctx)
	componentReg := cr.DefaultRegistry()

	reverseBatches, err := provision.DefaultRegistry().ReverseBatches()
	if err != nil {
		log.Error(err, "DAG reverse resolution failed, skipping component cleanup")

		return nil
	}

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
			}
		}
	}

	return nil
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

// cleanupDisabledModuleCRs deletes module CRs for disabled modules in
// reverse batch order via the module handler's DeleteModuleCR method.
func cleanupDisabledModuleCRs(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	moduleReg := modules.DefaultRegistry()
	if !moduleReg.HasEntries() {
		return nil
	}

	pm := instance.Spec.Components.PlatformModules()
	enabledModules := make(map[string]bool)
	for _, name := range pm.EnabledModules() {
		enabledModules[name] = true
	}

	log := logf.FromContext(ctx)

	reverseBatches, err := provision.DefaultRegistry().ReverseBatches()
	if err != nil {
		log.Error(err, "DAG reverse resolution failed, falling back to alphabetical module CR cleanup")

		return moduleReg.ForAll(func(handler modules.ModuleHandler, _ bool) error {
			if !enabledModules[handler.GetName()] {
				if delErr := handler.DeleteModuleCR(ctx, rr.Client); delErr != nil {
					log.Error(delErr, "DeleteModuleCR failed", "module", handler.GetName())
				}
			}

			return nil
		})
	}

	for _, batch := range reverseBatches {
		for _, entry := range provision.ModulesInBatch(batch) {
			handler := moduleReg.Lookup(entry.GetName())
			if handler == nil {
				continue
			}

			if enabledModules[handler.GetName()] {
				continue
			}

			if err := handler.DeleteModuleCR(ctx, rr.Client); err != nil {
				log.Error(err, "DeleteModuleCR failed", "module", handler.GetName())
			}
		}
	}

	return nil
}

// provisionComponents iterates over all enabled components and creates
// their CRs unconditionally (no DAG gating). rr.Resources is always
// complete, which ensures GC never deletes valid component CRs.
//
// Upgrade-time batch ordering is enforced by component controllers
// themselves via RunlevelGateAction, not by withholding CR creation.
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
			log.Error(nil, "component CR does not implement client.Object",
				"component", name, "type", fmt.Sprintf("%T", ci))
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

// provisionModuleCRs creates CRs for enabled modules. Deletion of
// disabled module CRs is handled by cleanupDisabledModuleCRs.
func provisionModuleCRs(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return fmt.Errorf("resource instance %v is not a dscv2.DataScienceCluster)", rr.Instance)
	}

	moduleReg := modules.DefaultRegistry()
	if !moduleReg.HasEntries() {
		return nil
	}

	pm := instance.Spec.Components.PlatformModules()
	enabledModules := make(map[string]bool)
	for _, name := range pm.EnabledModules() {
		enabledModules[name] = true
	}

	return moduleReg.ForAll(func(handler modules.ModuleHandler, _ bool) error {
		name := handler.GetName()

		if !enabledModules[name] {
			return nil
		}

		moduleCR, err := handler.BuildModuleCR(ctx, rr.Client, instance, nil)
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

	mirrorPlatformConditions(ctx, rr)

	return nil
}

// mirrorPlatformConditions copies conditions owned by the platform
// controller (ModulesReady, ProvisioningProgress) from Platform CR to
// DSC status so users see them on DSC.
func mirrorPlatformConditions(ctx context.Context, rr *odhtype.ReconciliationRequest) {
	var platform configv1alpha1.Platform
	if err := rr.Client.Get(ctx, client.ObjectKey{Name: configv1alpha1.PlatformInstanceName}, &platform); err != nil {
		return
	}

	mirrored := map[string]bool{
		status.ConditionTypeModulesReady:         true,
		status.ConditionTypeProvisioningProgress: true,
	}

	for _, c := range platform.GetConditions() {
		if mirrored[c.Type] {
			rr.Conditions.SetCondition(common.Condition{
				Type:    c.Type,
				Status:  c.Status,
				Reason:  c.Reason,
				Message: c.Message,
			})
		}
	}
}

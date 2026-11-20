package platformmodules

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/flags"
)

// buildModulePlatformContext constructs a modules.PlatformContext scoped to
// a single PlatformModule reconcile. Unlike the Platform controller's own
// context builder, this does not need module enablement data: the presence
// of the PlatformModule CR is itself the deploy signal.
func buildModulePlatformContext(ctx context.Context, rr *odhtype.ReconciliationRequest) (*modules.PlatformContext, error) {
	inputs, err := modules.ResolvePlatformInputs(ctx, rr.Client)
	if err != nil {
		return nil, fmt.Errorf("resolving platform inputs: %w", err)
	}

	var otelOperatorNS string
	var metricsServiceMonitorEnabled bool
	if rr.Instance.GetName() == serviceApi.MonitoringServiceName {
		otelOperatorNS, err = resolveOTelOperatorNamespace(ctx, rr.Client)
		if err != nil {
			return nil, fmt.Errorf("resolving OpenTelemetry Operator namespace: %w", err)
		}
		metricsServiceMonitorEnabled, err = cluster.HasCRD(ctx, rr.Client, gvk.ServiceMonitor)
		if err != nil {
			return nil, fmt.Errorf("checking RHOBS ServiceMonitor CRD availability: %w", err)
		}
	}

	return &modules.PlatformContext{
		ApplicationsNamespace:        inputs.ApplicationsNamespace,
		MonitoringNamespace:          inputs.MonitoringNamespace,
		OTelOperatorNamespace:        otelOperatorNS,
		MetricsServiceMonitorEnabled: metricsServiceMonitorEnabled,
		Release:                      rr.Release,
		ChartsBasePath:               rr.ChartsBasePath,
		ManifestsBasePath:            rr.ManifestsBasePath,
	}, nil
}

// checkPlatformModuleRunlevelGate reads the Platform controller's durable DAG
// checkpoint and skips deployment until this PlatformModule's runlevel is
// opened for the active release.
//
// RequeueAfterError does not stop the action pipeline (the framework
// treats it as "continue, schedule a delayed requeue"), so
// provisionModuleOperator and injectModulePlatformConfig must also check
// rr.SkipDeploy.
func checkPlatformModuleRunlevelGate(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	if flags.IsDAGOrderingDisabled() {
		return nil
	}

	name := rr.Instance.GetName()

	order, found := provision.DefaultRegistry().LookupOrder(name)
	if !found {
		return nil
	}

	platform := &configApi.Platform{}
	if err := rr.Client.Get(ctx, client.ObjectKey{Name: configApi.PlatformInstanceName}, platform); err != nil {
		return fmt.Errorf("getting Platform DAG status: %w", err)
	}

	version := rr.Release.Version.String()
	if platform.Status.DAG.Release.Version.String() != version || platform.Status.DAG.Release.Name != rr.Release.Name ||
		platform.Status.DAG.ClearedRunlevel < order {
		rr.SkipDeploy = true

		logf.FromContext(ctx).Info("runlevel not cleared, deferring module operator deploy",
			"module", name, "runlevel", order, "version", version,
			"clearedRunlevel", platform.Status.DAG.ClearedRunlevel)

		return odherrors.NewRequeueAfterError(30 * time.Second)
	}

	return nil
}

// provisionModuleOperator renders the module's operator manifests (Helm or
// Kustomize) into rr.HelmCharts/rr.Manifests for the standard render/deploy
// actions that follow in the pipeline.
func provisionModuleOperator(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	if rr.SkipDeploy {
		return nil
	}

	name := rr.Instance.GetName()

	handler := modules.DefaultRegistry().Lookup(name)
	if handler == nil {
		return fmt.Errorf("no module handler registered for %q", name)
	}

	platformCtx, err := buildModulePlatformContext(ctx, rr)
	if err != nil {
		return err
	}

	operatorManifests := handler.GetOperatorManifests(platformCtx)
	appendModuleEnvInjection(
		rr,
		platformCtx.ApplicationsNamespace,
		platformCtx.MonitoringNamespace,
		platformCtx.Release.Name,
		moduleImagesFor(handler, operatorManifests),
	)

	if len(operatorManifests.HelmCharts) > 0 {
		rr.HelmCharts = append(rr.HelmCharts, operatorManifests.HelmCharts...)
	}
	if len(operatorManifests.Manifests) > 0 {
		rr.Manifests = append(rr.Manifests, operatorManifests.Manifests...)
	}

	return nil
}

// injectModulePlatformConfig creates or merges the per-module platform
// config ConfigMap (odh-<module>-config) for the single module owned by
// this PlatformModule CR.
func injectModulePlatformConfig(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	if rr.SkipDeploy {
		return nil
	}

	log := logf.FromContext(ctx)
	name := rr.Instance.GetName()

	handler := modules.DefaultRegistry().Lookup(name)
	if handler == nil {
		return fmt.Errorf("no module handler registered for %q", name)
	}

	platformCtx, err := buildModulePlatformContext(ctx, rr)
	if err != nil {
		return err
	}

	platformVersion := rr.Release.Version.String()

	var extraParams map[string]string
	if rr.Release.Name == cluster.XKS {
		extraParams = buildCertManagerConfigParams()
	}

	existingCMs := indexConfigMapsByName(rr.Resources)
	cmName := PlatformConfigName(name)

	if idx, ok := existingCMs[cmName]; ok {
		log.V(1).Info("merging platform config into existing ConfigMap", "module", name, "configmap", cmName)
		mergePlatformKeys(&rr.Resources[idx], platformVersion, extraParams)

		return nil
	}

	log.V(1).Info("creating platform config ConfigMap", "module", name, "configmap", cmName)
	cm := buildPlatformConfigMap(cmName, platformCtx.ApplicationsNamespace, platformVersion, extraParams)

	u, err := resources.ToUnstructured(cm)
	if err != nil {
		return fmt.Errorf("converting platform config ConfigMap for %s: %w", name, err)
	}
	rr.Resources = append(rr.Resources, *u)

	return nil
}

// handlePlatformModuleDeletion implements two-phase cleanup as the
// PlatformModule CR's finalizer action.
//
// Returning a plain (non-StopError) error here is intentional: the
// framework's delete() loop only treats odherrors.StopError specially
// (break without failing); any other error propagates up, the finalizer
// is *not* removed, and controller-runtime retries with its standard
// backoff. That gives us "keep operator alive, check back later"
// semantics while the module CR is still deleting.
func handlePlatformModuleDeletion(ctx context.Context, rr *odhtype.ReconciliationRequest) (retErr error) {
	pm, ok := rr.Instance.(*configApi.PlatformModule)
	if !ok {
		return fmt.Errorf("handlePlatformModuleDeletion requires PlatformModule instance, got %T", rr.Instance)
	}

	before := pm.DeepCopy()
	// The ODH platform-utilities reconciler does not save status after finalizer
	// actions, so report blocked cleanup here before returning the retry error.
	defer func() {
		if retErr == nil {
			return
		}
		conditions.SetStatusCondition(pm, common.Condition{
			Type: status.ConditionTypeReady, Status: metav1.ConditionFalse, Reason: status.DeletingReason,
			Message: retErr.Error(), ObservedGeneration: pm.Generation,
		})
		if err := rr.Client.Status().Patch(ctx, pm, client.MergeFrom(before)); err != nil && !k8serr.IsNotFound(err) {
			retErr = multierror.Append(retErr, fmt.Errorf("persisting tracker deletion status: %w", err)).ErrorOrNil()
		}
	}()

	log := logf.FromContext(ctx)
	name := pm.GetName()

	handler := modules.DefaultRegistry().Lookup(name)
	if handler == nil {
		return fmt.Errorf("no module handler registered for %q; cannot establish module CR absence", name)
	}

	crState, err := handler.GetModuleCRState(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("checking module CR state for %s: %w", name, err)
	}

	if crState != modules.CRStateAbsent {
		if crState == modules.CRStateDeleting {
			return fmt.Errorf("module CR for %s still exists and is deleting; waiting for its finalizers before operator cleanup", name)
		}
		// A re-enabled module can have a live CR before the old tracker finishes.
		// Finish cleaning up the old inventory so its replacement can deploy.
		platform := &configApi.Platform{}
		if err := rr.Client.Get(ctx, client.ObjectKey{Name: configApi.PlatformInstanceName}, platform); err != nil && !k8serr.IsNotFound(err) {
			return fmt.Errorf("checking whether module %s was re-enabled: %w", name, err)
		}
		if crState != modules.CRStateAlive || !modules.PlatformOwnsTracker(platform, pm) || !platform.DeletionTimestamp.IsZero() ||
			!modules.DefaultRegistry().IsEnabled(name) || !handler.IsEnabled(&platform.Spec.Modules) {
			return fmt.Errorf("module CR for %s still exists; waiting for its deletion before operator cleanup", name)
		}
	}

	log.Info("deleting tracked operator resources", "module", name, "resourceCount", len(pm.Status.Resources))

	return deletePlatformModuleResources(ctx, rr.Client, pm, pm.Status.Resources)
}

// cleanupStalePlatformModuleResources removes resources present in the last
// successful inventory but absent from the newly deployed desired inventory.
// Status replacement runs only after this action succeeds, preserving the old
// inventory for retry when cleanup fails.
func cleanupStalePlatformModuleResources(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	if rr.SkipDeploy {
		return nil
	}

	pm, ok := rr.Instance.(*configApi.PlatformModule)
	if !ok {
		return fmt.Errorf("cleanupStalePlatformModuleResources requires PlatformModule instance, got %T", rr.Instance)
	}

	desired := resourceRefs(rr.Resources)
	desiredSet := make(map[platformModuleResourceKey]struct{}, len(desired))
	for _, ref := range desired {
		desiredSet[resourceKey(ref)] = struct{}{}
	}

	stale := make([]configApi.ResourceRef, 0)
	for _, ref := range pm.Status.Resources {
		if _, found := desiredSet[resourceKey(ref)]; !found {
			stale = append(stale, ref)
		}
	}

	return deletePlatformModuleResources(ctx, rr.Client, pm, stale)
}

type platformModuleResourceKey struct {
	group     string
	kind      string
	namespace string
	name      string
}

func resourceKey(ref configApi.ResourceRef) platformModuleResourceKey {
	return platformModuleResourceKey{
		group: ref.Group, kind: ref.Kind, namespace: ref.Namespace, name: ref.Name,
	}
}

func resourceRefs(objects []unstructured.Unstructured) []configApi.ResourceRef {
	refs := make([]configApi.ResourceRef, 0, len(objects))
	seen := make(map[platformModuleResourceKey]struct{}, len(objects))
	for i := range objects {
		gvk := objects[i].GroupVersionKind()
		ref := configApi.ResourceRef{
			Group:     gvk.Group,
			Version:   gvk.Version,
			Kind:      gvk.Kind,
			Namespace: objects[i].GetNamespace(),
			Name:      objects[i].GetName(),
		}
		if _, found := seen[resourceKey(ref)]; found {
			continue
		}
		seen[resourceKey(ref)] = struct{}{}
		refs = append(refs, ref)
	}

	sort.Slice(refs, func(i, j int) bool {
		left, right := resourceKey(refs[i]), resourceKey(refs[j])
		if left.group != right.group {
			return left.group < right.group
		}
		if left.kind != right.kind {
			return left.kind < right.kind
		}
		if left.namespace != right.namespace {
			return left.namespace < right.namespace
		}
		if left.name != right.name {
			return left.name < right.name
		}
		return refs[i].Version < refs[j].Version
	})

	return refs
}

func deletePlatformModuleResources(
	ctx context.Context,
	cli client.Client,
	pm *configApi.PlatformModule,
	refs []configApi.ResourceRef,
) error {
	var errs *multierror.Error
	var pending []string
	for _, ref := range slices.Backward(refs) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(ref.GroupVersionKind())
		err := cli.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: ref.Namespace}, obj)
		switch {
		case k8serr.IsNotFound(err), meta.IsNoMatchError(err):
			continue
		case err != nil:
			errs = multierror.Append(errs, fmt.Errorf("getting %s %s/%s before deletion: %w", ref.Kind, ref.Namespace, ref.Name, err))
			continue
		}

		if !isCurrentModuleController(metav1.GetControllerOf(obj), pm) {
			continue
		}

		if isUnremovablePlatformModuleResource(ref) || obj.GetAnnotations()[annotations.ManagedByODHOperator] == "false" {
			// Retained resources must also survive Kubernetes garbage collection.
			err := resources.RemoveOwnerReferences(ctx, cli, obj, func(ref metav1.OwnerReference) bool { return ref.UID == pm.GetUID() })
			if err != nil && !k8serr.IsNotFound(err) && !meta.IsNoMatchError(err) {
				errs = multierror.Append(errs, err)
			}
			continue
		}

		if obj.GetDeletionTimestamp().IsZero() {
			uid, resourceVersion := obj.GetUID(), obj.GetResourceVersion()
			err = cli.Delete(ctx, obj, client.Preconditions{UID: &uid, ResourceVersion: &resourceVersion})
			switch {
			case k8serr.IsNotFound(err), meta.IsNoMatchError(err):
				continue
			case err != nil:
				errs = multierror.Append(errs, fmt.Errorf("deleting %s %s/%s: %w", ref.Kind, ref.Namespace, ref.Name, err))
				continue
			}
		}
		// Confirm absence on the next reconciliation, after the cache can observe deletion.
		resource := fmt.Sprintf("%s %s", obj.GroupVersionKind().GroupKind(), client.ObjectKeyFromObject(obj))
		if finalizers := obj.GetFinalizers(); len(finalizers) > 0 {
			resource += fmt.Sprintf(" (finalizers: %s)", strings.Join(finalizers, ", "))
		}
		pending = append(pending, resource)
	}
	if err := errs.ErrorOrNil(); err != nil {
		return err
	}
	if len(pending) > 0 {
		return fmt.Errorf("operator resources are still deleting: %s: %w", strings.Join(pending, "; "), odherrors.NewRequeueAfterError(30*time.Second))
	}
	return nil
}

func isCurrentModuleController(owner *metav1.OwnerReference, pm *configApi.PlatformModule) bool {
	return owner != nil && owner.APIVersion == configApi.GroupVersion.String() && owner.Kind == configApi.PlatformModuleKind &&
		owner.Name == pm.Name && pm.UID != "" && owner.UID == pm.UID
}

// isUnremovablePlatformModuleResource also protects historical or incorrect
// inventories and owner references. New protected kinds must be excluded from
// dynamic ownership (or the deploy action's ownership handling) as well.
func isUnremovablePlatformModuleResource(ref configApi.ResourceRef) bool {
	switch ref.Kind {
	case gvk.CustomResourceDefinition.Kind, gvk.Namespace.Kind, gvk.Lease.Kind:
		return true
	default:
		return false
	}
}

// updatePlatformModuleStatus is the last reconciliation action: finish stale
// cleanup after a successful apply, then publish inventory and release.
// Runtime module readiness remains independent of that durable deployment commit.
func updatePlatformModuleStatus(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	pm, ok := rr.Instance.(*configApi.PlatformModule)
	if !ok {
		return fmt.Errorf("updatePlatformModuleStatus requires PlatformModule instance, got %T", rr.Instance)
	}
	handler := modules.DefaultRegistry().Lookup(pm.GetName())
	if handler == nil {
		return fmt.Errorf("no module handler registered for %q", pm.GetName())
	}
	defer updateModuleCRCondition(ctx, rr, handler)

	if rr.SkipDeploy {
		// The runlevel gate already scheduled a retry. Waiting must not mark a
		// healthy existing module unready or publish an unapplied release.
		return nil
	}

	if err := cleanupStalePlatformModuleResources(ctx, rr); err != nil {
		if pending, ok := errors.AsType[odherrors.RequeueAfterError](err); ok {
			// Convert only on apply: finalizers must retain the shared deletion error.
			// Do not wrap the requeue marker; the framework would continue past it.
			return odherrors.NewStopError("operator resource cleanup incomplete: %v", err).
				WithRequeueAfter(pending.After)
		}
		return err
	}

	pm.Status.Resources = resourceRefs(rr.Resources)
	pm.Status.Release = rr.Release
	return nil
}

func updateModuleCRCondition(ctx context.Context, rr *odhtype.ReconciliationRequest, handler modules.ModuleHandler) {
	moduleStatus, err := handler.GetModuleStatus(ctx, rr.Client)
	if err != nil {
		rr.Conditions.MarkUnknown(status.ConditionTypeModuleCRReady,
			conditions.WithReason(status.NotReadyReason),
			conditions.WithMessagef("failed to read module CR status: %v", err),
		)

		return
	}

	if moduleStatus.ObservedGeneration > 0 && moduleStatus.ObservedGeneration < moduleStatus.Generation {
		rr.Conditions.MarkFalse(status.ConditionTypeModuleCRReady,
			conditions.WithReason(status.NotReadyReason),
			conditions.WithMessage("Module status is stale (observedGeneration < generation)"),
		)
		return
	}

	var readyCond, degradedCond *common.Condition
	for i := range moduleStatus.Conditions {
		switch moduleStatus.Conditions[i].Type {
		case status.ConditionTypeReady:
			readyCond = &moduleStatus.Conditions[i]
		case status.ConditionTypeDegraded:
			degradedCond = &moduleStatus.Conditions[i]
		}
	}

	crState, _ := handler.GetModuleCRState(ctx, rr.Client)
	switch {
	case crState == modules.CRStateDeleting:
		rr.Conditions.MarkFalse(status.ConditionTypeModuleCRReady,
			conditions.WithReason(status.DeletingReason),
			conditions.WithMessage(status.DeletingMessage),
		)
	case degradedCond != nil && degradedCond.Status == metav1.ConditionTrue:
		rr.Conditions.MarkFalse(status.ConditionTypeModuleCRReady,
			conditions.WithReason(degradedCond.Reason),
			conditions.WithMessage(degradedCond.Message),
		)
	case readyCond != nil:
		rr.Conditions.MarkFrom(status.ConditionTypeModuleCRReady, *readyCond)
	default:
		rr.Conditions.MarkFalse(status.ConditionTypeModuleCRReady,
			conditions.WithReason(status.NotReadyReason),
			conditions.WithMessage("module CR has no Ready condition"),
		)
	}
}

// checkModuleUpgradeGates enforces the shared gate barrier just before deploy,
// including for existing trackers when runlevel ordering is disabled.
func checkModuleUpgradeGates(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	if rr.SkipDeploy {
		return nil
	}
	platform := &configApi.Platform{}
	if err := rr.Client.Get(ctx, client.ObjectKey{Name: configApi.PlatformInstanceName}, platform); err != nil {
		return fmt.Errorf("getting Platform for shared upgrade gates: %w", err)
	}
	gates, _, err := modules.DiscoverModuleUpgradeGates(ctx, rr, platform, rr.Instance.GetName())
	if err != nil {
		return err
	}
	return provision.CheckUpgradeGates(ctx, rr.Client, rr.Release, rr.Conditions, gates)
}

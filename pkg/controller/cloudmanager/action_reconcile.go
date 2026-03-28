package cloudmanager

import (
	"context"
	"errors"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/helm"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/rules"
)

var ConditionsTypes = []string{
	status.ConditionDeploymentsAvailable,
}

// reconcileAction holds the configuration for the combined reconcile action.
type reconcileAction struct {
	helmOpts   []helm.ActionOpts
	deployOpts []deploy.ActionOpts
	resourceID string
}

// ReconcileActionOpts configures the combined reconcile action.
type ReconcileActionOpts func(*reconcileAction)

// WithHelmOptions adds Helm rendering options.
func WithHelmOptions(opts ...helm.ActionOpts) ReconcileActionOpts {
	return func(a *reconcileAction) {
		a.helmOpts = append(a.helmOpts, opts...)
	}
}

// WithDeployOptions adds deploy action options.
func WithDeployOptions(opts ...deploy.ActionOpts) ReconcileActionOpts {
	return func(a *reconcileAction) {
		a.deployOpts = append(a.deployOpts, opts...)
	}
}

// NewReconcileAction creates a combined action that:
// - Renders Helm charts
// - Runs PreApply hooks from HelmCharts
// - Sets infrastructure labels on rendered resources
// - Deploys resources via SSA
// - Runs PostApply hooks from HelmCharts
// - Checks deployment status.
func NewReconcileAction(resourceID string, opts ...ReconcileActionOpts) (actions.Fn, error) {
	resourceID = labels.NormalizePartOfValue(resourceID)
	if resourceID == "" {
		return nil, errors.New("NewReconcileAction: resourceID is required")
	}

	action := reconcileAction{
		resourceID: resourceID,
	}

	for _, opt := range opts {
		opt(&action)
	}

	helmRender := helm.NewAction(action.helmOpts...)
	deployAction := deploy.NewAction(append(action.deployOpts, deploy.WithApplyOrder())...)
	deploymentsAction := deployments.NewAction(
		deployments.InNamespaceFn(func(_ context.Context, _ *types.ReconciliationRequest) (string, error) {
			return "", nil
		}),
		deployments.WithSelectorLabel(labels.InfrastructurePartOf, action.resourceID),
	)

	return func(ctx context.Context, rr *types.ReconciliationRequest) error {
		// Render Helm charts (populates rr.Resources)
		if err := helmRender(ctx, rr); err != nil {
			return fmt.Errorf("helm render failed: %w", err)
		}

		// Execute pre-apply hooks
		err := runHooks(ctx, rr, func(c *types.HelmChartInfo) []types.HookFn {
			return c.PreApply
		})
		if err != nil {
			return err
		}

		// Set infrastructure label on all rendered resources
		for i := range rr.Resources {
			resources.SetLabel(&rr.Resources[i], labels.InfrastructurePartOf, action.resourceID)
		}

		// Remove owner references from resources of Unmanaged dependencies so they
		// survive cascade deletion when the CR is deleted. Must run after the render
		// loop (needs rr.Resources to derive currentDeps) and before deploy (deploy
		// re-adds owner refs to currently-Managed resources in the same cycle).
		if err := cleanupOwnership(ctx, rr, action.resourceID); err != nil {
			return fmt.Errorf("ownership cleanup failed: %w", err)
		}

		if err := deployAction(ctx, rr); err != nil {
			return fmt.Errorf("deploy failed: %w", err)
		}

		// Execute post-apply hooks
		err = runHooks(ctx, rr, func(c *types.HelmChartInfo) []types.HookFn {
			return c.PostApply
		})
		if err != nil {
			return err
		}

		// Check deployments
		// TODO: the monitoring should be set per dependency to improve user experience
		if err := deploymentsAction(ctx, rr); err != nil {
			return fmt.Errorf("deployments check failed: %w", err)
		}

		return nil
	}, nil
}

// cleanupOwnership removes owner references from resources of Unmanaged dependencies.
//
// After the render step, rr.Resources contains only the resources for currently-Managed
// dependencies. A cluster resource whose InfrastructureDependency label is NOT in that
// set belongs to an Unmanaged dependency: its owner reference is removed so the resource
// survives cascade deletion when the CR is deleted.
//
// Resource types are pre-filtered by RBAC permissions (list+update) before any list call
// is issued, matching the pattern used by the GC action. The server-side list additionally
// uses InfrastructurePartOf=resourceID plus InfrastructureDependency NotIn (managed-dep-labels)
// to return only Unmanaged dep resources.
//
// The function is a no-op when rr.Generated is false (cache hit), because dependency
// state can only change on a spec update, which always causes a fresh render.
//
// Error handling is best-effort per resource: a transient error on one resource is logged
// and skipped; remaining resources are still processed. A resource retains its owner
// reference until the next successful reconcile.
func cleanupOwnership(ctx context.Context, rr *types.ReconciliationRequest, resourceID string) error {
	// Skip when nothing was freshly rendered — dep state cannot have changed.
	if !rr.Generated {
		return nil
	}

	log := logf.FromContext(ctx)

	currentDeps := currentManagedDeps(rr)

	apiResourceLists, err := resources.ListAvailableAPIResources(rr.Controller.GetDiscoveryClient())
	if err != nil {
		return fmt.Errorf("failed to discover resource types for ownership cleanup: %w", err)
	}
	if len(apiResourceLists) == 0 {
		return nil
	}

	ns, err := operatorNamespaceFn(ctx, rr)
	if err != nil {
		return fmt.Errorf("failed to resolve operator namespace for ownership cleanup: %w", err)
	}

	authorizedTypes, err := rules.ListAuthorizedResources(ctx, rr.Client, apiResourceLists, ns, []string{rules.VerbList, rules.VerbUpdate})
	if err != nil {
		return fmt.Errorf("failed to list authorized resource types for ownership cleanup: %w", err)
	}

	// Build a selector that returns only resources belonging to Unmanaged dependencies.
	// "Unmanaged dep resource" = has InfrastructureDependency label whose value is NOT
	// in the set of currently-rendered (Managed) dep labels.
	selector := k8slabels.SelectorFromSet(k8slabels.Set{
		labels.InfrastructurePartOf: resourceID,
	})
	depExistsReq, err := k8slabels.NewRequirement(labels.InfrastructureDependency, selection.Exists, nil)
	if err != nil {
		return fmt.Errorf("failed to build dep exists selector: %w", err)
	}
	selector = selector.Add(*depExistsReq)
	if len(currentDeps) > 0 {
		managedDepValues := make([]string, 0, len(currentDeps))
		for dep := range currentDeps {
			managedDepValues = append(managedDepValues, dep)
		}
		notInReq, err := k8slabels.NewRequirement(labels.InfrastructureDependency, selection.NotIn, managedDepValues)
		if err != nil {
			return fmt.Errorf("failed to build dep not-in selector: %w", err)
		}
		selector = selector.Add(*notInReq)
	}

	lo := metav1.ListOptions{LabelSelector: selector.String()}

	dc := rr.Controller.GetDynamicClient()

	for _, res := range authorizedTypes {
		gvr := res.GroupVersionResource()
		items, err := dc.Resource(gvr).Namespace("").List(ctx, lo)
		switch {
		case k8serr.IsForbidden(err), k8serr.IsMethodNotSupported(err), k8serr.IsNotFound(err):
			log.V(3).Info("skipping resource type for ownership cleanup",
				"gvr", gvr, "reason", err)
			continue
		case err != nil:
			log.V(1).Info("unexpected error listing resource type for ownership cleanup",
				"gvr", gvr, "reason", err)
			continue
		}

		for i := range items.Items {
			obj := &items.Items[i]

			depLabel := resources.GetLabel(obj, labels.InfrastructureDependency)
			if depLabel == "" {
				continue
			}
			if _, managed := currentDeps[depLabel]; managed {
				continue
			}

			// Only remove owner refs for resources that belong to this CR instance.
			if resources.GetAnnotation(obj, annotations.InstanceUID) != string(rr.Instance.GetUID()) {
				continue
			}
			if !obj.GetDeletionTimestamp().IsZero() {
				continue
			}

			err := resources.RemoveOwnerReferences(ctx, rr.Client, obj, func(ref metav1.OwnerReference) bool {
				return ref.UID == rr.Instance.GetUID()
			})
			if err != nil {
				log.Error(err, "failed to remove owner reference from Unmanaged resource",
					"gvk", obj.GroupVersionKind(),
					"name", obj.GetName(),
					"namespace", obj.GetNamespace(),
					"dependency", depLabel)
				continue
			}

			log.Info("removed owner reference from Unmanaged resource",
				"gvk", obj.GroupVersionKind(),
				"name", obj.GetName(),
				"namespace", obj.GetNamespace(),
				"dependency", depLabel)
		}
	}

	return nil
}

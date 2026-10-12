package cloudmanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	helmRenderer "github.com/k8s-manifest-kit/renderer-helm/pkg"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// How long to wait before retrying Pass B after deferring SA/RBAC deletion.
// Workload termination is usually prompt; a short interval avoids waiting on a
// watch that may not fire if only non-owned objects change.
const accessControlCleanupRequeueAfter = 5 * time.Second

// deferredAccessControlCleanup tracks instance UIDs that deferred Pass B
// (access-control deletion) because owned workloads were still present. Keys are
// string(UID). This bypasses the rr.Generated guard on subsequent reconciles so
// Pass B can finish even when the resource hash is unchanged (CleanupCharts are
// not part of that hash).
var deferredAccessControlCleanup sync.Map

func markAccessControlCleanupDeferred(rr *types.ReconciliationRequest) {
	deferredAccessControlCleanup.Store(string(rr.Instance.GetUID()), struct{}{})
}

func clearAccessControlCleanupDeferred(rr *types.ReconciliationRequest) {
	deferredAccessControlCleanup.Delete(string(rr.Instance.GetUID()))
}

func isAccessControlCleanupDeferred(rr *types.ReconciliationRequest) bool {
	_, ok := deferredAccessControlCleanup.Load(string(rr.Instance.GetUID()))
	return ok
}

// isAccessControlKind reports whether kind is an access-control resource that must
// remain until operator workloads have finished terminating (so leader-elected
// processes can still update their Lease on SIGTERM).
func isAccessControlKind(kind string) bool {
	switch kind {
	case gvk.ServiceAccount.Kind,
		gvk.Role.Kind,
		gvk.RoleBinding.Kind,
		gvk.ClusterRole.Kind,
		gvk.ClusterRoleBinding.Kind:
		return true
	default:
		return false
	}
}

// isWorkloadKind reports whether kind is an operator workload that must be gone
// before access-control resources are deleted.
func isWorkloadKind(kind string) bool {
	switch kind {
	case gvk.Deployment.Kind,
		gvk.StatefulSet.Kind,
		gvk.DaemonSet.Kind,
		"ReplicaSet":
		return true
	default:
		return false
	}
}

func cleanupExcludedCharts(ctx context.Context, rr *types.ReconciliationRequest, charts []types.HelmChartInfo) error {
	if len(charts) == 0 {
		clearAccessControlCleanupDeferred(rr)
		return nil
	}

	// rr.Generated is true only on a cache miss (rendered resource hash changed).
	// Skipping when false avoids redundant chart renders and API Gets in steady
	// state — consistent with how NewGCAction guards its own run.
	//
	// Exception: once Pass B has been deferred (workloads still terminating), keep
	// running cleanup even when Generated is false. CleanupCharts are not part of
	// the render hash, so Generated can stay false after Pass A while SA/RBAC must
	// still be deleted once workloads are gone.
	if !rr.Generated && !isAccessControlCleanupDeferred(rr) {
		return nil
	}

	l := logf.FromContext(ctx)

	sources := make([]helmRenderer.Source, 0, len(charts))
	for _, c := range charts {
		sources = append(sources, c.Source)
	}

	renderer, err := helmRenderer.New(sources, helmRenderer.RendererOptions{Strict: true})
	if err != nil {
		return fmt.Errorf("cleanup chart render failed: %w", err)
	}

	resources, err := renderer.Process(ctx, map[string]any{})
	if err != nil {
		return fmt.Errorf("cleanup chart render failed: %w", err)
	}

	unremovables := make(map[schema.GroupVersionKind]struct{}, len(gc.DefaultUnremovables)+len(unremovableGVKs))
	for _, u := range gc.DefaultUnremovables {
		unremovables[u] = struct{}{}
	}
	for _, u := range unremovableGVKs {
		unremovables[u] = struct{}{}
	}

	var merr *multierror.Error

	// Pass A: delete non-access-control resources (workloads and everything else).
	for i := range resources {
		obj := &resources[i]
		if isAccessControlKind(obj.GroupVersionKind().Kind) {
			continue
		}

		if err := deleteOwnedResource(ctx, rr, obj, unremovables); err != nil {
			merr = multierror.Append(merr, err)
		}
	}

	// Gate: retain SA/RBAC while any owned operator workload is still present so
	// the terminating process can release its leader-election Lease.
	workloadsRemain, err := ownedWorkloadExists(ctx, rr, resources, unremovables)
	if err != nil {
		merr = multierror.Append(merr, err)
	}
	if workloadsRemain {
		l.V(1).Info("deferring access-control cleanup until operator workloads terminate")
		markAccessControlCleanupDeferred(rr)
		if err := merr.ErrorOrNil(); err != nil {
			return err
		}

		return odherrors.NewRequeueAfterError(accessControlCleanupRequeueAfter)
	}

	// Pass B: workloads are gone — safe to remove ServiceAccount and RBAC.
	// Mark deferred before Pass B so a Generated=false follow-up still retries if
	// a delete fails mid-pass. ConfigMap-only charts skip this (no access-control).
	for i := range resources {
		if isAccessControlKind(resources[i].GroupVersionKind().Kind) {
			markAccessControlCleanupDeferred(rr)
			break
		}
	}

	for i := range resources {
		obj := &resources[i]
		if !isAccessControlKind(obj.GroupVersionKind().Kind) {
			continue
		}

		if err := deleteOwnedResource(ctx, rr, obj, unremovables); err != nil {
			merr = multierror.Append(merr, err)
		}
	}

	if err := merr.ErrorOrNil(); err != nil {
		return err
	}

	clearAccessControlCleanupDeferred(rr)

	return nil
}

// ownedWorkloadExists reports whether any owned operator workload from the
// rendered chart set still exists on the cluster.
func ownedWorkloadExists(
	ctx context.Context,
	rr *types.ReconciliationRequest,
	resources []unstructured.Unstructured,
	unremovables map[schema.GroupVersionKind]struct{},
) (bool, error) {
	l := logf.FromContext(ctx)
	var merr *multierror.Error

	for i := range resources {
		obj := &resources[i]
		objGVK := obj.GroupVersionKind()

		if !isWorkloadKind(objGVK.Kind) {
			continue
		}
		if _, skip := unremovables[objGVK]; skip {
			continue
		}

		live, owned, err := getOwnedLive(ctx, rr, obj)
		if err != nil {
			// Fail closed: if we cannot confirm the workload is gone, keep
			// access-control so a terminating operator can still release its Lease.
			l.Error(err, "cleanup workload get failed, deferring access-control cleanup",
				"gvk", objGVK,
				"childNamespace", obj.GetNamespace(),
				"child", obj.GetName(),
				"resourceKind", objGVK.Kind,
			)
			merr = multierror.Append(merr, fmt.Errorf("cleanup get failed for %s/%s: %w", obj.GetNamespace(), obj.GetName(), err))

			return true, merr.ErrorOrNil()
		}
		if live == nil || !owned {
			continue
		}

		l.V(1).Info("owned operator workload still present",
			"gvk", objGVK,
			"childNamespace", live.GetNamespace(),
			"child", live.GetName(),
		)

		return true, merr.ErrorOrNil()
	}

	return false, merr.ErrorOrNil()
}

// getOwnedLive Gets the live object for desired. Returns (nil, false, nil) when
// the object is not found or the GVK is not registered. A non-nil live object
// with owned=false means the resource exists but is not owned by rr.Instance.
func getOwnedLive(
	ctx context.Context,
	rr *types.ReconciliationRequest,
	desired *unstructured.Unstructured,
) (*unstructured.Unstructured, bool, error) {
	objGVK := desired.GroupVersionKind()

	live := &unstructured.Unstructured{}
	live.SetGroupVersionKind(objGVK)

	err := rr.Client.Get(ctx, client.ObjectKeyFromObject(desired), live)
	if err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil, false, nil
		}

		return nil, false, err
	}

	for _, ref := range live.GetOwnerReferences() {
		if ref.UID == rr.Instance.GetUID() {
			return live, true, nil
		}
	}

	return live, false, nil
}

// deleteOwnedResource deletes desired when it exists on the cluster and is owned
// by rr.Instance. Unremovable GVKs and unowned/missing objects are skipped.
// Returns a collected error for unexpected Get/Delete failures; nil otherwise.
func deleteOwnedResource(
	ctx context.Context,
	rr *types.ReconciliationRequest,
	desired *unstructured.Unstructured,
	unremovables map[schema.GroupVersionKind]struct{},
) error {
	l := logf.FromContext(ctx)
	objGVK := desired.GroupVersionKind()

	if _, skip := unremovables[objGVK]; skip {
		return nil
	}

	live, owned, err := getOwnedLive(ctx, rr, desired)
	if err != nil {
		l.Error(err, "cleanup get failed, skipping resource",
			"gvk", objGVK,
			"childNamespace", desired.GetNamespace(),
			"child", desired.GetName(),
			"resourceKind", objGVK.Kind,
		)

		return fmt.Errorf("cleanup get failed for %s/%s: %w", desired.GetNamespace(), desired.GetName(), err)
	}
	if live == nil {
		return nil
	}
	if !owned {
		l.V(1).Info("resource not owned by this instance, skipping cleanup",
			"gvk", objGVK,
			"childNamespace", live.GetNamespace(),
			"child", live.GetName(),
		)

		return nil
	}

	l.Info("cleanup excluded chart resource",
		"gvk", objGVK,
		"childNamespace", live.GetNamespace(),
		"child", live.GetName(),
	)

	if err := rr.Client.Delete(ctx, live, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}

		l.Error(err, "cleanup delete failed, skipping resource",
			"gvk", objGVK,
			"childNamespace", live.GetNamespace(),
			"child", live.GetName(),
			"resourceKind", objGVK.Kind,
		)

		return fmt.Errorf("cleanup delete failed for %s/%s: %w", live.GetNamespace(), live.GetName(), err)
	}

	return nil
}

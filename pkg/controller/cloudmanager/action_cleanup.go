package cloudmanager

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	helmRenderer "github.com/k8s-manifest-kit/renderer-helm/pkg"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func cleanupExcludedCharts(ctx context.Context, rr *types.ReconciliationRequest, charts []types.HelmChartInfo) error {
	// rr.Generated is true only on a cache miss (rendered resource hash changed). Skipping
	// when false avoids redundant chart renders and API Gets in steady state — consistent with
	// how NewGCAction guards its own run.
	//
	// Trade-off: a transient Get/Delete error here won't be retried until rr.Generated becomes
	// true again (next spec change or watch event from a deleting owned resource). This is
	// acceptable because deletion events from the excluded chart's resources (e.g. sail-operator
	// Deployment terminating) naturally trigger re-reconciliation via dynamic ownership watches,
	// setting rr.Generated = true on the next cycle.
	//
	// Future possible improvement: track cleanup completion state (e.g. a status condition) so this
	// guard can be tightened to "skip only when cleanup is known-complete", rather than
	// "skip whenever the rendered hash is unchanged".
	if len(charts) == 0 || !rr.Generated {
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

	for i := range resources {
		obj := &resources[i]
		objGVK := obj.GroupVersionKind()

		if _, skip := unremovables[objGVK]; skip {
			continue
		}

		live := &unstructured.Unstructured{}
		live.SetGroupVersionKind(objGVK)

		err := rr.Client.Get(ctx, client.ObjectKeyFromObject(obj), live)
		if err != nil {
			if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
				continue
			}

			l.Error(err, "cleanup get failed, skipping resource",
				"gvk", objGVK,
				"ns", obj.GetNamespace(),
				"name", obj.GetName(),
			)
			merr = multierror.Append(merr, fmt.Errorf("cleanup get failed for %s/%s: %w", obj.GetNamespace(), obj.GetName(), err))

			continue
		}

		owned := false
		for _, ref := range live.GetOwnerReferences() {
			if ref.UID == rr.Instance.GetUID() {
				owned = true

				break
			}
		}

		if !owned {
			l.V(1).Info("resource not owned by this instance, skipping cleanup",
				"gvk", objGVK,
				"ns", live.GetNamespace(),
				"name", live.GetName(),
			)

			continue
		}

		l.Info("cleanup excluded chart resource",
			"gvk", objGVK,
			"ns", live.GetNamespace(),
			"name", live.GetName(),
		)

		if err := rr.Client.Delete(ctx, live, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
			if k8serr.IsNotFound(err) {
				continue
			}

			l.Error(err, "cleanup delete failed, skipping resource",
				"gvk", objGVK,
				"ns", live.GetNamespace(),
				"name", live.GetName(),
			)
			merr = multierror.Append(merr, fmt.Errorf("cleanup delete failed for %s/%s: %w", live.GetNamespace(), live.GetName(), err))
		}
	}

	return merr.ErrorOrNil()
}

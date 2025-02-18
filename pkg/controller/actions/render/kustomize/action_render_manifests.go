package kustomize

import (
	"context"
	"fmt"

	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/resourcecacher"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/manifests/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const rendererEngine = "kustomize"

// Action takes a set of manifest locations and render them as Unstructured resources for
// further processing. The Action can eventually cache the results in memory to avoid doing
// a full manifest rendering when not needed.
type Action struct {
	resourcecacher.ResourceCacher

	keOpts     []kustomize.EngineOptsFn
	ke         *kustomize.Engine
	nsSelector func(context.Context, *types.ReconciliationRequest) (string, error)
}

type ActionOpts func(*Action)

func WithEngineFS(value filesys.FileSystem) ActionOpts {
	return func(a *Action) {
		a.keOpts = append(a.keOpts, kustomize.WithEngineFS(value))
	}
}

func WithLabel(name string, value string) ActionOpts {
	return func(a *Action) {
		a.keOpts = append(a.keOpts, kustomize.WithEngineRenderOpts(kustomize.WithLabel(name, value)))
	}
}

func WithLabels(values map[string]string) ActionOpts {
	return func(a *Action) {
		a.keOpts = append(a.keOpts, kustomize.WithEngineRenderOpts(kustomize.WithLabels(values)))
	}
}

func WithAnnotation(name string, value string) ActionOpts {
	return func(a *Action) {
		a.keOpts = append(a.keOpts, kustomize.WithEngineRenderOpts(kustomize.WithAnnotation(name, value)))
	}
}

func WithAnnotations(values map[string]string) ActionOpts {
	return func(a *Action) {
		a.keOpts = append(a.keOpts, kustomize.WithEngineRenderOpts(kustomize.WithAnnotations(values)))
	}
}

func WithManifestsOptions(values ...kustomize.EngineOptsFn) ActionOpts {
	return func(action *Action) {
		action.keOpts = append(action.keOpts, values...)
	}
}

func WithCache() ActionOpts {
	return func(action *Action) {
		action.ResourceCacher.SetKeyFn(types.Hash)
	}
}

func WithNamespaceSelector(value func(context.Context, *types.ReconciliationRequest) (string, error)) ActionOpts {
	return func(action *Action) {
		action.nsSelector = value
	}
}

func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	return a.ResourceCacher.Render(ctx, rr, a.render)
}

func (a *Action) render(ctx context.Context, rr *types.ReconciliationRequest) (resources.UnstructuredList, error) {
	result := make(resources.UnstructuredList, 0)

	ns, err := a.nsSelector(ctx, rr)
	if err != nil {
		return nil, fmt.Errorf("unable to compute rendering namespace: %w", err)
	}

	for i := range rr.Manifests {
		renderedResources, err := a.ke.Render(
			rr.Manifests[i].String(),
			kustomize.WithNamespace(ns),
		)

		if err != nil {
			return nil, err
		}

		result = append(result, renderedResources...)
	}

	return result, nil
}

func NewAction(opts ...ActionOpts) actions.Fn {
	action := Action{
		ResourceCacher: resourcecacher.NewResourceCacher(rendererEngine),
		nsSelector: func(_ context.Context, rr *types.ReconciliationRequest) (string, error) {
			return rr.DSCI.Spec.ApplicationsNamespace, nil
		},
	}

	for _, opt := range opts {
		opt(&action)
	}

	action.ke = kustomize.NewEngine(action.keOpts...)

	return action.run
}

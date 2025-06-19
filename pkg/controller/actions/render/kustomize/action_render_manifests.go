package kustomize

import (
	"context"

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
	cacher resourcecacher.ResourceCacher
	cache  bool

	keOpts []kustomize.EngineOptsFn
	ke     *kustomize.Engine
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

func WithCache(enabled bool) ActionOpts {
	return func(action *Action) {
		action.cache = enabled
	}
}

func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	return a.cacher.Render(ctx, rr, a.render)
}

func (a *Action) render(_ context.Context, rr *types.ReconciliationRequest) (resources.UnstructuredList, error) {
	result := make(resources.UnstructuredList, 0)

	for i := range rr.Manifests {
		renderedResources, err := a.ke.Render(
			rr.Manifests[i].String(),
			kustomize.WithNamespace(rr.DSCI.Spec.ApplicationsNamespace),
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
		cacher: resourcecacher.NewResourceCacher(rendererEngine),
		cache:  true,
	}

	for _, opt := range opts {
		opt(&action)
	}

	if action.cache {
		action.cacher.SetKeyFn(types.Hash)
	}

	action.ke = kustomize.NewEngine(action.keOpts...)

	return action.run
}

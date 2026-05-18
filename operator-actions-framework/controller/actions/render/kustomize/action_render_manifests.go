package kustomize

import (
	"context"
	"errors"

	"github.com/opendatahub-io/operator-actions-framework/controller/actions"
	"github.com/opendatahub-io/operator-actions-framework/controller/actions/resourcecacher"
	"github.com/opendatahub-io/operator-actions-framework/controller/types"
	"github.com/opendatahub-io/operator-actions-framework/resources"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const rendererEngine = "kustomize"

// Engine defines the interface for a kustomize rendering engine.
// Implementations must be able to render a kustomize path into a set of
// unstructured resources.
type Engine interface {
	Render(path string, opts ...RenderOpt) ([]unstructured.Unstructured, error)
}

// RenderOpt is a functional option for configuring kustomize rendering.
type RenderOpt func(*renderConfig)

type renderConfig struct {
	namespace string
}

// WithNamespace sets the namespace for rendered resources.
func WithNamespace(ns string) RenderOpt {
	return func(c *renderConfig) {
		c.namespace = ns
	}
}

// Action takes a set of manifest locations and render them as Unstructured resources for
// further processing. The Action can eventually cache the results in memory to avoid doing
// a full manifest rendering when not needed.
type Action struct {
	cacher      resourcecacher.ResourceCacher
	cache       bool
	engine      Engine
	namespaceFn actions.Getter[string]
}

type ActionOpts func(*Action)

// WithEngine sets the kustomize engine to use for rendering.
func WithEngine(engine Engine) ActionOpts {
	return func(a *Action) {
		a.engine = engine
	}
}

// WithNamespaceFn sets the function used to determine the target namespace
// for rendered resources.
func WithNamespaceFn(fn actions.Getter[string]) ActionOpts {
	return func(a *Action) {
		if fn != nil {
			a.namespaceFn = fn
		}
	}
}

// WithCache enables or disables caching.
func WithCache(enabled bool) ActionOpts {
	return func(action *Action) {
		action.cache = enabled
	}
}

func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	if rr.SkipDeploy {
		return nil
	}

	return a.cacher.Render(ctx, rr, a.render)
}

func (a *Action) render(ctx context.Context, rr *types.ReconciliationRequest) (resources.UnstructuredList, error) {
	if a.engine == nil {
		return nil, errors.New("kustomize engine is not configured")
	}

	result := make(resources.UnstructuredList, 0)

	var renderOpts []RenderOpt
	if a.namespaceFn != nil {
		appNamespace, err := a.namespaceFn(ctx, rr)
		if err != nil {
			return nil, err
		}
		renderOpts = append(renderOpts, WithNamespace(appNamespace))
	}

	for i := range rr.Manifests {
		perManifestOpts := renderOpts
		if rr.Manifests[i].Namespace != "" {
			perManifestOpts = append(append([]RenderOpt{}, renderOpts...), WithNamespace(rr.Manifests[i].Namespace))
		}

		renderedResources, err := a.engine.Render(
			rr.Manifests[i].String(),
			perManifestOpts...,
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

	return action.run
}

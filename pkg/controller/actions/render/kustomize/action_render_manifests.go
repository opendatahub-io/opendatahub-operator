package kustomize

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/manifests/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const RendererEngine = "kustomize"

// Action takes a set of manifest locations and render them as Unstructured resources for
// further processing. The Action can eventually cache the results in memory to avoid doing
// a full manifest rendering when not needed.
type Action struct {
	keOpts []kustomize.EngineOptsFn
	ke     *kustomize.Engine

	cachingKeyFn    render.CachingKeyFn
	cachingKey      []byte
	cachedResources resources.UnstructuredList
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
		action.cachingKeyFn = types.Hash
	}
}

func (a *Action) run(_ context.Context, rr *types.ReconciliationRequest) error {
	var err error
	var cachingKey []byte

	inst, ok := rr.Instance.(common.WithDevFlags)
	if ok && inst.GetDevFlags() != nil {
		// if dev flags are enabled, caching is disabled as dev flags are meant for
		// development time only where caching is not relevant
		a.cachingKey = nil
	} else {
		cachingKey, err = a.cachingKeyFn(rr)
		if err != nil {
			return fmt.Errorf("unable to calculate checksum of reconciliation object: %w", err)
		}
	}

	var result resources.UnstructuredList

	if len(cachingKey) != 0 && bytes.Equal(cachingKey, a.cachingKey) && len(a.cachedResources) != 0 {
		result = a.cachedResources
	} else {
		res, err := a.render(rr)
		if err != nil {
			return fmt.Errorf("unable to render reconciliation object: %w", err)
		}

		result = res

		if len(cachingKey) != 0 {
			a.cachingKey = cachingKey
			a.cachedResources = result
		}

		controllerName := strings.ToLower(rr.Instance.GetObjectKind().GroupVersionKind().Kind)
		render.RenderedResourcesTotal.WithLabelValues(controllerName, RendererEngine).Add(float64(len(result)))

		rr.Generated = true
	}

	// deep copy object so changes done in the pipelines won't
	// alter them
	rr.Resources = append(rr.Resources, result.Clone()...)

	return nil
}

func (a *Action) render(rr *types.ReconciliationRequest) ([]unstructured.Unstructured, error) {
	result := make([]unstructured.Unstructured, 0)

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
		cachingKeyFn: func(rr *types.ReconciliationRequest) ([]byte, error) {
			return nil, nil
		},
	}

	for _, opt := range opts {
		opt(&action)
	}

	action.ke = kustomize.NewEngine(action.keOpts...)

	return action.run
}

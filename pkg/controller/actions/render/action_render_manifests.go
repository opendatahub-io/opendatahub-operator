package render

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/manifests/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

type CachingKeyFn func(_ context.Context, rr *types.ReconciliationRequest) ([]byte, error)

// Action takes a set of manifest locations and render them as Unstructured resources for
// further processing. The Action can eventually cache the results in memory to avoid doing
// a full manifest rendering when not needed.
type Action struct {
	keOpts []kustomize.EngineOptsFn
	ke     *kustomize.Engine

	cacheAnnotation bool
	cachingKeyFn    CachingKeyFn
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

func WithCache(addHashAnnotation bool, value CachingKeyFn) ActionOpts {
	return func(action *Action) {
		action.cacheAnnotation = addHashAnnotation
		action.cachingKeyFn = value
	}
}

func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	var err error
	var cachingKey []byte

	if rr.Instance.GetDevFlags() == nil {
		cachingKey, err = a.cachingKeyFn(ctx, rr)
		if err != nil {
			return fmt.Errorf("unable to calculate checksum of reconciliation object: %w", err)
		}
	} else {
		// if dev flags are enabled, caching is disabled as dev flags are meant for
		// development time only where caching is not relevant
		a.cachingKey = nil
	}

	var result resources.UnstructuredList

	if len(cachingKey) != 0 && bytes.Equal(cachingKey, a.cachingKey) && len(a.cachedResources) != 0 {
		result = a.cachedResources
	} else {
		res, err := a.render(rr)
		if err != nil {
			return fmt.Errorf("unable to render reconciliation object: %w", err)
		}

		// Add a letter at the beginning and use URL safe encoding
		digest := "v" + base64.RawURLEncoding.EncodeToString(cachingKey)
		result = res

		if len(cachingKey) != 0 {
			if a.cacheAnnotation {
				for i := range result {
					resources.SetAnnotation(&result[i], annotations.ComponentHash, digest)
				}
			}

			a.cachingKey = cachingKey
			a.cachedResources = result
		}
	}

	// deep copy object so changes done in the pipelines won't
	// alter them
	rr.Resources = result.Clone()

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
		cachingKeyFn: func(_ context.Context, rr *types.ReconciliationRequest) ([]byte, error) {
			return nil, nil
		},
	}

	for _, opt := range opts {
		opt(&action)
	}

	action.ke = kustomize.NewEngine(action.keOpts...)

	return action.run
}

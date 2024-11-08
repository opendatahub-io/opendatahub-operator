package template

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"strings"
	gt "text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const RendererEngine = "template"

// Action takes a set of template locations and render them as Unstructured resources for
// further processing. The Action can eventually cache the results in memory to avoid doing
// a full manifest rendering when not needed.
type Action struct {
	cachingKeyFn    render.CachingKeyFn
	cachingKey      []byte
	cachedResources resources.UnstructuredList
}
type ActionOpts func(*Action)

func WithCache(value render.CachingKeyFn) ActionOpts {
	return func(action *Action) {
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

		result = res

		if len(cachingKey) != 0 {
			a.cachingKey = cachingKey
			a.cachedResources = result
		}

		controllerName := strings.ToLower(rr.Instance.GetObjectKind().GroupVersionKind().Kind)
		render.RenderedResourcesTotal.WithLabelValues(controllerName, RendererEngine).Add(float64(len(result)))
	}

	// deep copy object so changes done in the pipelines won't
	// alter them
	rr.Resources = append(rr.Resources, result.Clone()...)

	return nil
}

func (a *Action) render(rr *types.ReconciliationRequest) ([]unstructured.Unstructured, error) {
	decoder := serializer.NewCodecFactory(rr.Client.Scheme()).UniversalDeserializer()
	data := map[string]any{"Instance": rr.Instance, "DSCI": rr.DSCI}

	result := make([]unstructured.Unstructured, 0)

	var buffer bytes.Buffer

	for i := range rr.Templates {
		content, err := fs.ReadFile(rr.Templates[i].FS, rr.Templates[i].Path)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}

		tmpl, err := gt.New(rr.Templates[i].Path).
			Option("missingkey=error").
			Parse(string(content))

		if err != nil {
			return nil, fmt.Errorf("failed to parse template: %w", err)
		}

		buffer.Reset()
		err = tmpl.Execute(&buffer, data)
		if err != nil {
			return nil, fmt.Errorf("failed to execute template: %w", err)
		}

		u, err := resources.Decode(decoder, buffer.Bytes())
		if err != nil {
			return nil, fmt.Errorf("failed to decode template: %w", err)
		}

		result = append(result, u...)
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

	return action.run
}

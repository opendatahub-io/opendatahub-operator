package template

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"maps"
	"strings"
	gt "text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	RendererEngine = "template"
	ComponentKey   = "Component"
	DSCIKey        = "DSCI"
)

// Action takes a set of template locations and render them as Unstructured resources for
// further processing. The Action can eventually cache the results in memory to avoid doing
// a full manifest rendering when not needed.
type Action struct {
	cachingKeyFn    render.CachingKeyFn
	cachingKey      []byte
	cachedResources resources.UnstructuredList
	data            map[string]any
	dataFn          []func(context.Context, *types.ReconciliationRequest) (map[string]any, error)
}

type ActionOpts func(*Action)

func WithCache() ActionOpts {
	return func(action *Action) {
		action.cachingKeyFn = types.Hash
	}
}

func WithData(data map[string]any) ActionOpts {
	return func(action *Action) {
		for k, v := range data {
			action.data[k] = v
		}
	}
}

func WithDataFn(fns ...func(context.Context, *types.ReconciliationRequest) (map[string]any, error)) ActionOpts {
	return func(action *Action) {
		action.dataFn = append(action.dataFn, fns...)
	}
}

func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
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
		res, err := a.render(ctx, rr)
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

func (a *Action) render(ctx context.Context, rr *types.ReconciliationRequest) ([]unstructured.Unstructured, error) {
	decoder := serializer.NewCodecFactory(rr.Client.Scheme()).UniversalDeserializer()

	data := maps.Clone(a.data)

	for _, fn := range a.dataFn {
		values, err := fn(ctx, rr)
		if err != nil {
			return nil, fmt.Errorf("unable to compute template data: %w", err)
		}

		maps.Copy(data, values)
	}

	data[ComponentKey] = rr.Instance
	data[DSCIKey] = rr.DSCI

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
		cachingKeyFn: func(rr *types.ReconciliationRequest) ([]byte, error) {
			return nil, nil
		},
		data: make(map[string]any),
	}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}

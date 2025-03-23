package template

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	gt "text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/resourcecacher"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	rendererEngine = "template"
	ComponentKey   = "Component"
	DSCIKey        = "DSCI"
)

// Action takes a set of template locations and render them as Unstructured resources for
// further processing. The Action can eventually cache the results in memory to avoid doing
// a full manifest rendering when not needed.
type Action struct {
	resourcecacher.ResourceCacher

	data   map[string]any
	dataFn []func(context.Context, *types.ReconciliationRequest) (map[string]any, error)

	labels      map[string]string
	annotations map[string]string
}

type ActionOpts func(*Action)

func WithCache() ActionOpts {
	return func(action *Action) {
		action.ResourceCacher.SetKeyFn(types.Hash)
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

func WithLabel(name string, value string) ActionOpts {
	return func(a *Action) {
		a.labels[name] = value
	}
}

func WithLabels(values map[string]string) ActionOpts {
	return func(a *Action) {
		maps.Copy(a.labels, values)
	}
}

func WithAnnotation(name string, value string) ActionOpts {
	return func(a *Action) {
		a.annotations[name] = value
	}
}

func WithAnnotations(values map[string]string) ActionOpts {
	return func(a *Action) {
		maps.Copy(a.annotations, values)
	}
}

func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	return a.ResourceCacher.Render(ctx, rr, a.render)
}

func (a *Action) decode(decoder runtime.Decoder, data []byte, info types.TemplateInfo) ([]unstructured.Unstructured, error) {
	u, err := resources.Decode(decoder, data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode template: %w", err)
	}

	for i := range u {
		resources.SetLabels(&u[i], a.labels)
		resources.SetAnnotations(&u[i], a.annotations)

		resources.SetLabels(&u[i], info.Labels)
		resources.SetAnnotations(&u[i], info.Annotations)
	}

	return u, err
}

func (a *Action) render(ctx context.Context, rr *types.ReconciliationRequest) (resources.UnstructuredList, error) {
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

	result := make(resources.UnstructuredList, 0)

	var buffer bytes.Buffer

	for i := range rr.Templates {
		tmpl, err := gt.ParseFS(rr.Templates[i].FS, rr.Templates[i].Path)
		if err != nil {
			return nil, fmt.Errorf("failed to parse template from: %w", err)
		}

		tmpl = tmpl.Option("missingkey=error")

		for _, t := range tmpl.Templates() {
			buffer.Reset()
			err = t.Execute(&buffer, data)
			if err != nil {
				return nil, fmt.Errorf("failed to execute template: %w", err)
			}

			u, err := a.decode(decoder, buffer.Bytes(), rr.Templates[i])
			if err != nil {
				return nil, fmt.Errorf("failed to decode template: %w", err)
			}

			result = append(result, u...)
		}
	}

	return result, nil
}

func NewAction(opts ...ActionOpts) actions.Fn {
	action := Action{
		data:           make(map[string]any),
		ResourceCacher: resourcecacher.NewResourceCacher(rendererEngine),
		labels:         make(map[string]string),
		annotations:    make(map[string]string),
	}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}

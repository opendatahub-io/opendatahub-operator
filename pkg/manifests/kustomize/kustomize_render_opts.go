package kustomize

import (
	"sigs.k8s.io/kustomize/api/resmap"
	kyaml "sigs.k8s.io/kustomize/kyaml/yaml"
)

type FilterFn func(nodes []*kyaml.RNode) ([]*kyaml.RNode, error)

type renderOpts struct {
	kustomizationFileName    string
	kustomizationFileOverlay string
	ns                       string
	labels                   map[string]string
	annotations              map[string]string
	plugins                  []resmap.Transformer
}

type RenderOptsFn func(*renderOpts)

func WithKustomizationFileName(value string) RenderOptsFn {
	return func(opts *renderOpts) {
		opts.kustomizationFileName = value
	}
}

func WithKustomizationOverlayPath(value string) RenderOptsFn {
	return func(opts *renderOpts) {
		opts.kustomizationFileOverlay = value
	}
}

func WithNamespace(value string) RenderOptsFn {
	return func(opts *renderOpts) {
		opts.ns = value
	}
}

func WithLabel(name string, value string) RenderOptsFn {
	return func(opts *renderOpts) {
		if opts.labels == nil {
			opts.labels = map[string]string{}
		}

		opts.labels[name] = value
	}
}

func WithLabels(values map[string]string) RenderOptsFn {
	return func(opts *renderOpts) {
		if opts.labels == nil {
			opts.labels = map[string]string{}
		}

		for k, v := range values {
			opts.labels[k] = v
		}
	}
}

func WithAnnotation(name string, value string) RenderOptsFn {
	return func(opts *renderOpts) {
		if opts.annotations == nil {
			opts.annotations = map[string]string{}
		}

		opts.annotations[name] = value
	}
}

func WithAnnotations(values map[string]string) RenderOptsFn {
	return func(opts *renderOpts) {
		if opts.annotations == nil {
			opts.annotations = map[string]string{}
		}

		for k, v := range values {
			opts.annotations[k] = v
		}
	}
}

func WithPlugin(value resmap.Transformer) RenderOptsFn {
	return func(opts *renderOpts) {
		opts.plugins = append(opts.plugins, value)
	}
}

func WithFilter(value FilterFn) RenderOptsFn {
	return func(opts *renderOpts) {
		opts.plugins = append(opts.plugins, &filterPlugin{f: value})
	}
}

func WithFilters(values ...FilterFn) RenderOptsFn {
	return func(opts *renderOpts) {
		for i := range values {
			opts.plugins = append(opts.plugins, &filterPlugin{f: values[i]})
		}
	}
}

package helm

import (
	"context"
	"maps"
	"slices"

	"github.com/k8s-manifest-kit/engine/pkg/transformer/meta/annotations"
	"github.com/k8s-manifest-kit/engine/pkg/transformer/meta/labels"
	engineTypes "github.com/k8s-manifest-kit/engine/pkg/types"
	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/resourcecacher"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const rendererEngine = "helm"

// ChartReleaseAnnotation is used to tag rendered resources with their source chart.
const ChartReleaseAnnotation = "opendatahub.io/helm-release"

// Action takes a set of Helm chart specifications and renders them as Unstructured resources
// for further processing. The Action can cache the results in memory to avoid doing
// a full manifest rendering when not needed.
type Action struct {
	cacher       resourcecacher.ResourceCacher
	cache        bool
	labels       map[string]string
	annotations  map[string]string
	transformers []engineTypes.Transformer
}

type ActionOpts func(*Action)

// WithLabel adds a label to all rendered resources.
func WithLabel(name, value string) ActionOpts {
	return func(a *Action) {
		if a.labels == nil {
			a.labels = make(map[string]string)
		}
		a.labels[name] = value
	}
}

// WithLabels adds multiple labels to all rendered resources.
func WithLabels(values map[string]string) ActionOpts {
	return func(a *Action) {
		if a.labels == nil {
			a.labels = make(map[string]string)
		}
		maps.Copy(a.labels, values)
	}
}

// WithAnnotation adds an annotation to all rendered resources.
func WithAnnotation(name, value string) ActionOpts {
	return func(a *Action) {
		if a.annotations == nil {
			a.annotations = make(map[string]string)
		}
		a.annotations[name] = value
	}
}

// WithAnnotations adds multiple annotations to all rendered resources.
func WithAnnotations(values map[string]string) ActionOpts {
	return func(a *Action) {
		if a.annotations == nil {
			a.annotations = make(map[string]string)
		}
		maps.Copy(a.annotations, values)
	}
}

// WithCache enables or disables caching.
func WithCache(enabled bool) ActionOpts {
	return func(a *Action) {
		a.cache = enabled
	}
}

func WithTransformer(transformer engineTypes.Transformer) ActionOpts {
	return func(a *Action) {
		a.transformers = append(a.transformers, transformer)
	}
}

func WithTransformers(transformers ...engineTypes.Transformer) ActionOpts {
	return func(a *Action) {
		a.transformers = append(a.transformers, transformers...)
	}
}

func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	return a.cacher.Render(ctx, rr, a.render)
}

func (a *Action) render(ctx context.Context, rr *types.ReconciliationRequest) (resources.UnstructuredList, error) {
	result := make(resources.UnstructuredList, 0)

	charts := make([]helm.Source, 0, len(rr.HelmCharts))
	for _, chart := range rr.HelmCharts {
		charts = append(charts, chart.Source)
	}

	helmOptions := helm.RendererOptions{
		Strict:       true,
		Transformers: slices.Clone(a.transformers),
		// TODO: add source annotations. Before it, make the source annotations configurable.
	}

	if a.annotations != nil {
		helmOptions.Transformers = append(helmOptions.Transformers, annotations.Set(a.annotations))
	}
	if a.labels != nil {
		helmOptions.Transformers = append(helmOptions.Transformers, labels.Set(a.labels))
	}

	renderer, err := helm.New(charts, helmOptions)
	if err != nil {
		return nil, err
	}

	// TODO: manage render time values
	objects, err := renderer.Process(ctx, map[string]any{})
	if err != nil {
		return nil, err
	}

	result = append(result, objects...)

	// Sort resources to ensure proper installation order (CRDs before CRs)
	sortByInstallOrder(result)

	return result, nil
}

// sortByInstallOrder sorts resources so that CRDs come before other resources.
// This ensures that CRDs are installed before any CRs that depend on them.
func sortByInstallOrder(resources []unstructured.Unstructured) {
	slices.SortStableFunc(resources, func(a, b unstructured.Unstructured) int {
		aIsCRD := a.GetKind() == "CustomResourceDefinition"
		bIsCRD := b.GetKind() == "CustomResourceDefinition"

		switch {
		case aIsCRD && !bIsCRD:
			return -1
		case !aIsCRD && bIsCRD:
			return 1
		default:
			return 0
		}
	})
}

// NewAction creates a new Helm rendering action.
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

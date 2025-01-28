package resourcecacher

import (
	"fmt"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/cacher"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type ResourceCacher struct {
	cacher.Cacher
}

func (s *ResourceCacher) SetKey(key cacher.CachingKeyFn) {
	s.Cacher.SetKey(key)
}

func (*ResourceCacher) Name() string {
	return "ResourceCacher"
}

func (s *ResourceCacher) Render(r cacher.Renderer, rr *types.ReconciliationRequest) (any, bool, error) {
	inst, ok := rr.Instance.(common.WithDevFlags)
	if ok && inst.GetDevFlags() != nil {
		// if dev flags are enabled, caching is disabled as dev flags are meant for
		// development time only where caching is not relevant
		s.InvalidateCache()
	}

	res, acted, err := s.Cacher.Render(r, rr)
	if err != nil {
		return nil, acted, err
	}

	resUnstructured, ok := res.([]unstructured.Unstructured)
	if !ok {
		return nil, acted, fmt.Errorf("got wrong resource type")
	}
	// type assertion for type alias does not work
	resList := resources.UnstructuredList(resUnstructured)

	if acted {
		controllerName := strings.ToLower(rr.Instance.GetObjectKind().GroupVersionKind().Kind)
		render.RenderedResourcesTotal.WithLabelValues(controllerName, s.Name()).Add(float64(len(resList)))

		rr.Generated = true
	}

	// deep copy object so changes done in the pipelines won't
	// alter them
	rr.Resources = append(rr.Resources, resList.Clone()...)

	return res, acted, nil
}

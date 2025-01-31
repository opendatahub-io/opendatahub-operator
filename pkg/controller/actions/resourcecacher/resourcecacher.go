package resourcecacher

import (
	"context"
	"errors"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/cacher"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

type ResourceCacher struct {
	cacher.Cacher
}

func (s *ResourceCacher) SetKeyFn(key cacher.CachingKeyFn) {
	s.Cacher.SetKeyFn(key)
}

func (*ResourceCacher) Name() string {
	return "ResourceCacher"
}

func (s *ResourceCacher) Render(ctx context.Context, r cacher.Renderer, rr *types.ReconciliationRequest) error {
	log := logf.FromContext(ctx)
	inst, ok := rr.Instance.(common.WithDevFlags)
	if ok && inst.GetDevFlags() != nil {
		// if dev flags are enabled, caching is disabled as dev flags are meant for
		// development time only where caching is not relevant
		log.V(4).Info("devFlags enabled, invalidating resource cache")
		s.InvalidateCache()
	}

	res, acted, err := s.Cacher.Render(ctx, r, rr)
	if err != nil {
		return err
	}

	// supposed to be used by actions, which append resources (resources.UnstructuredList)
	// to the ReconcilitaionRequest (rr.Resources)
	resUnstructured, ok := res.([]unstructured.Unstructured)
	if !ok {
		return errors.New("got wrong resource type")
	}
	// type assertion for type alias does not work
	resList := resources.UnstructuredList(resUnstructured)
	resLen := len(resList)

	if acted {
		log.V(4).Info("accounted rendered resources", "count", resLen)

		controllerName := strings.ToLower(rr.Instance.GetObjectKind().GroupVersionKind().Kind)
		render.RenderedResourcesTotal.WithLabelValues(controllerName, s.Name()).Add(float64(resLen))

		// flag new resources, used by GC to avoid useless run
		rr.Generated = true
	}

	// deep copy object so changes done in the pipelines won't
	// alter them
	rr.Resources = append(rr.Resources, resList.Clone()...)
	log.V(4).Info("added resources to the request", "count", resLen)

	return nil
}

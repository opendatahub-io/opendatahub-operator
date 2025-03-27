package resourcecacher

import (
	"context"
	"strings"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/cacher"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// Renderer is the actual function received from upper layers
// which generates resources based on the ReconciliationRequest.
type Renderer func(ctx context.Context, rr *types.ReconciliationRequest) (resources.UnstructuredList, error)
type ResourceCacher struct {
	cacher.Cacher[resources.UnstructuredList]
	name string
}

func (s *ResourceCacher) SetKeyFn(key cacher.CachingKeyFn) {
	s.Cacher.SetKeyFn(key)
}

func (s *ResourceCacher) Render(ctx context.Context, rr *types.ReconciliationRequest, r Renderer) error {
	log := logf.FromContext(ctx)
	inst, ok := rr.Instance.(common.WithDevFlags)
	if ok && inst.GetDevFlags() != nil {
		// if dev flags are enabled, caching is disabled as dev flags are meant for
		// development time only where caching is not relevant
		log.V(4).Info("devFlags enabled, invalidating resource cache")
		s.InvalidateCache()
	}

	res, acted, err := s.Cacher.Render(ctx, rr, r)
	if err != nil {
		return err
	}

	resLen := len(res)

	if acted {
		log.V(4).Info("accounted rendered resources", "count", resLen)

		controllerName := strings.ToLower(rr.Instance.GetObjectKind().GroupVersionKind().Kind)
		render.RenderedResourcesTotal.WithLabelValues(controllerName, s.name).Add(float64(resLen))

		// flag new resources, used by GC to avoid useless run
		rr.Generated = true
	}

	// deep copy object so changes done in the pipelines won't
	// alter them
	rr.Resources = append(rr.Resources, res.Clone()...)
	log.V(4).Info("added resources to the request", "count", resLen)

	return nil
}

func NewResourceCacher(name string) ResourceCacher {
	return ResourceCacher{name: name}
}

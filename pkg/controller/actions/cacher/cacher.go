package cacher

import (
	"bytes"
	"context"
	"fmt"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// Renderer is the actual function received from upper layers
// which generates resources based on the ReconciliationRequest.
// Resource can be any type, so it returns `any` (or `error`)
// The upper layers passing the function and can cast it to the expected type.
type Renderer func(ctx context.Context, rr *types.ReconciliationRequest) (any, error)

type CachingKeyFn func(rr *types.ReconciliationRequest) ([]byte, error)

type Cacher struct {
	cachingKeyFn    CachingKeyFn
	cachingKey      []byte
	cachedResources any
}

type CacherOpts func(*Cacher)

func (s *Cacher) SetKeyFn(key CachingKeyFn) {
	s.cachingKeyFn = key
}

// InvalidateCache invalidates both caching key and cached resources
// what forces Render() to regenerate them.
func (s *Cacher) InvalidateCache() {
	s.cachingKey = nil
	s.cachedResources = nil
}

func (s *Cacher) Render(ctx context.Context, r Renderer, rr *types.ReconciliationRequest) (any, bool, error) {
	var err error
	var cachingKey []byte
	log := logf.FromContext(ctx)

	if s.cachingKeyFn != nil {
		cachingKey, err = s.cachingKeyFn(rr)
	}
	if err != nil {
		return nil, false, fmt.Errorf("unable to calculate checksum of reconciliation object: %w", err)
	}

	if len(cachingKey) != 0 && bytes.Equal(cachingKey, s.cachingKey) && s.cachedResources != nil {
		log.V(4).Info("using cached resources")
		return s.cachedResources, false, nil
	}

	log.V(4).Info("cache is not valid, rendering resources")
	res, err := r(ctx, rr)
	if err != nil {
		return nil, false, err
	}

	if len(cachingKey) != 0 {
		s.cachingKey = cachingKey
		s.cachedResources = res
	}
	return res, true, nil
}

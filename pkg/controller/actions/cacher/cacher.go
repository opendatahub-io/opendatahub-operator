package cacher

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type CachingKeyFn func(rr *types.ReconciliationRequest) ([]byte, error)

type Cacher[T any] struct {
	cachingKeyFn    CachingKeyFn
	cachingKey      []byte
	cachedResources T
}

func Zero[T any]() T {
	return *new(T)
}

// SetKeyFn installs the function which calculates hash of resource sources,
// taking ReconciliationRequest as the argument and returning []byte
// The returned hash MUST NOT be empty by the contract.
func (s *Cacher[T]) SetKeyFn(key CachingKeyFn) {
	s.cachingKeyFn = key
}

// InvalidateCache invalidates both caching key and cached resources
// what forces Render() to regenerate them.
func (s *Cacher[T]) InvalidateCache() {
	s.cachingKey = nil
}

func (s *Cacher[T]) reRender(ctx context.Context, cachingKey []byte, rr *types.ReconciliationRequest,
	r func(ctx context.Context, rr *types.ReconciliationRequest) (T, error)) (T, bool, error) {
	var err error
	log := logf.FromContext(ctx)

	log.V(4).Info("cache is not valid, rendering resources")
	res, err := r(ctx, rr)
	if err != nil {
		return Zero[T](), false, err
	}

	// does not matter at this point if it's empty or not, because the next run:
	// if the keyFn is nil, anyway rerender
	// if the keyFn returns empty, it's a error
	// otherwise comparing non-empty new key with empty will cause rerender.
	s.cachingKey = cachingKey
	s.cachedResources = res

	return res, true, nil
}

func (s *Cacher[T]) Render(ctx context.Context, rr *types.ReconciliationRequest,
	r func(ctx context.Context, rr *types.ReconciliationRequest) (T, error)) (T, bool, error) {
	var err error
	var cachingKey []byte
	log := logf.FromContext(ctx)

	if s.cachingKeyFn == nil {
		return s.reRender(ctx, nil, rr, r)
	}

	cachingKey, err = s.cachingKeyFn(rr)
	if err != nil {
		return Zero[T](), false, fmt.Errorf("unable to calculate caching key: %w", err)
	}
	// Contract
	if len(cachingKey) == 0 {
		return Zero[T](), false, errors.New("calculated empty hash")
	}

	// s.cachingKey can be empty (invalidated) causing cache rebuild
	if bytes.Equal(cachingKey, s.cachingKey) {
		log.V(4).Info("using cached resources")
		return s.cachedResources, false, nil
	}

	return s.reRender(ctx, cachingKey, rr, r)
}

package cacher

import (
	"bytes"
	"fmt"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type Renderer interface {
	Render(r Renderer, rr *types.ReconciliationRequest) (any, bool, error)
}

type CachingKeyFn func(rr *types.ReconciliationRequest) ([]byte, error)

type Cacher struct {
	cachingKeyFn    CachingKeyFn
	cachingKey      []byte
	cachedResources any
}

type CacherOpts func(*Cacher)

func (s *Cacher) SetKey(key CachingKeyFn) {
	s.cachingKeyFn = key
}

func (s *Cacher) InvalidateCache() {
	s.cachingKey = nil
}

func (s *Cacher) Render(r Renderer, rr *types.ReconciliationRequest) (any, bool, error) {
	var err error
	var cachingKey []byte

	if s.cachingKeyFn != nil {
		cachingKey, err = s.cachingKeyFn(rr)
	}
	if err != nil {
		return nil, false, fmt.Errorf("unable to calculate checksum of reconciliation object: %w", err)
	}

	if len(cachingKey) != 0 && bytes.Equal(cachingKey, s.cachingKey) && s.cachedResources != nil {
		return s.cachedResources, false, nil
	}
	res, _, err := r.Render(nil, rr)
	if err != nil {
		return nil, false, err
	}

	if len(cachingKey) != 0 {
		s.cachingKey = cachingKey
		s.cachedResources = res
	}
	return res, true, nil
}

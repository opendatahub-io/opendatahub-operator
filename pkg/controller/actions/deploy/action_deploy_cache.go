package deploy

import (
	"errors"
	"time"

	"k8s.io/client-go/tools/cache"
)

// This code is heavily inspired by https://github.com/kubernetes-sigs/cluster-api/tree/main/internal/util/ssa

const (
	DefaultCacheTTL = 10 * time.Minute
)

type Cache struct {
	s   cache.Store
	ttl time.Duration
}

type CacheOpt func(*Cache)

func WithTTL(ttl time.Duration) CacheOpt {
	return func(c *Cache) {
		c.ttl = ttl
	}
}

func newCache(opts ...CacheOpt) *Cache {
	c := Cache{
		ttl: DefaultCacheTTL,
	}

	for _, opt := range opts {
		opt(&c)
	}

	c.s = cache.NewTTLStore(
		func(obj interface{}) (string, error) {
			s, ok := obj.(string)
			if !ok {
				return "", errors.New("failed to cast object to string")
			}

			return s, nil
		},
		c.ttl,
	)

	return &c
}

func (r *Cache) Add(key string) {
	if key == "" {
		return
	}

	_ = r.s.Add(key)
}

func (r *Cache) Has(key string) bool {
	if key == "" {
		return false
	}

	_, exists, _ := r.s.GetByKey(key)
	return exists
}

func (r *Cache) Remove(key string) {
	if key == "" {
		return
	}

	_ = r.s.Delete(key)
}

func (r *Cache) Sync() {
	r.s.List()
}

package deploy

import (
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
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

func (r *Cache) Add(original *unstructured.Unstructured, modified *unstructured.Unstructured) error {
	if original == nil || modified == nil {
		return errors.New("invalid input")
	}

	key, err := r.computeCacheKey(original, modified)
	if err != nil {
		return fmt.Errorf("failed to compute cacheKey: %w", err)
	}

	if key == "" {
		return nil
	}

	_ = r.s.Add(key)

	return nil
}

func (r *Cache) Has(original *unstructured.Unstructured, modified *unstructured.Unstructured) (bool, error) {
	if original == nil || modified == nil {
		return false, nil
	}

	key, err := r.computeCacheKey(original, modified)
	if err != nil {
		return false, fmt.Errorf("failed to compute cacheKey: %w", err)
	}

	if key == "" {
		return false, nil
	}

	_, exists, _ := r.s.GetByKey(key)

	return exists, nil
}

func (r *Cache) Sync() {
	r.s.List()
}

func (r *Cache) computeCacheKey(
	original *unstructured.Unstructured,
	modified *unstructured.Unstructured,
) (string, error) {
	modifiedObjectHash, err := resources.Hash(modified)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s.%s.%s.%s.%s",
		original.GroupVersionKind().GroupVersion(),
		original.GroupVersionKind().Kind,
		klog.KObj(original),
		original.GetResourceVersion(),
		base64.RawURLEncoding.EncodeToString(modifiedObjectHash),
	), nil
}

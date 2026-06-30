package cacher

import (
	fwcacher "github.com/opendatahub-io/operator-actions-framework/controller/actions/cacher"
)

type CachingKeyFn = fwcacher.CachingKeyFn

type Cacher[T any] = fwcacher.Cacher[T]

func Zero[T any]() T {
	return fwcacher.Zero[T]()
}

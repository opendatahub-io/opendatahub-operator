package deploy

import (
	fwdeploy "github.com/opendatahub-io/operator-actions-framework/controller/actions/deploy"
)

const DefaultCacheTTL = fwdeploy.DefaultCacheTTL

type Cache = fwdeploy.Cache

type CacheOpt = fwdeploy.CacheOpt

var (
	WithTTL  = fwdeploy.WithTTL
	NewCache = fwdeploy.NewCache
)

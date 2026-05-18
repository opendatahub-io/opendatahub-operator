package helm

import (
	fwhelm "github.com/opendatahub-io/operator-actions-framework/controller/actions/render/helm"
)

type Action = fwhelm.Action

type ActionOpts = fwhelm.ActionOpts

var (
	WithLabel        = fwhelm.WithLabel
	WithLabels       = fwhelm.WithLabels
	WithAnnotation   = fwhelm.WithAnnotation
	WithAnnotations  = fwhelm.WithAnnotations
	WithCache        = fwhelm.WithCache
	WithTransformer  = fwhelm.WithTransformer
	WithTransformers = fwhelm.WithTransformers
	NewAction        = fwhelm.NewAction
)

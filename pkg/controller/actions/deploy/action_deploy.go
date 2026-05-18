package deploy

import (
	fwdeploy "github.com/opendatahub-io/operator-actions-framework/controller/actions/deploy"
)

type Mode = fwdeploy.Mode

const (
	ModePatch = fwdeploy.ModePatch
	ModeSSA   = fwdeploy.ModeSSA
)

type CustomizerFn = fwdeploy.CustomizerFn

type SortFn = fwdeploy.SortFn

type Action = fwdeploy.Action

type ActionOpts = fwdeploy.ActionOpts

var (
	WithFieldOwner          = fwdeploy.WithFieldOwner
	WithMode                = fwdeploy.WithMode
	WithPartOfLabel         = fwdeploy.WithPartOfLabel
	WithPartOfLabelDefault  = fwdeploy.WithPartOfLabelDefault
	WithAnnotationPrefix    = fwdeploy.WithAnnotationPrefix
	WithManagedByAnnotation = fwdeploy.WithManagedByAnnotation
	WithLabel               = fwdeploy.WithLabel
	WithLabels              = fwdeploy.WithLabels
	WithAnnotation          = fwdeploy.WithAnnotation
	WithAnnotations         = fwdeploy.WithAnnotations
	WithCache               = fwdeploy.WithCache
	WithSortFn              = fwdeploy.WithSortFn
	WithApplyOrder          = fwdeploy.WithApplyOrder
	WithContinueOnError     = fwdeploy.WithContinueOnError
	WithApplyCustomizer     = fwdeploy.WithApplyCustomizer
	WithPatchCustomizer     = fwdeploy.WithPatchCustomizer
	NewAction               = fwdeploy.NewAction
)

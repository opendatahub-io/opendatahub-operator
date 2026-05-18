package sanitycheck

import (
	fwsc "github.com/opendatahub-io/operator-actions-framework/controller/actions/sanitycheck"
)

type UnwantedResource = fwsc.UnwantedResource

type Action = fwsc.Action

type ActionOpts = fwsc.ActionOpts

var (
	NewAction            = fwsc.NewAction
	WithUnwantedResource = fwsc.WithUnwantedResource
)

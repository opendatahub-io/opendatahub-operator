package rules

import (
	fwrules "github.com/opendatahub-io/operator-actions-framework/rules"
)

const (
	VerbDelete  = fwrules.VerbDelete
	VerbAny     = fwrules.VerbAny
	ResourceAny = fwrules.ResourceAny
)

var (
	RetrieveSelfSubjectRules   = fwrules.RetrieveSelfSubjectRules
	IsResourceMatchingRule     = fwrules.IsResourceMatchingRule
	HasPermissions             = fwrules.HasPermissions
	ComputeAuthorizedResources = fwrules.ComputeAuthorizedResources
	ListAuthorizedResources    = fwrules.ListAuthorizedResources
)

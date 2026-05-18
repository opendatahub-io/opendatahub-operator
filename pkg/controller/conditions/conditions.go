package conditions

import (
	fwcond "github.com/opendatahub-io/operator-actions-framework/controller/conditions"
)

const ConditionReasonNotSet = fwcond.ConditionReasonNotSet

type Option = fwcond.Option

type Manager = fwcond.Manager

var (
	NewManager                       = fwcond.NewManager
	WithReason                       = fwcond.WithReason
	WithMessage                      = fwcond.WithMessage
	WithObservedGeneration           = fwcond.WithObservedGeneration
	WithSeverity                     = fwcond.WithSeverity
	WithError                        = fwcond.WithError
	SetStatusCondition               = fwcond.SetStatusCondition
	RemoveStatusCondition            = fwcond.RemoveStatusCondition
	FindStatusCondition              = fwcond.FindStatusCondition
	IsStatusConditionTrue            = fwcond.IsStatusConditionTrue
	IsStatusConditionFalse           = fwcond.IsStatusConditionFalse
	IsStatusConditionPresentAndEqual = fwcond.IsStatusConditionPresentAndEqual
)

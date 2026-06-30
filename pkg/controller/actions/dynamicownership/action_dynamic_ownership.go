package dynamicownership

import (
	fwdo "github.com/opendatahub-io/operator-actions-framework/controller/actions/dynamicownership"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

type WatchRegisterFunc = fwdo.WatchRegisterFunc

type ResourceMatcher = fwdo.ResourceMatcher

type Option = fwdo.Option

type Action = fwdo.Action

var (
	WithManagedByFalseMatcher = fwdo.WithManagedByFalseMatcher
	WithGVKPredicates         = fwdo.WithGVKPredicates
	WithPreRegistered         = fwdo.WithPreRegistered
	NewAction                 = fwdo.NewAction
)

// DefaultManagedByFalseMatcher returns true if the resource has the
// managed-by-operator annotation set to "false".
var DefaultManagedByFalseMatcher ResourceMatcher = fwdo.DefaultManagedByFalseMatcher(annotations.ManagedByODHOperator)

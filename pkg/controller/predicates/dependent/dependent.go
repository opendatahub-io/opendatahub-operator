package dependent

import (
	fwdep "github.com/opendatahub-io/operator-actions-framework/controller/predicates/dependent"
)

type Predicate = fwdep.Predicate

type PredicateOption = fwdep.PredicateOption

var (
	New              = fwdep.New
	WithWatchDeleted = fwdep.WithWatchDeleted
	WithWatchUpdate  = fwdep.WithWatchUpdate
	WithWatchStatus  = fwdep.WithWatchStatus
)

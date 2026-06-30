package partial

import (
	fwpartial "github.com/opendatahub-io/operator-actions-framework/controller/predicates/partial"
)

type Predicate = fwpartial.Predicate

type PredicateOption = fwpartial.PredicateOption

var (
	New          = fwpartial.New
	WatchDeleted = fwpartial.WatchDeleted
	WatchUpdate  = fwpartial.WatchUpdate
)

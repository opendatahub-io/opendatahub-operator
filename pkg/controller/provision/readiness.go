package provision

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
)

// NewCompositeChecker builds a dag.CompositeChecker from the given
// per-type readiness checkers. Both controllers use the same composite
// to gate on cross-type readiness across the unified DAG.
//
// The concrete ReadinessChecker implementations live in each controller's
// package (internal/controller/components, internal/controller/modules)
// where they have access to handler types. This function composes them.
func NewCompositeChecker(checkers ...dag.ReadinessChecker) dag.CompositeChecker {
	return dag.CompositeChecker(checkers)
}

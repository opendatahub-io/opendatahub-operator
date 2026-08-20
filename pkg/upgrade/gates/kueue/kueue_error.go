package kueue

import (
	"fmt"
	"strings"
)

type UpgradeBlockedError struct {
	QueuedWorkloadsWithRemovedKueue     int
	WorkloadsWithoutKueueNamespaceLabel int
}

func (e *UpgradeBlockedError) Error() string {
	parts := make([]string, 0, 2)
	if e.QueuedWorkloadsWithRemovedKueue > 0 {
		parts = append(parts, fmt.Sprintf(
			"%d kueue-labeled workloads found while Kueue is Removed",
			e.QueuedWorkloadsWithRemovedKueue,
		))
	}
	if e.WorkloadsWithoutKueueNamespaceLabel > 0 {
		parts = append(parts, fmt.Sprintf(
			"%d kueue-labeled workloads found in namespaces without kueue management labels",
			e.WorkloadsWithoutKueueNamespaceLabel,
		))
	}

	return strings.Join(parts, ", ")
}

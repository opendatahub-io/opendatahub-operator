package kueue

import (
	"fmt"
	"strings"
)

type UpgradeBlockedError struct {
	WorkloadsWithoutKueueNamespaceLabel int
}

func (e *UpgradeBlockedError) Error() string {
	parts := make([]string, 0, 1)
	if e.WorkloadsWithoutKueueNamespaceLabel > 0 {
		parts = append(parts, fmt.Sprintf(
			"%d kueue-labeled workloads found in namespaces without kueue management labels",
			e.WorkloadsWithoutKueueNamespaceLabel,
		))
	}

	return strings.Join(parts, ", ")
}

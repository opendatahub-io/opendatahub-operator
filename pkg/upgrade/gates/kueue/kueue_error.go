package kueue

import (
	"fmt"
	"strings"
)

type UpgradeBlockedError struct {
	ManagedStateUnsupported             bool
	MissingKueueOperatorSubscription    bool
	WorkloadsWithoutKueueNamespaceLabel int
}

func (e *UpgradeBlockedError) Error() string {
	parts := make([]string, 0, 3)
	if e.ManagedStateUnsupported {
		parts = append(parts, "Kueue managementState Managed is not supported")
	}
	if e.MissingKueueOperatorSubscription {
		parts = append(parts, "kueue-operator Subscription missing while Kueue managementState is Unmanaged")
	}
	if e.WorkloadsWithoutKueueNamespaceLabel > 0 {
		parts = append(parts, fmt.Sprintf(
			"%d kueue-labeled workloads found in namespaces without kueue management labels",
			e.WorkloadsWithoutKueueNamespaceLabel,
		))
	}

	return strings.Join(parts, ", ")
}

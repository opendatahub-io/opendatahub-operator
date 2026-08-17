package kueue

import "fmt"

type UpgradeBlockedError struct {
	WorkloadsWithoutKueueNamespaceLabel int
}

func (e *UpgradeBlockedError) Error() string {
	return fmt.Sprintf(
		"%d kueue-labeled workloads found in namespaces without kueue management labels",
		e.WorkloadsWithoutKueueNamespaceLabel,
	)
}

package kueueoperator

import "strings"

type UpgradeBlockedError struct {
	ManagedStateUnsupported          bool
	MissingKueueOperatorSubscription bool
}

func (e *UpgradeBlockedError) Error() string {
	parts := make([]string, 0, 2)
	if e.ManagedStateUnsupported {
		parts = append(parts, "Kueue managementState Managed is not supported")
	}
	if e.MissingKueueOperatorSubscription {
		parts = append(parts, "kueue-operator Subscription missing while Kueue managementState is Unmanaged")
	}

	return strings.Join(parts, ", ")
}

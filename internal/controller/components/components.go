package components

import (
	operatorv1 "github.com/openshift/api/operator/v1"
)

// NormalizeManagementState returns the ManagementState or operatorv1.Removed if empty.
func NormalizeManagementState(managementState operatorv1.ManagementState) operatorv1.ManagementState {
	if managementState == "" {
		return operatorv1.Removed
	}
	return managementState
}

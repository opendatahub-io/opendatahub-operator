package components

import (
	"embed"

	operatorv1 "github.com/openshift/api/operator/v1"
)

//go:embed kueue/monitoring
//go:embed trainingoperator/monitoring
//go:embed trainer/monitoring
//go:embed trustyai/monitoring
//go:embed workbenches/monitoring
//go:embed dashboard/monitoring
//go:embed datasciencepipelines/monitoring
//go:embed feastoperator/monitoring
//go:embed kserve/monitoring
//go:embed llamastackoperator/monitoring
//go:embed modelcontroller/monitoring
//go:embed modelregistry/monitoring
//go:embed ray/monitoring
var ComponentRulesFS embed.FS

// NormalizeManagementState returns the ManagementState or operatorv1.Removed if empty.
func NormalizeManagementState(managementState operatorv1.ManagementState) operatorv1.ManagementState {
	if managementState == "" {
		return operatorv1.Removed
	}
	return managementState
}

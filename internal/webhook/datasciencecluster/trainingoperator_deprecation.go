//go:build !nowebhook

package datasciencecluster

import (
	operatorv1 "github.com/openshift/api/operator/v1"
)

// TrainingOperatorReEnablementDenied is the denial message when a user tries to
// re-enable the deprecated TrainingOperator (KFTO v1) on an update.
// The CEL transition rule on DSCTrainingOperator covers the case where the
// field was already present in the stored object, but the API server prunes
// empty sub-objects, so the CEL rule's oldSelf is unavailable when the field
// was previously unset. This webhook check covers that gap.
const TrainingOperatorReEnablementDenied = "TrainingOperator v1 is obsolete in RHOAI 3.6. " +
	"Set managementState to Removed, then delete the TrainingOperator CR to clean up. " +
	"Use Trainer v2 instead."

// IsTrainingOperatorReEnabled reports whether an update is transitioning
// TrainingOperator from a non-Managed state to Managed.
func IsTrainingOperatorReEnabled(oldState, newState operatorv1.ManagementState) bool {
	return newState == operatorv1.Managed && oldState != operatorv1.Managed
}

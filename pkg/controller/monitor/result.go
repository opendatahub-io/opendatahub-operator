package monitor

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CheckResult holds the outcome of a health check.
//
// A check has three possible outcomes, mirroring the Kubernetes condition
// contract:
//
//   - True: the check found positive evidence that the dependency is usable.
//   - False: the check found direct evidence of absence or failure.
//   - Unknown: the evidence is incomplete or cannot be read reliably (for
//     example a CRD that is not yet established, or a required status
//     condition that is missing or itself Unknown).
//
// Missing information must never be reported as healthy, so an indeterminate
// check reports Unknown rather than True.
type CheckResult struct {
	// Pass reports whether the check succeeded. It is true only when the check
	// found positive evidence of health.
	Pass bool

	// Message describes why the check did not pass. It is empty when the check
	// passed.
	Message string

	// Status, when set, is the condition status this result maps to. It exists
	// so a check can distinguish a known-indeterminate outcome
	// (metav1.ConditionUnknown) from a definite failure
	// (metav1.ConditionFalse); both have Pass set to false.
	//
	// When empty, the status is derived from Pass. When set, it takes
	// precedence over Pass. Prefer the [Passed], [Failed] and [Indeterminate]
	// constructors over setting this field directly.
	Status metav1.ConditionStatus
}

// ConditionStatus resolves the condition status this result maps to.
//
// An explicitly set, valid, and consistent Status wins; otherwise the status is
// derived from Pass. Invalid or contradictory values are reported as Unknown so a
// manually constructed result cannot silently mark a dependency healthy or write an
// invalid Kubernetes condition status.
func (r CheckResult) ConditionStatus() metav1.ConditionStatus {
	switch r.Status {
	case "":
		if r.Pass {
			return metav1.ConditionTrue
		}

		return metav1.ConditionFalse
	case metav1.ConditionTrue:
		if r.Pass {
			return metav1.ConditionTrue
		}
	case metav1.ConditionFalse:
		if !r.Pass {
			return metav1.ConditionFalse
		}
	case metav1.ConditionUnknown:
		// Unknown is the safe resolution when the explicit status conflicts with
		// Pass, or when the caller explicitly has indeterminate evidence.
		return metav1.ConditionUnknown
	}

	return metav1.ConditionUnknown
}

// Passed returns a result reporting that the check found positive evidence of health.
func Passed() CheckResult {
	return CheckResult{Pass: true, Status: metav1.ConditionTrue}
}

// Failed returns a result reporting direct evidence of absence or failure.
func Failed(format string, args ...any) CheckResult {
	return CheckResult{
		Pass:    false,
		Status:  metav1.ConditionFalse,
		Message: fmt.Sprintf(format, args...),
	}
}

// Indeterminate returns a result reporting that the evidence is incomplete or
// could not be read reliably. It is not a failure and must not be treated as healthy.
func Indeterminate(format string, args ...any) CheckResult {
	return CheckResult{
		Pass:    false,
		Status:  metav1.ConditionUnknown,
		Message: fmt.Sprintf(format, args...),
	}
}

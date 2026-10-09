package monitor

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CheckResult maps a health check to a Kubernetes condition.
//
//   - True: the check found positive evidence that the dependency is usable.
//   - False: the check found direct evidence of absence or failure.
//   - Unknown: the evidence is incomplete or cannot be read reliably.
//
// Missing information must never be reported as healthy, so an indeterminate
// check reports Unknown rather than True.
type CheckResult struct {
	Pass bool

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

// ConditionStatus derives a safe status for manually constructed results.
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
		return metav1.ConditionUnknown
	}

	return metav1.ConditionUnknown
}

// Passed returns a healthy result.
func Passed() CheckResult {
	return CheckResult{Pass: true, Status: metav1.ConditionTrue}
}

// Failed returns an unhealthy result.
func Failed(format string, args ...any) CheckResult {
	return CheckResult{
		Pass:    false,
		Status:  metav1.ConditionFalse,
		Message: fmt.Sprintf(format, args...),
	}
}

// Indeterminate returns Unknown rather than treating incomplete evidence as healthy.
func Indeterminate(format string, args ...any) CheckResult {
	return CheckResult{
		Pass:    false,
		Status:  metav1.ConditionUnknown,
		Message: fmt.Sprintf(format, args...),
	}
}

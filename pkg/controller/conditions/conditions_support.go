package conditions

import (
	"slices"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

func SetStatusCondition(a common.ConditionsAccessor, newCondition common.Condition) bool {
	conditions := a.GetConditions()

	// reset LastHeartbeatTime to ensure is not set in any condition that is
	// eventually carrying it from an old implementation
	newCondition.LastHeartbeatTime = nil

	if newCondition.LastTransitionTime.IsZero() {
		newCondition.LastTransitionTime = metav1.NewTime(time.Now())
	}

	idx := slices.IndexFunc(conditions, func(condition common.Condition) bool {
		return condition.Type == newCondition.Type
	})

	if idx == -1 {
		if newCondition.LastTransitionTime.IsZero() {
			newCondition.LastTransitionTime = metav1.NewTime(time.Now())
		}
		conditions = append(conditions, newCondition)
		a.SetConditions(conditions)
		return true
	}

	if equals(conditions[idx], newCondition) {
		return false
	}

	updateTransitionTime := conditions[idx].Status != newCondition.Status

	conditions[idx] = newCondition
	conditions[idx].LastHeartbeatTime = nil

	if updateTransitionTime {
		conditions[idx].LastTransitionTime = newCondition.LastTransitionTime

		if conditions[idx].LastTransitionTime.IsZero() {
			conditions[idx].LastTransitionTime = metav1.NewTime(time.Now())
		}
	}

	a.SetConditions(conditions)

	return true
}

func RemoveStatusCondition(a common.ConditionsAccessor, conditionType string) bool {
	conditions := a.GetConditions()
	l := len(conditions)

	if l == 0 {
		return false
	}

	conditions = slices.DeleteFunc(conditions, func(condition common.Condition) bool {
		return condition.Type == conditionType
	})

	removed := l != len(conditions)
	if removed {
		a.SetConditions(conditions)
	}

	return removed
}

func FindStatusCondition(a common.ConditionsAccessor, conditionType string) *common.Condition {
	for _, c := range a.GetConditions() {
		if c.Type == conditionType {
			return c.DeepCopy()
		}
	}

	return nil
}

func IsStatusConditionTrue(a common.ConditionsAccessor, conditionType string) bool {
	return IsStatusConditionPresentAndEqual(a, conditionType, metav1.ConditionTrue)
}

func IsStatusConditionFalse(a common.ConditionsAccessor, conditionType string) bool {
	return IsStatusConditionPresentAndEqual(a, conditionType, metav1.ConditionFalse)
}

func IsStatusConditionPresentAndEqual(a common.ConditionsAccessor, conditionType string, status metav1.ConditionStatus) bool {
	return slices.ContainsFunc(a.GetConditions(), func(condition common.Condition) bool {
		return condition.Type == conditionType && condition.Status == status
	})
}

func applyOpts(c *common.Condition, opts ...Option) {
	for _, o := range opts {
		o(c)
	}
}

func equals(c1 common.Condition, c2 common.Condition) bool {
	return c1.Status == c2.Status &&
		c1.Reason == c2.Reason &&
		c1.Message == c2.Message &&
		c1.ObservedGeneration == c2.ObservedGeneration &&
		c1.Severity == c2.Severity
}

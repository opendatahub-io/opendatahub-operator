package conditions

import (
	"slices"
	"time"

	"github.com/opendatahub-io/operator-actions-framework/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func SetStatusCondition(a api.ConditionsAccessor, newCondition api.Condition) bool {
	conditions := a.GetConditions()

	newCondition.LastHeartbeatTime = nil

	if newCondition.LastTransitionTime.IsZero() {
		newCondition.LastTransitionTime = metav1.NewTime(time.Now())
	}

	idx := slices.IndexFunc(conditions, func(condition api.Condition) bool {
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

func RemoveStatusCondition(a api.ConditionsAccessor, conditionType string) bool {
	conditions := a.GetConditions()
	l := len(conditions)

	if l == 0 {
		return false
	}

	conditions = slices.DeleteFunc(conditions, func(condition api.Condition) bool {
		return condition.Type == conditionType
	})

	removed := l != len(conditions)
	if removed {
		a.SetConditions(conditions)
	}

	return removed
}

func FindStatusCondition(a api.ConditionsAccessor, conditionType string) *api.Condition {
	for _, c := range a.GetConditions() {
		if c.Type == conditionType {
			return c.DeepCopy()
		}
	}

	return nil
}

func IsStatusConditionTrue(a api.ConditionsAccessor, conditionType string) bool {
	return IsStatusConditionPresentAndEqual(a, conditionType, metav1.ConditionTrue)
}

func IsStatusConditionFalse(a api.ConditionsAccessor, conditionType string) bool {
	return IsStatusConditionPresentAndEqual(a, conditionType, metav1.ConditionFalse)
}

func IsStatusConditionPresentAndEqual(a api.ConditionsAccessor, conditionType string, status metav1.ConditionStatus) bool {
	return slices.ContainsFunc(a.GetConditions(), func(condition api.Condition) bool {
		return condition.Type == conditionType && condition.Status == status
	})
}

func applyOpts(c *api.Condition, opts ...Option) {
	for _, o := range opts {
		o(c)
	}
}

func equals(c1 api.Condition, c2 api.Condition) bool {
	return c1.Status == c2.Status &&
		c1.Reason == c2.Reason &&
		c1.Message == c2.Message &&
		c1.ObservedGeneration == c2.ObservedGeneration &&
		c1.Severity == c2.Severity
}

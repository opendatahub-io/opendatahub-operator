package conditions

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
)

func SetStatusCondition(conditions *[]common.Condition, newCondition common.Condition) bool {
	if conditions == nil {
		return false
	}

	changed := false

	// reset LastHeartbeatTime to ensure is not set in any condition that is
	// eventually carrying it from an old implementation
	newCondition.LastHeartbeatTime = nil

	existingCondition := FindStatusCondition(*conditions, newCondition.Type)
	if existingCondition == nil {
		if newCondition.LastTransitionTime.IsZero() {
			newCondition.LastTransitionTime = metav1.NewTime(time.Now())
		}
		*conditions = append(*conditions, newCondition)
		return true
	}

	if existingCondition.Status != newCondition.Status {
		existingCondition.Status = newCondition.Status
		if !newCondition.LastTransitionTime.IsZero() {
			existingCondition.LastTransitionTime = newCondition.LastTransitionTime
		} else {
			existingCondition.LastTransitionTime = metav1.NewTime(time.Now())
		}
		changed = true
	}

	if existingCondition.Reason != newCondition.Reason {
		existingCondition.Reason = newCondition.Reason
		changed = true
	}
	if existingCondition.Message != newCondition.Message {
		existingCondition.Message = newCondition.Message
		changed = true
	}
	if existingCondition.ObservedGeneration != newCondition.ObservedGeneration {
		existingCondition.ObservedGeneration = newCondition.ObservedGeneration
		changed = true
	}
	if existingCondition.Severity != newCondition.Severity {
		existingCondition.Severity = newCondition.Severity
		changed = true
	}

	// reset LastHeartbeatTime to ensure is not set in any condition that is
	// eventually carrying it from an old implementation
	existingCondition.LastHeartbeatTime = nil

	return changed
}

func RemoveStatusCondition(conditions *[]common.Condition, conditionType string) bool {
	if conditions == nil || len(*conditions) == 0 {
		return false
	}

	newConditions := make([]common.Condition, 0, len(*conditions)-1)
	for _, condition := range *conditions {
		if condition.Type != conditionType {
			newConditions = append(newConditions, condition)
		}
	}

	removed := len(*conditions) != len(newConditions)
	*conditions = newConditions

	return removed
}

func FindStatusCondition(conditions []common.Condition, conditionType string) *common.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}

	return nil
}

func IsStatusConditionTrue(conditions []common.Condition, conditionType string) bool {
	return IsStatusConditionPresentAndEqual(conditions, conditionType, metav1.ConditionTrue)
}

func IsStatusConditionFalse(conditions []common.Condition, conditionType string) bool {
	return IsStatusConditionPresentAndEqual(conditions, conditionType, metav1.ConditionFalse)
}

func IsStatusConditionPresentAndEqual(conditions []common.Condition, conditionType string, status metav1.ConditionStatus) bool {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Status == status
		}
	}

	return false
}

func applyOpts(c *common.Condition, opts ...Option) {
	for _, o := range opts {
		o(c)
	}
}

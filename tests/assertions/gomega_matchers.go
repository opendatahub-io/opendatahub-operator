package assertions

import (
	"fmt"

	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
)

//nolint:nolintlint,ireturn // Reason: returning an interface is necessary for dynamic typing in this context
func HaveCondition(conditionType conditionsv1.ConditionType, conditionStatus corev1.ConditionStatus, reason featurev1.FeaturePhase) types.GomegaMatcher {
	return &HaveConditionMatcher{
		conditionType:   conditionType,
		conditionStatus: conditionStatus,
		reason:          reason,
	}
}

type HaveConditionMatcher struct {
	conditionType   conditionsv1.ConditionType
	conditionStatus corev1.ConditionStatus
	reason          featurev1.FeaturePhase
}

func (h HaveConditionMatcher) Match(actual interface{}) (bool, error) {
	conditions, err := asConditions(actual)
	if err != nil {
		return false, err
	}

	desiredCondition := conditionsv1.FindStatusCondition(conditions, h.conditionType)

	return desiredCondition != nil && desiredCondition.Status == h.conditionStatus && desiredCondition.Reason == string(h.reason), nil
}

func asConditions(actual interface{}) ([]conditionsv1.Condition, error) {
	var conditions []conditionsv1.Condition

	switch v := actual.(type) {
	case []conditionsv1.Condition:
		conditions = v
	case *[]conditionsv1.Condition:
		if v != nil {
			conditions = *v
		} else {
			conditions = []conditionsv1.Condition{}
		}
	default:
		return nil, fmt.Errorf("unsupported type: %T", v)
	}

	return conditions, nil
}

func (h HaveConditionMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected %s to be:\n%s", format.Object(actual, 1), h.desiredCondition())
}

func (h HaveConditionMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected %s to not be:\n%s", format.Object(actual, 1), h.desiredCondition())
}

func (h HaveConditionMatcher) desiredCondition() interface{} {
	return "Type:   " + string(h.conditionType) + "\n" +
		"Status: " + string(h.conditionStatus) + "\n" +
		"Reason: " + string(h.reason)
}

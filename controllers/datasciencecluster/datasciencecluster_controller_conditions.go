package datasciencecluster

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
)

var (
	dependantConditions = []conditionsv1.ConditionType{
		status.ConditionTypeProvisioningSucceeded,
		status.ConditionTypeComponentsReady,
	}

	conditionsPriorities = map[conditionsv1.ConditionType]int{
		status.ConditionTypeReady:                 1,
		status.ConditionTypeProvisioningSucceeded: 2,
		status.ConditionTypeComponentsReady:       3,
	}
)

type StatusConditionOption func(*conditionsv1.Condition)

func WithReason(value string) StatusConditionOption {
	return func(c *conditionsv1.Condition) {
		c.Reason = value
	}
}

func WithMessage(msg string, opts ...any) StatusConditionOption {
	value := msg
	if len(opts) != 0 {
		value = fmt.Sprintf(msg, opts...)
	}

	return func(c *conditionsv1.Condition) {
		c.Message = value
	}
}

func WithError(err error) StatusConditionOption {
	return func(c *conditionsv1.Condition) {
		c.Reason = common.ConditionReasonError
		c.Message = err.Error()
	}
}

func InitializeStatusCondition(in *dscv1.DataScienceCluster) {
	conditionsv1.SetStatusCondition(&in.Status.Conditions, conditionsv1.Condition{
		Type:    status.ConditionTypeReady,
		Status:  corev1.ConditionUnknown,
		Reason:  string(corev1.ConditionUnknown),
		Message: string(corev1.ConditionUnknown),
	})
	conditionsv1.SetStatusCondition(&in.Status.Conditions, conditionsv1.Condition{
		Type:    status.ConditionTypeProvisioningSucceeded,
		Status:  corev1.ConditionUnknown,
		Reason:  string(corev1.ConditionUnknown),
		Message: string(corev1.ConditionUnknown),
	})
	conditionsv1.SetStatusCondition(&in.Status.Conditions, conditionsv1.Condition{
		Type:    status.ConditionTypeComponentsReady,
		Status:  corev1.ConditionUnknown,
		Reason:  string(corev1.ConditionUnknown),
		Message: string(corev1.ConditionUnknown),
	})
}

func SetStatusCondition(in *dscv1.DataScienceCluster, condition conditionsv1.Condition) {
	conditionsv1.SetStatusCondition(&in.Status.Conditions, condition)
	RecomputeStatusConditionHappiness(in)
}

func MarkStatusCondition(
	in *dscv1.DataScienceCluster,
	conditionType conditionsv1.ConditionType,
	conditionStatus corev1.ConditionStatus,
	opts ...StatusConditionOption,
) {
	condition := conditionsv1.Condition{
		Type:   conditionType,
		Status: conditionStatus,
	}

	for _, opt := range opts {
		opt(&condition)
	}

	SetStatusCondition(in, condition)
}

func MarkStatusConditionTrue(
	in *dscv1.DataScienceCluster,
	conditionType conditionsv1.ConditionType,
	opts ...StatusConditionOption,
) {
	MarkStatusCondition(in, conditionType, corev1.ConditionTrue, opts...)
}

func MarkStatusConditionFalse(
	in *dscv1.DataScienceCluster,
	conditionType conditionsv1.ConditionType,
	opts ...StatusConditionOption,
) {
	MarkStatusCondition(in, conditionType, corev1.ConditionFalse, opts...)
}

func MarkStatusConditionUnknown(
	in *dscv1.DataScienceCluster,
	conditionType conditionsv1.ConditionType,
	opts ...StatusConditionOption,
) {
	MarkStatusCondition(in, conditionType, corev1.ConditionUnknown, opts...)
}

func MarkStatusConditionError(
	in *dscv1.DataScienceCluster,
	conditionType conditionsv1.ConditionType,
	err error,
) {
	MarkStatusCondition(in, conditionType, corev1.ConditionFalse, WithError(err))
}

func RecomputeStatusConditionHappiness(in *dscv1.DataScienceCluster) {
	conditions := make([]conditionsv1.Condition, 0, len(dependantConditions))

	for _, condType := range dependantConditions {
		if c := conditionsv1.FindStatusCondition(in.Status.Conditions, condType); c != nil {
			conditions = append(conditions, *c)
		}
	}

	if len(conditions) == 0 {
		return
	}

	if len(conditions) > 1 {
		sort.Slice(conditions, func(i, j int) bool {
			return conditions[i].LastTransitionTime.After(conditions[j].LastTransitionTime.Time)
		})
	}

	var unhappy *conditionsv1.Condition

	for _, c := range conditions {
		if c.Status == corev1.ConditionFalse || c.Status == corev1.ConditionUnknown {
			unhappy = &c
			break
		}
	}

	if unhappy != nil {
		conditionsv1.SetStatusCondition(&in.Status.Conditions, conditionsv1.Condition{
			Type:    status.ConditionTypeReady,
			Status:  corev1.ConditionFalse,
			Reason:  unhappy.Reason,
			Message: unhappy.Message,
		})
	} else {
		conditionsv1.SetStatusCondition(&in.Status.Conditions, conditionsv1.Condition{
			Type:    status.ConditionTypeReady,
			Status:  corev1.ConditionTrue,
			Reason:  status.ReadyReason,
			Message: status.ReadyReason,
		})
	}
}

// SortStatusConditions sorts the Status.Conditions slice of a DataScienceCluster
// according to the following rules:
//
//   - Conditions with assigned priorities (priority > 0) are placed at the
//     beginning of the slice, ordered by their priority values (lower values
//     first).
//   - The remaining conditions (priority = 0 or not in the priority map) are
//     sorted alphabetically by Type.
//
// The original slice is modified in place.
func SortStatusConditions(in *dscv1.DataScienceCluster) {
	if in == nil || len(in.Status.Conditions) <= 1 {
		return
	}

	slices.SortStableFunc(in.Status.Conditions, func(a, b conditionsv1.Condition) int {
		ret := cmp.Compare(conditionsPriorities[a.Type], conditionsPriorities[b.Type])
		if ret == 0 {
			ret = strings.Compare(string(a.Type), string(b.Type))
		}

		return ret
	})
}

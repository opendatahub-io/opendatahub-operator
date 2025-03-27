// inspired by https://github.com/knative/pkg/blob/main/apis/condition_set.go

package conditions

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

type Option func(*common.Condition)

func WithReason(value string) Option {
	return func(c *common.Condition) {
		c.Reason = value
	}
}

func WithMessage(msg string, opts ...any) Option {
	value := msg
	if len(opts) != 0 {
		value = fmt.Sprintf(msg, opts...)
	}

	return func(c *common.Condition) {
		c.Message = value
	}
}

func WithObservedGeneration(value int64) Option {
	return func(c *common.Condition) {
		c.ObservedGeneration = value
	}
}

func WithSeverity(value common.ConditionSeverity) Option {
	return func(c *common.Condition) {
		c.Severity = value
	}
}

func WithError(err error) Option {
	return func(c *common.Condition) {
		c.Severity = common.ConditionSeverityError
		c.Reason = common.ConditionReasonError
		c.Message = err.Error()
	}
}

type Manager struct {
	happy      string
	dependents []string
	accessor   common.ConditionsAccessor
}

func NewManager(accessor common.ConditionsAccessor, happy string, dependents ...string) *Manager {
	deps := make([]string, 0, len(dependents))
	for _, d := range dependents {
		if d == happy || slices.Contains(deps, d) {
			continue
		}
		deps = append(deps, d)
	}

	m := Manager{
		accessor:   accessor,
		happy:      happy,
		dependents: deps,
	}

	m.initializeConditions()

	return &m
}

// initializeConditions ensures that the conditions for the manager and its dependents are properly
// initialized. Specifically, it initializes the "happy" condition and sets the initial status for
// each dependent condition.
//
// The method performs the following:
//  1. Retrieves the "happy" condition. If it does not exist, it creates a new one with
//     `ConditionUnknown` status and sets it.
//  2. Sets the status of each dependent condition based on the "happy" condition's status. If the
//     "happy" condition's status is `True`, all dependent conditions are set to `True`, otherwise
//     to `Unknown`.
func (r *Manager) initializeConditions() {
	happy := r.GetCondition(r.happy)
	if happy == nil {
		happy = &common.Condition{
			Type:   r.happy,
			Status: metav1.ConditionUnknown,
		}
		r.SetCondition(*happy)
	}

	status := metav1.ConditionUnknown
	if happy.Status == metav1.ConditionTrue {
		status = metav1.ConditionTrue
	}

	for _, t := range r.dependents {
		if c := r.GetCondition(t); c != nil {
			continue
		}

		r.SetCondition(common.Condition{
			Type:   t,
			Status: status,
		})
	}
}

func (r *Manager) IsHappy() bool {
	if r.accessor == nil {
		return false
	}

	return IsStatusConditionTrue(r.accessor, r.happy)
}

func (r *Manager) GetTopLevelCondition() *common.Condition {
	return r.GetCondition(r.happy)
}

func (r *Manager) GetCondition(t string) *common.Condition {
	return FindStatusCondition(r.accessor, t)
}

// SetCondition sets the given condition on the manager. It updates the list of conditions and
// sorts them alphabetically. After updating, it recomputes the happiness state based on the
// provided condition type.
//
// Parameters:
//   - `cond`: The condition to set. The condition will be added or updated based on its type.
func (r *Manager) SetCondition(cond common.Condition) {
	if r.accessor == nil {
		return
	}

	if !SetStatusCondition(r.accessor, cond) {
		return
	}

	r.RecomputeHappiness(cond.Type)
}

// ClearCondition removes the specified condition type from the manager's list of conditions
// and recomputes happiness.
//
// Parameters:
//   - `t`: The type of the condition to remove.
//
// Returns:
//   - `nil` if the condition was removed successfully or was not found.
func (r *Manager) ClearCondition(t string) error {
	if r.accessor == nil {
		return nil
	}

	if !RemoveStatusCondition(r.accessor, t) {
		return nil
	}

	r.RecomputeHappiness(t)

	return nil
}

// Mark updates the status of a specified condition type and applies optional modifications.
//
// This method allows setting a condition to any of the possible statuses (`True`, `False`, or
// `Unknown`) while also allowing additional options to modify the condition before it is stored.
//
// Parameters:
//   - `t`: The type of the condition to update.
//   - `status`: The new status of the condition (one of `metav1.ConditionTrue`,
//     `metav1.ConditionFalse`, or `metav1.ConditionUnknown`).
//   - `opts`: Variadic options that can modify attributes of the condition (e.g., reason,
//     message, timestamp).
//
// Behavior:
//  1. Creates a `common.Condition` with the specified type and status.
//  2. Applies any provided options using `applyOpts(&c, opts...)`.
//  3. Sets the condition using `r.SetCondition(c)`, which updates the condition list and
//     recomputes happiness.
func (r *Manager) Mark(t string, status metav1.ConditionStatus, opts ...Option) {
	c := common.Condition{
		Type:   t,
		Status: status,
	}

	applyOpts(&c, opts...)

	r.SetCondition(c)
}

func (r *Manager) MarkTrue(t string, opts ...Option) {
	r.Mark(t, metav1.ConditionTrue, opts...)
}

func (r *Manager) MarkFalse(t string, opts ...Option) {
	r.Mark(t, metav1.ConditionFalse, opts...)
}

func (r *Manager) MarkUnknown(t string, opts ...Option) {
	r.Mark(t, metav1.ConditionUnknown, opts...)
}

func (r *Manager) MarkFrom(t string, in common.Condition) {
	c := common.Condition{
		Type:     t,
		Status:   in.Status,
		Reason:   in.Reason,
		Message:  in.Message,
		Severity: in.Severity,
	}

	r.SetCondition(c)
}

// RecomputeHappiness re-evaluates the happiness state of the manager based on the current set
// of conditions.
//
// It checks if any dependent condition is unhappy (either `False` or `Unknown`). If found, the
// "happy" condition is updated to reflect the first unhappy condition's status. If no unhappy
// dependent conditions exist, it sets the "happy" condition to `True`.
//
// Parameters:
//   - `t`: The type of the condition that may have triggered a recomputation of happiness.
func (r *Manager) RecomputeHappiness(t string) {
	if c := r.findUnhappyDependent(); c != nil {
		r.SetCondition(common.Condition{
			Type:    r.happy,
			Status:  c.Status,
			Reason:  c.Reason,
			Message: c.Message,
		})
	} else if t != r.happy {
		r.SetCondition(common.Condition{
			Type:   r.happy,
			Status: metav1.ConditionTrue,
		})
	}
}

// findUnhappyDependent identifies and returns the first dependent condition that is unhappy (i.e.,
// False or Unknown).
//
// The method operates by filtering and sorting the current conditions, checking if they meet
// the criteria for being unhappy:
// - The condition must have a Severity level of "Error".
// - The dependent condition is either `ConditionFalse` or `ConditionUnknown`.
//
// The function performs the following steps:
//  1. It determines the number of dependents and retrieves the current conditions.
//  2. It iterates through each condition, filtering out conditions that do not meet the criteria
//     (e.g., those not related to the dependents or those without "Error" severity).
//  3. It sorts the remaining conditions by the `LastTransitionTime` in descending order.
//  4. It returns the first `ConditionFalse` or `ConditionUnknown` condition found, prioritizing
//     the former.
//
// If no unhappy condition is found, the function returns nil.
//
// Returns:
//   - A pointer to the first unhappy condition if found, otherwise nil.
func (r *Manager) findUnhappyDependent() *common.Condition {
	dn := len(r.dependents)

	conditions := slices.Clone(r.accessor.GetConditions())
	n := 0

	for _, c := range conditions {
		switch {
		case dn == 0 && c.Type == r.happy:
			break
		case dn != 0 && !slices.Contains(r.dependents, c.Type):
			break
		case c.Severity != common.ConditionSeverityError:
			break
		default:
			conditions[n] = c
			n++
		}
	}

	conditions = conditions[:n]

	sort.Slice(conditions, func(i, j int) bool {
		return conditions[i].LastTransitionTime.After(conditions[j].LastTransitionTime.Time)
	})

	for _, c := range conditions {
		if c.Status == metav1.ConditionFalse {
			ret := c
			return &ret
		}
	}

	for _, c := range conditions {
		if c.Status == metav1.ConditionUnknown {
			ret := c
			return &ret
		}
	}

	return nil
}

// Sort arranges the conditions retrieved from the accessor based on the following rules:
// 1. `happy` condition is assigned the highest priority.
// 2. `dependents` are prioritized in the order they are defined.
// 3. Conditions with priority `0` (not explicitly listed) are sorted alphabetically.
//
// The sorting is stable, ensuring consistent ordering when conditions have the same
// priority.
func (r *Manager) Sort() {
	conditions := r.accessor.GetConditions()
	if len(conditions) <= 1 {
		return
	}

	priorities := make(map[string]int)
	dl := len(r.dependents)

	for i, d := range r.dependents {
		priorities[d] = dl - i
	}

	priorities[r.happy] = len(r.dependents) + 1

	slices.SortStableFunc(conditions, func(a, b common.Condition) int {
		ret := cmp.Compare(priorities[b.Type], priorities[a.Type])
		if ret == 0 {
			ret = strings.Compare(a.Type, b.Type)
		}

		return ret
	})
}

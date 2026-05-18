// inspired by https://github.com/knative/pkg/blob/main/apis/condition_set.go

package conditions

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/opendatahub-io/operator-actions-framework/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const ConditionReasonNotSet = "ConditionNotSet"

type Option func(*api.Condition)

func WithReason(value string) Option {
	return func(c *api.Condition) {
		c.Reason = value
	}
}

func WithMessage(msg string, opts ...any) Option {
	value := msg
	if len(opts) != 0 {
		value = fmt.Sprintf(msg, opts...)
	}

	return func(c *api.Condition) {
		c.Message = value
	}
}

func WithObservedGeneration(value int64) Option {
	return func(c *api.Condition) {
		c.ObservedGeneration = value
	}
}

func WithSeverity(value api.ConditionSeverity) Option {
	return func(c *api.Condition) {
		c.Severity = value
	}
}

func WithError(err error) Option {
	return func(c *api.Condition) {
		c.Severity = api.ConditionSeverityError
		c.Reason = api.ConditionReasonError
		c.Message = err.Error()
	}
}

type Manager struct {
	happy       string
	dependents  []string
	accessor    api.ConditionsAccessor
	activeTypes map[string]struct{}
}

func NewManager(accessor api.ConditionsAccessor, happy string, dependents ...string) *Manager {
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

func (r *Manager) initializeConditions() {
	happy := r.GetCondition(r.happy)
	if happy == nil {
		happy = &api.Condition{
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

		r.SetCondition(api.Condition{
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

func (r *Manager) GetTopLevelCondition() *api.Condition {
	return r.GetCondition(r.happy)
}

func (r *Manager) GetCondition(t string) *api.Condition {
	return FindStatusCondition(r.accessor, t)
}

func (r *Manager) SetCondition(cond api.Condition) {
	if r.accessor == nil {
		return
	}

	if r.activeTypes != nil {
		r.activeTypes[cond.Type] = struct{}{}
	}

	if !SetStatusCondition(r.accessor, cond) {
		return
	}

	r.RecomputeHappiness(cond.Type)
}

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

func (r *Manager) Mark(t string, status metav1.ConditionStatus, opts ...Option) {
	c := api.Condition{
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

func (r *Manager) MarkFrom(t string, in api.Condition) {
	c := api.Condition{
		Type:     t,
		Status:   in.Status,
		Reason:   in.Reason,
		Message:  in.Message,
		Severity: in.Severity,
	}

	r.SetCondition(c)
}

func (r *Manager) RecomputeHappiness(t string) {
	if c := r.findUnhappyDependent(); c != nil {
		r.SetCondition(api.Condition{
			Type:    r.happy,
			Status:  c.Status,
			Reason:  c.Reason,
			Message: c.Message,
		})
	} else if t != r.happy {
		r.SetCondition(api.Condition{
			Type:   r.happy,
			Status: metav1.ConditionTrue,
		})
	}
}

func (r *Manager) findUnhappyDependent() *api.Condition {
	dn := len(r.dependents)

	conditions := slices.Clone(r.accessor.GetConditions())
	n := 0

	for _, c := range conditions {
		switch {
		case dn == 0 && c.Type == r.happy:
			break
		case dn != 0 && !slices.Contains(r.dependents, c.Type):
			break
		case c.Severity != api.ConditionSeverityError:
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

func (r *Manager) Reset() {
	r.activeTypes = make(map[string]struct{})
}

func (r *Manager) CleanupStaleConditions() {
	if r.accessor == nil || r.activeTypes == nil {
		return
	}

	dependentSet := make(map[string]struct{}, len(r.dependents))
	for _, d := range r.dependents {
		dependentSet[d] = struct{}{}
	}

	var toRemove []string
	changed := false

	for _, c := range slices.Clone(r.accessor.GetConditions()) {
		if c.Type == r.happy {
			continue
		}

		if _, active := r.activeTypes[c.Type]; active {
			continue
		}

		if _, isDependent := dependentSet[c.Type]; isDependent {
			SetStatusCondition(r.accessor, api.Condition{
				Type:     c.Type,
				Status:   metav1.ConditionFalse,
				Severity: api.ConditionSeverityError,
				Reason:   ConditionReasonNotSet,
				Message:  fmt.Sprintf("condition %s was not set during reconciliation", c.Type),
			})
			changed = true
		} else {
			toRemove = append(toRemove, c.Type)
		}
	}

	for _, t := range toRemove {
		RemoveStatusCondition(r.accessor, t)
	}

	if len(toRemove) > 0 || changed {
		r.RecomputeHappiness("")
	}
}

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

	slices.SortStableFunc(conditions, func(a, b api.Condition) int {
		ret := cmp.Compare(priorities[b.Type], priorities[a.Type])
		if ret == 0 {
			ret = strings.Compare(a.Type, b.Type)
		}

		return ret
	})
}

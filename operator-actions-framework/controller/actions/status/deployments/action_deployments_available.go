package deployments

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/opendatahub-io/operator-actions-framework/controller/actions"
	"github.com/opendatahub-io/operator-actions-framework/controller/conditions"
	"github.com/opendatahub-io/operator-actions-framework/controller/types"
	"github.com/opendatahub-io/operator-actions-framework/resources"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultConditionType      = "DeploymentsAvailable"
	DefaultNotAvailableReason = "DeploymentsNotReady"
	DefaultPartOfLabelKey     = "platform.opendatahub.io/part-of"
)

type Action struct {
	partOfLabelKey                string
	labels                        map[string]string
	namespaceFn                   actions.Getter[string]
	conditionType                 string
	notAvailableReason            string
	disableAutomaticPartOfDefault bool
}

type ActionOpts func(*Action)

func WithSelectorLabel(k string, v string) ActionOpts {
	return func(action *Action) {
		action.labels[k] = v
	}
}

func WithSelectorLabels(values map[string]string) ActionOpts {
	return func(action *Action) {
		maps.Copy(action.labels, values)
	}
}

// WithPartOfLabel overrides the label key used to discover Deployments belonging
// to this controller for availability checks.
func WithPartOfLabel(key string) ActionOpts {
	return func(action *Action) {
		action.partOfLabelKey = key
	}
}

// WithConditionType sets the condition type used to report deployment availability.
func WithConditionType(conditionType string) ActionOpts {
	return func(action *Action) {
		action.conditionType = conditionType
	}
}

// WithNotAvailableReason sets the reason string used when deployments are not available.
func WithNotAvailableReason(reason string) ActionOpts {
	return func(action *Action) {
		action.notAvailableReason = reason
	}
}

// WithoutAutomaticPartOfDefault skips setting the selector value for
// the part-of label from the reconciled instance kind when that key is
// absent. Use with WithSelectorLabel(s) when deployments are identified by a
// stable component label rather than part-of matching lower(kind).
func WithoutAutomaticPartOfDefault() ActionOpts {
	return func(action *Action) {
		action.disableAutomaticPartOfDefault = true
	}
}

func InNamespace(ns string) ActionOpts {
	return func(action *Action) {
		action.namespaceFn = func(_ context.Context, _ *types.ReconciliationRequest) (string, error) {
			return ns, nil
		}
	}
}

func InNamespaceFn(fn actions.Getter[string]) ActionOpts {
	return func(action *Action) {
		if fn == nil {
			return
		}
		action.namespaceFn = fn
	}
}

func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	l := make(map[string]string, len(a.labels))
	maps.Copy(l, a.labels)

	if !a.disableAutomaticPartOfDefault && l[a.partOfLabelKey] == "" {
		kind, err := resources.KindForObject(rr.Client.Scheme(), rr.Instance)
		if err != nil {
			return err
		}

		l[a.partOfLabelKey] = strings.ToLower(kind)
	}

	obj, ok := rr.Instance.(types.ResourceObject)
	if !ok {
		return fmt.Errorf("resource instance %v is not a ResourceObject", rr.Instance)
	}

	deployments := &appsv1.DeploymentList{}

	if a.namespaceFn == nil {
		return errors.New("namespace function is not configured for deployment status action")
	}

	ns, err := a.namespaceFn(ctx, rr)
	if err != nil {
		return fmt.Errorf("unable to compute namespace: %w", err)
	}

	err = rr.Client.List(
		ctx,
		deployments,
		client.InNamespace(ns),
		client.MatchingLabels(l),
	)

	if err != nil {
		return fmt.Errorf("error fetching list of deployments: %w", err)
	}

	ready := 0
	for _, deployment := range deployments.Items {
		if deployment.Status.ReadyReplicas == deployment.Status.Replicas && deployment.Status.Replicas != 0 {
			ready++
		}
	}

	s := obj.GetStatus()

	rr.Conditions.MarkTrue(a.conditionType, conditions.WithObservedGeneration(s.ObservedGeneration))

	if len(deployments.Items) == 0 || (len(deployments.Items) > 0 && ready != len(deployments.Items)) {
		rr.Conditions.MarkFalse(
			a.conditionType,
			conditions.WithObservedGeneration(s.ObservedGeneration),
			conditions.WithReason(a.notAvailableReason),
			conditions.WithMessage("%d/%d deployments ready", ready, len(deployments.Items)),
		)
	}

	return nil
}

func NewAction(opts ...ActionOpts) actions.Fn {
	action := Action{
		partOfLabelKey:     DefaultPartOfLabelKey,
		labels:             map[string]string{},
		conditionType:      DefaultConditionType,
		notAvailableReason: DefaultNotAvailableReason,
	}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}

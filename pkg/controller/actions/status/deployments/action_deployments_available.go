package deployments

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

type Action struct {
	labels      map[string]string
	namespaceFn actions.StringGetter
}

type ActionOpts func(*Action)

func WithSelectorLabel(k string, v string) ActionOpts {
	return func(action *Action) {
		action.labels[k] = v
	}
}

func WithSelectorLabels(values map[string]string) ActionOpts {
	return func(action *Action) {
		for k, v := range values {
			action.labels[k] = v
		}
	}
}

func InNamespace(ns string) ActionOpts {
	return func(action *Action) {
		action.namespaceFn = func(_ context.Context, _ *types.ReconciliationRequest) (string, error) {
			return ns, nil
		}
	}
}

func InNamespaceFn(fn actions.StringGetter) ActionOpts {
	return func(action *Action) {
		if fn == nil {
			return
		}
		action.namespaceFn = fn
	}
}

func (a *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	l := make(map[string]string, len(a.labels))
	for k, v := range a.labels {
		l[k] = v
	}

	if l[labels.PlatformPartOf] == "" {
		kind, err := resources.KindForObject(rr.Client.Scheme(), rr.Instance)
		if err != nil {
			return err
		}

		l[labels.PlatformPartOf] = strings.ToLower(kind)
	}

	obj, ok := rr.Instance.(types.ResourceObject)
	if !ok {
		return fmt.Errorf("resource instance %v is not a ResourceObject", rr.Instance)
	}

	deployments := &appsv1.DeploymentList{}

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

	rr.Conditions.MarkTrue(status.ConditionDeploymentsAvailable, conditions.WithObservedGeneration(s.ObservedGeneration))

	if len(deployments.Items) == 0 || (len(deployments.Items) > 0 && ready != len(deployments.Items)) {
		rr.Conditions.MarkFalse(
			status.ConditionDeploymentsAvailable,
			conditions.WithObservedGeneration(s.ObservedGeneration),
			conditions.WithReason(status.ConditionDeploymentsNotAvailableReason),
			conditions.WithMessage("%d/%d deployments ready", ready, len(deployments.Items)),
		)
	}

	return nil
}

func NewAction(opts ...ActionOpts) actions.Fn {
	action := Action{
		labels:      map[string]string{},
		namespaceFn: actions.ApplicationNamespace,
	}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}

package updatestatus

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	DeploymentsNotReadyReason = "DeploymentsNotReady"
	ReadyReason               = "Ready"
)

type Action struct {
	labels map[string]string
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

	err := rr.Client.List(
		ctx,
		deployments,
		client.InNamespace(rr.DSCI.Spec.ApplicationsNamespace),
		client.MatchingLabels(l),
	)

	if err != nil {
		return fmt.Errorf("error fetching list of deployments: %w", err)
	}

	ready := 0
	for _, deployment := range deployments.Items {
		if deployment.Status.ReadyReplicas == deployment.Status.Replicas {
			ready++
		}
	}

	s := obj.GetStatus()
	s.ObservedGeneration = obj.GetGeneration()
	s.Phase = "Ready"

	conditionReady := metav1.Condition{
		Type:               status.ConditionTypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             ReadyReason,
		Message:            fmt.Sprintf("%d/%d deployments ready", ready, len(deployments.Items)),
		ObservedGeneration: s.ObservedGeneration,
	}

	if len(deployments.Items) == 0 || (len(deployments.Items) > 0 && ready != len(deployments.Items)) {
		conditionReady.Status = metav1.ConditionFalse
		conditionReady.Reason = DeploymentsNotReadyReason

		s.Phase = "NotReady"
	}

	meta.SetStatusCondition(&s.Conditions, conditionReady)

	return nil
}

func NewAction(opts ...ActionOpts) actions.Fn {
	action := Action{
		labels: map[string]string{},
	}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}

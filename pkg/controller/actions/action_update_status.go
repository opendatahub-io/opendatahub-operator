package actions

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	UpdateStatusActionName    = "update-status"
	DeploymentsNotReadyReason = "DeploymentsNotReady"
	ReadyReason               = "Ready"
)

type UpdateStatusAction struct {
	BaseAction
	labels map[string]string
}

type UpdateStatusActionOpts func(*UpdateStatusAction)

func WithUpdateStatusLabel(k string, v string) UpdateStatusActionOpts {
	return func(action *UpdateStatusAction) {
		action.labels[k] = v
	}
}

func WithUpdateStatusLabels(values map[string]string) UpdateStatusActionOpts {
	return func(action *UpdateStatusAction) {
		for k, v := range values {
			action.labels[k] = v
		}
	}
}

func (a *UpdateStatusAction) Execute(ctx context.Context, rr *types.ReconciliationRequest) error {
	if len(a.labels) == 0 {
		return nil
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
		client.MatchingLabels(a.labels),
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
	s.Phase = "Ready"

	conditionReady := metav1.Condition{
		Type:    status.ConditionTypeReady,
		Status:  metav1.ConditionTrue,
		Reason:  ReadyReason,
		Message: fmt.Sprintf("%d/%d deployments ready", ready, len(deployments.Items)),
	}

	if len(deployments.Items) > 0 && ready != len(deployments.Items) {
		conditionReady.Status = metav1.ConditionFalse
		conditionReady.Reason = DeploymentsNotReadyReason

		s.Phase = "NotReady"
	}

	meta.SetStatusCondition(&s.Conditions, conditionReady)

	return nil
}

func NewUpdateStatusAction(ctx context.Context, opts ...UpdateStatusActionOpts) *UpdateStatusAction {
	action := UpdateStatusAction{
		BaseAction: BaseAction{
			Log: log.FromContext(ctx).WithName(ActionGroup).WithName(UpdateStatusActionName),
		},
		labels: map[string]string{},
	}

	for _, opt := range opts {
		opt(&action)
	}

	return &action
}

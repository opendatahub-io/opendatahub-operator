package components

import (
	"context"
	"fmt"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ActionGroup = "action"
)

//
// Delete Resources Action
//

const (
	DeleteResourcesActionName = "delete-resources"
)

type DeleteResourcesAction struct {
	BaseAction
	types  []client.Object
	labels map[string]string
}

type DeleteResourcesActionOpts func(*DeleteResourcesAction)

func WithDeleteResourcesTypes(values ...client.Object) DeleteResourcesActionOpts {
	return func(action *DeleteResourcesAction) {
		action.types = append(action.types, values...)
	}
}

func WithDeleteResourcesLabel(k string, v string) DeleteResourcesActionOpts {
	return func(action *DeleteResourcesAction) {
		action.labels[k] = v
	}
}

func WithDeleteResourcesLabels(values map[string]string) DeleteResourcesActionOpts {
	return func(action *DeleteResourcesAction) {
		for k, v := range values {
			action.labels[k] = v
		}
	}
}

func (r *DeleteResourcesAction) Execute(ctx context.Context, rr *ReconciliationRequest) error {
	for i := range r.types {
		opts := make([]client.DeleteAllOfOption, 0)

		if len(r.labels) > 0 {
			opts = append(opts, client.MatchingLabels(r.labels))
		}

		namespaced, err := rr.Client.IsObjectNamespaced(r.types[i])
		if err != nil {
			return err
		}

		if namespaced {
			opts = append(opts, client.InNamespace(rr.DSCI.Spec.ApplicationsNamespace))
		}

		err = rr.Client.DeleteAllOf(ctx, r.types[i], opts...)
		if err != nil {
			return err
		}
	}

	return nil
}

func NewDeleteResourcesAction(ctx context.Context, opts ...DeleteResourcesActionOpts) *DeleteResourcesAction {
	action := DeleteResourcesAction{
		BaseAction: BaseAction{
			Log: log.FromContext(ctx).WithName(ActionGroup).WithName(DeleteResourcesActionName),
		},
		types:  make([]client.Object, 0),
		labels: map[string]string{},
	}

	for _, opt := range opts {
		opt(&action)
	}

	return &action
}

//
// Update Status Action
//

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

func (a *UpdateStatusAction) Execute(ctx context.Context, rr *ReconciliationRequest) error {
	if len(a.labels) == 0 {
		return nil
	}

	obj, ok := rr.Instance.(ResourceObject)
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

	conditionReady := metav1.Condition{
		Type:    status.ConditionTypeReady,
		Status:  metav1.ConditionTrue,
		Reason:  ReadyReason,
		Message: fmt.Sprintf("%d/%d deployments ready", ready, len(deployments.Items)),
	}

	if len(deployments.Items) > 0 && ready != len(deployments.Items) {
		conditionReady.Status = metav1.ConditionFalse
		conditionReady.Reason = DeploymentsNotReadyReason
	}

	status.SetStatusCondition(obj, conditionReady)

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

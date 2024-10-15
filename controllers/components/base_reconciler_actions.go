package components

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DeleteResourcesAction struct {
	BaseAction
	Types  []client.Object
	Labels map[string]string
}

func (r *DeleteResourcesAction) Execute(ctx context.Context, rr *ReconciliationRequest) error {
	for i := range r.Types {
		opts := make([]client.DeleteAllOfOption, 0, 1)
		opts = append(opts, client.MatchingLabels(r.Labels))

		namespaced, err := rr.Client.IsObjectNamespaced(r.Types[i])
		if err != nil {
			return err
		}

		if namespaced {
			opts = append(opts, client.InNamespace(rr.DSCI.Spec.ApplicationsNamespace))
		}

		err = rr.Client.DeleteAllOf(ctx, r.Types[i], opts...)
		if err != nil {
			return err
		}
	}

	return nil
}

type UpdateStatusAction struct {
	BaseAction
	Labels map[string]string
}

func (a *UpdateStatusAction) Execute(ctx context.Context, rr *ReconciliationRequest) error {
	obj, ok := rr.Instance.(ResourceObject)
	if !ok {
		return fmt.Errorf("resource instance %v is not a ResourceObject", rr.Instance)
	}

	deployments := &appsv1.DeploymentList{}

	err := rr.Client.List(
		ctx,
		deployments,
		client.InNamespace(rr.DSCI.Spec.ApplicationsNamespace),
		client.MatchingLabels(a.Labels),
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

	status := metav1.ConditionTrue
	reason := "Ready"

	if len(deployments.Items) > 0 && ready != len(deployments.Items) {
		status = metav1.ConditionFalse
		reason = "Ready"
	}

	s := obj.GetStatus()

	meta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    "Ready",
		Status:  status,
		Reason:  reason,
		Message: fmt.Sprintf("%d/%d deployments ready", ready, len(deployments.Items)),
	})

	return nil
}

package feastoperator

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/releases"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/component"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &componentApi.FeastOperator{}).
		// customized Owns() for Component with new predicates
		Owns(&corev1.ConfigMap{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.Deployment{}, reconciler.WithPredicates(resources.NewDeploymentPredicate())).
		Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.FeastOperatorInstanceName)),
			reconciler.WithPredicates(
				component.ForLabel(labels.ODH.Component(ComponentName), labels.True)),
		).
		// Add FeastOperator-specific actions
		WithAction(initialize).
		WithAction(devFlags).
		WithAction(releases.NewAction()).
		WithAction(kustomize.NewAction(
			kustomize.WithCache(),
			kustomize.WithLabel(labels.ODH.Component(ComponentName), labels.True),
			kustomize.WithLabel(labels.K8SCommon.PartOf, ComponentName),
		)).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		WithAction(deployments.NewAction()).
		// must be the final action
		WithAction(gc.NewAction()).
		// declares the list of additional, controller specific conditions that are
		// contributing to the controller readiness status
		WithConditions(conditionTypes...).
		Build(ctx)

	if err != nil {
		return err // no need customize error, it is done in the caller main
	}

	return nil
}

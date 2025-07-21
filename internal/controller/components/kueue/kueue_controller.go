/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kueue

import (
	"context"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/observability"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/releases"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/component"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	b := reconciler.ReconcilerFor(mgr, &componentApi.Kueue{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&promv1.PodMonitor{}).
		Owns(&promv1.PrometheusRule{}).
		Owns(&admissionregistrationv1.MutatingWebhookConfiguration{}).
		Owns(&admissionregistrationv1.ValidatingWebhookConfiguration{}).
		Owns(&appsv1.Deployment{}, reconciler.WithPredicates(resources.NewDeploymentPredicate())).
		Watches(
			&corev1.ConfigMap{},
			reconciler.WithPredicates(
				predicates.DefaultPredicate,
				component.ForLabel(labels.PlatformPartOf, componentApi.KueueComponentName),
				resources.CreatedOrUpdatedOrDeletedNamed(KueueConfigMapName),
			),
		).
		WatchesGVK(gvk.LocalQueue,
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.KueueInstanceName),
			),
			reconciler.Dynamic(reconciler.CrdExists(gvk.LocalQueue))).
		WatchesGVK(gvk.ClusterQueue,
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.KueueInstanceName),
			),
			reconciler.Dynamic(reconciler.CrdExists(gvk.ClusterQueue))).
		WatchesGVK(gvk.KueueConfigV1,
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.KueueInstanceName),
			),
			reconciler.Dynamic(reconciler.CrdExists(gvk.KueueConfigV1))).
		WatchesGVK(gvk.OperatorCondition,
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.KueueInstanceName),
			),
			reconciler.WithPredicates(resources.CreatedOrUpdatedOrDeletedNamePrefixed(kueueOperator))).
		Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.KueueInstanceName)),
			reconciler.WithPredicates(predicate.Or(
				component.ForLabel(labels.ODH.Component(LegacyComponentName), labels.True),
				resources.CreatedOrUpdatedOrDeletedNamed(kueueCRDname),
			)),
		).
		Watches(&rbacv1.ClusterRole{},
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.KueueInstanceName),
			),
			reconciler.WithPredicates(resources.CreatedOrUpdatedName(ClusterQueueViewerRoleName), predicate.LabelChangedPredicate{}),
		).
		Watches(&corev1.Namespace{},
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.KueueInstanceName),
			),
			reconciler.WithPredicates(
				predicate.And(
					predicate.LabelChangedPredicate{},
					predicate.Or(component.ForLabel(cluster.KueueManagedLabelKey, "true")),
				),
			),
		).
		Watches(&serviceApi.Auth{},
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.KueueInstanceName),
			),
		).
		WithAction(checkPreConditions).
		WithAction(initialize).
		WithAction(devFlags).
		WithAction(releases.NewAction()).
		WithAction(kustomize.NewAction(
			kustomize.WithLabel(labels.ODH.Component(LegacyComponentName), labels.True),
			kustomize.WithLabel(labels.K8SCommon.PartOf, LegacyComponentName),
		)).
		WithAction(observability.NewAction()).
		WithAction(manageDefaultKueueResourcesAction).
		WithAction(manageKueueAdminRoleBinding).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		WithAction(deployments.NewAction()).
		WithAction(func(ctx context.Context, rr *types.ReconciliationRequest) error {
			kueueCRInstance, ok := rr.Instance.(*componentApi.Kueue)
			if !ok {
				return fmt.Errorf("resource instance %v is not a componentApi.Kueue)", rr.Instance)
			}
			if kueueCRInstance.Spec.KueueManagementSpec.ManagementState == operatorv1.Unmanaged {
				rr.Conditions.MarkFalse(status.ConditionDeploymentsAvailable, conditions.WithSeverity(common.ConditionSeverityInfo))
			}
			return nil
		}).
		WithAction(configureClusterQueueViewerRoleAction).
		// must be the final action
		WithAction(gc.NewAction()).
		// declares the list of additional, controller specific conditions that are
		// contributing to the controller readiness status
		WithConditions(conditionTypes...)

	if _, err := b.Build(ctx); err != nil {
		return err // no need customize error, it is done in the caller main
	}

	return nil
}

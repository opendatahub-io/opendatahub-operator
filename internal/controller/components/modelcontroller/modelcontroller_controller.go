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

package modelcontroller

import (
	"context"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	templatev1 "github.com/openshift/api/template/v1"
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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/releases"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/precondition"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/component"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(
		mgr,
		&componentApi.ModelController{},
	).
		// customized Owns() for Component with new predicates
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Secret{}).
		Owns(&promv1.ServiceMonitor{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&corev1.Service{}).
		Owns(&admissionregistrationv1.ValidatingWebhookConfiguration{}).
		Owns(&templatev1.Template{}).
		Owns(&appsv1.Deployment{}, reconciler.WithPredicates(predicates.DefaultDeploymentPredicate)).
		Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.ModelControllerInstanceName)),
			reconciler.WithPredicates(
				predicate.Or(
					component.ForLabel(labels.ODH.Component(LegacyComponentName), labels.True),
					resources.CreatedOrUpdatedOrDeletedNamed(gvk.VariantAutoscalingCRDname),
				),
			),
		).
		WatchesGVK(gvk.Subscription,
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.ModelControllerInstanceName),
			),
			reconciler.WithPredicates(
				resources.CreatedOrUpdatedOrDeletedNamed(CMAOperatorSubscription),
			),
			reconciler.Dynamic(reconciler.CrdExists(gvk.Subscription))).
		// This get deleted configmap e.g workload-variant-autoscaler-saturation-scaling-config re-created
		Watches(
			&corev1.ConfigMap{},
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.ModelControllerInstanceName),
			),
			reconciler.WithPredicates(
				predicate.And(
					resources.Deleted(),
					component.ForLabel("app.kubernetes.io/name", "workload-variant-autoscaler"),
				),
			),
		).
		// preconditions
		WithPreCondition(precondition.MonitorSubscriptions(
			[]precondition.SubscriptionDependency{
				{Name: CMAOperatorSubscription, DisplayName: "Custom Metrics Autoscaler"},
			},
			precondition.WithConditionType(LLMDWVADependencies),
			precondition.WithClusterTypes(cluster.ClusterTypeOpenShift),
			precondition.WithSeverity(common.ConditionSeverityInfo),
			// Skip CMA subscription check unless both Kserve and WVA are managed
			precondition.WithSkipFunc(func(_ context.Context, rr *odhtypes.ReconciliationRequest) (bool, error) {
				mc, ok := rr.Instance.(*componentApi.ModelController)
				if !ok {
					return false, fmt.Errorf("expected *ModelController, got %T", rr.Instance)
				}
				if mc.Spec.Kserve == nil {
					return true, nil
				}
				return mc.Spec.Kserve.ManagementState != operatorv1.Managed ||
					mc.Spec.Kserve.WVA.ManagementState != operatorv1.Managed, nil
			}),
		)).
		// actions
		WithAction(precondition.RunlevelGateAction()).
		WithAction(initialize).
		WithAction(kustomize.NewAction(
			kustomize.WithLabel(labels.ODH.Component(LegacyComponentName), labels.True),
			kustomize.WithLabel(labels.K8SCommon.PartOf, LegacyComponentName),
		)).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		WithAction(deployments.NewAction()).
		WithAction(releases.NewPlatformVersionAction()).
		// must be the final action
		WithAction(gc.NewAction()).
		// declares the list of additional, controller specific conditions that are
		// contributing to the controller readiness status
		WithConditions(conditionTypes...).
		Build(ctx) // include GenerationChangedPredicate no need set in each Owns() above

	if err != nil {
		return err // no need customize error, it is done in the caller main
	}

	return nil
}

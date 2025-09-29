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

package kserve

import (
	"context"

	templatev1 "github.com/openshift/api/template/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/releases"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/component"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/generation"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/hash"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// NewComponentReconciler creates a ComponentReconciler for the Dashboard API.
func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &componentApi.Kserve{}).
		// operands - owned
		Owns(&corev1.Secret{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		// The ovms template gets a new resourceVersion periodically without any other
		// changes. The compareHashPredicate ensures that we don't needlessly enqueue
		// requests if there are no changes that we don't care about.
		Owns(&templatev1.Template{}, reconciler.WithPredicates(hash.Updated())).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&monitoringv1.ServiceMonitor{}).
		Owns(&admissionregistrationv1.MutatingWebhookConfiguration{}).
		Owns(&admissionregistrationv1.ValidatingWebhookConfiguration{}).
		Owns(&appsv1.Deployment{}, reconciler.WithPredicates(resources.NewDeploymentPredicate())).

		// operands - dynamically owned
		OwnsGVK(gvk.IstioGateway, reconciler.Dynamic(reconciler.CrdExists(gvk.IstioGateway))).
		OwnsGVK(gvk.EnvoyFilter, reconciler.Dynamic(reconciler.CrdExists(gvk.EnvoyFilter))).
		OwnsGVK(gvk.KnativeServing, reconciler.Dynamic(reconciler.CrdExists(gvk.KnativeServing))).
		OwnsGVK(gvk.ServiceMeshMember, reconciler.Dynamic(reconciler.CrdExists(gvk.ServiceMeshMember))).
		OwnsGVK(gvk.AuthorizationPolicy, reconciler.Dynamic(reconciler.CrdExists(gvk.AuthorizationPolicy))).
		OwnsGVK(gvk.AuthorizationPolicyv1beta1, reconciler.Dynamic(reconciler.CrdExists(gvk.AuthorizationPolicyv1beta1))).
		OwnsGVK(gvk.InferencePoolV1alpha2, reconciler.Dynamic(reconciler.CrdExists(gvk.InferencePoolV1alpha2))).
		OwnsGVK(gvk.InferenceModelV1alpha2, reconciler.Dynamic(reconciler.CrdExists(gvk.InferenceModelV1alpha2))).
		OwnsGVK(gvk.LLMInferenceServiceConfigV1Alpha1, reconciler.Dynamic(reconciler.CrdExists(gvk.LLMInferenceServiceConfigV1Alpha1))).
		OwnsGVK(gvk.LLMInferenceServiceV1Alpha1, reconciler.Dynamic(reconciler.CrdExists(gvk.LLMInferenceServiceV1Alpha1))).

		// operands - watched
		//
		// By default the Watches functions adds:
		// - an event handler mapping to a cluster scope resource identified by the
		//   components.platform.opendatahub.io/managed-by annotation
		// - a predicate that check for generation change for Delete/Updates events
		//   for to objects that have the label components.platform.opendatahub.io/managed-by
		//   set to the current owner
		Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.KserveInstanceName)),
			reconciler.WithPredicates(
				component.ForLabel(labels.ODH.Component(LegacyComponentName), labels.True),
			),
		).
		// resource
		Watches(
			&dsciv2.DSCInitialization{},
			reconciler.WithEventHandler(handlers.ToNamed(componentApi.KserveInstanceName)),
			reconciler.WithPredicates(predicate.Or(generation.New(), resources.DSCIReadiness, resources.DSCIServiceMeshCondition)),
		).
		WatchesGVK(
			gvk.OperatorCondition,
			reconciler.WithEventHandler(handlers.ToNamed(componentApi.KserveInstanceName)),
			reconciler.WithPredicates(isRequiredOperators),
		).
		WatchesGVK(gvk.LeaderWorkerSetOperator,
			reconciler.Dynamic(reconciler.CrdExists(gvk.LeaderWorkerSetOperator))).
		// actions
		WithAction(checkPreConditions).
		WithAction(initialize).
		WithAction(releases.NewAction()).
		WithAction(addTemplateFiles).
		WithAction(template.NewAction(
			template.WithDataFn(getTemplateData),
		)).

		// these actions deal with the Kserve serving config and DSCI
		// service mesh config, and the transitions between different
		// management states of those.
		WithAction(addServingCertResourceIfManaged).
		WithAction(removeOwnershipFromUnmanagedResources).
		WithAction(cleanUpTemplatedResources).
		WithAction(kustomize.NewAction(
			// These are the default labels added by the legacy deploy method
			// and should be preserved as the original plugin were affecting
			// deployment selectors that are immutable once created, so it won't
			// be possible to actually amend the labels in a non-disruptive
			// manner.
			//
			// Additional labels/annotations MUST be added by the deploy action
			// so they would affect only objects metadata without side effects
			kustomize.WithLabel(labels.ODH.Component(LegacyComponentName), labels.True),
			kustomize.WithLabel(labels.K8SCommon.PartOf, LegacyComponentName),
		)).
		WithAction(customizeKserveConfigMap).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		WithAction(deployments.NewAction()).
		WithAction(setStatusFields).
		// TODO: can be removed after RHOAI 2.26 (next EUS)
		WithAction(deleteFeatureTrackers).
		// must be the final action
		WithAction(gc.NewAction()).
		// declares the list of additional, controller specific conditions that are
		// contributing to the controller readiness status
		WithConditions(conditionTypes...).
		Build(ctx)

	return err
}

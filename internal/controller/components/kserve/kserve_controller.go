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
	"slices"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/releases"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/component"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/dependent"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/hash"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	pkgresources "github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// NewComponentReconciler creates a ComponentReconciler for the Kserve API.
func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	versionPrefix := strings.ReplaceAll("v"+cluster.GetRelease().Version.String(), ".", "-")

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
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&admissionregistrationv1.MutatingWebhookConfiguration{}).
		Owns(&admissionregistrationv1.ValidatingWebhookConfiguration{}).
		Owns(&admissionregistrationv1.ValidatingAdmissionPolicy{}).
		Owns(&admissionregistrationv1.ValidatingAdmissionPolicyBinding{}).
		Owns(&appsv1.Deployment{}, reconciler.WithPredicates(predicates.DefaultDeploymentPredicate)).

		// The ovms template gets a new resourceVersion periodically without any other
		// changes. The compareHashPredicate ensures that we don't needlessly enqueue
		// requests if there are no changes that we don't care about.
		OwnsGVK(gvk.OpenshiftTemplate, reconciler.WithPredicates(hash.Updated()), reconciler.Dynamic(reconciler.ClusterIsOpenShift())).
		OwnsGVK(gvk.CoreosServiceMonitor, reconciler.Dynamic(reconciler.CrdExists(gvk.CoreosServiceMonitor))).

		// operands - dynamically owned
		OwnsGVK(gvk.InferencePoolV1alpha2, reconciler.Dynamic(reconciler.CrdExists(gvk.InferencePoolV1alpha2))).
		OwnsGVK(gvk.InferencePoolV1, reconciler.Dynamic(reconciler.CrdExists(gvk.InferencePoolV1))).
		OwnsGVK(gvk.InferenceModelV1alpha2, reconciler.Dynamic(reconciler.CrdExists(gvk.InferenceModelV1alpha2))).
		OwnsGVK(gvk.LLMInferenceServiceConfigV1Alpha1, reconciler.Dynamic(reconciler.CrdExists(gvk.LLMInferenceServiceConfigV1Alpha1))).
		OwnsGVK(gvk.LLMInferenceServiceConfigV1Alpha2, reconciler.Dynamic(reconciler.CrdExists(gvk.LLMInferenceServiceConfigV1Alpha2))).
		OwnsGVK(gvk.LLMInferenceServiceV1Alpha1, reconciler.Dynamic(reconciler.CrdExists(gvk.LLMInferenceServiceV1Alpha1))).
		OwnsGVK(gvk.LLMInferenceServiceV1Alpha2, reconciler.Dynamic(reconciler.CrdExists(gvk.LLMInferenceServiceV1Alpha2))).

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
				predicate.Or(
					component.ForLabel(labels.ODH.Component(LegacyComponentName), labels.True),
					resources.CreatedOrUpdatedOrDeletedNameSuffixed(".networking.istio.io"),
					resources.CreatedOrUpdatedOrDeletedNameSuffixed(".security.istio.io"),
					resources.CreatedOrUpdatedOrDeletedNameSuffixed(".telemetry.istio.io"),
					resources.CreatedOrUpdatedOrDeletedNameSuffixed(".extensions.istio.io"),
					resources.CreatedOrUpdatedOrDeletedNameSuffixed(".cert-manager.io"),
					resources.CreatedOrUpdatedOrDeletedNameSuffixed(".leaderworkerset.x-k8s.io"),
					resources.CreatedOrUpdatedOrDeletedNamed(gvk.LeaderWorkerSetOperatorCRDname),
					resources.CreatedOrUpdatedOrDeletedNamed(gvk.SubscriptionCRDname),
				),
			),
		).
		WatchesGVK(gvk.Subscription,
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.KserveInstanceName),
			),
			reconciler.WithPredicates(
				predicate.Or(
					resources.CreatedOrUpdatedOrDeletedNamed(rhclOperatorSubscription),
					resources.CreatedOrUpdatedOrDeletedNamed(lwsOperatorSubscription),
					resources.CreatedOrUpdatedOrDeletedNamed(certManagerOperatorSubscription),
				),
			),
			reconciler.Dynamic(reconciler.CrdExists(gvk.Subscription))).
		WatchesGVK(gvk.LeaderWorkerSetOperatorV1,
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.KserveInstanceName),
			),
			reconciler.WithPredicates(
				dependent.New(dependent.WithWatchStatus(true)),
			),
			reconciler.Dynamic(reconciler.CrdExists(gvk.LeaderWorkerSetOperatorV1))).
		// Watch for dependency CRDs (istio, cert-manager, leaderworkerset)
		// so the controller re-reconciles when they appear or disappear.

		// actions
		WithAction(initialize).
		WithAction(checkOperatorAndCRDDependencies()).
		WithAction(checkSubscriptionDependencies()).
		WithAction(releases.NewAction()).
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
		WithAction(func(ctx context.Context, rr *types.ReconciliationRequest) error {
			return versionedWellKnownLLMInferenceServiceConfigs(ctx, versionPrefix, rr)
		}).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
			WithApplyOrderLLMInferenceServiceConfigLast(),
			deploy.WithApplyOrder(),
		)).
		WithAction(deployments.NewAction()).
		// must be the final action
		WithAction(gc.NewAction(gc.WithUnremovables(gvk.LLMInferenceServiceConfigV1Alpha1, gvk.LLMInferenceServiceConfigV1Alpha2))).
		// declares the list of additional, controller specific conditions that are
		// contributing to the controller readiness status
		WithConditions(conditionTypes...).
		Build(ctx)

	return err
}

// WithApplyOrderLLMInferenceServiceConfigLast returns a deploy option that sorts
// resources using the standard apply order (CRDs first, webhooks last), then
// moves all LLMInferenceServiceConfig resources to the very end.
//
// This ordering is critical for upgrades (e.g. 3.3 → 3.4). In 3.3, a single
// kserve controller handled LLMInferenceServiceConfig validation. In 3.4,
// validation moves to the separate llmisvc controller with its own webhook.
// During upgrades the kserve controller is updated to 3.4 first, but the old
// ValidatingWebhookConfiguration still points to kserve-webhook-server-service
// which no longer serves the LLMInferenceServiceConfig validation endpoint.
// Since WithApplyOrder places webhooks last, if LLMInferenceServiceConfig
// resources are applied before the new ValidatingWebhookConfiguration replaces
// the old one, validation fails and the operator stops — preventing the new
// webhook configuration from ever being applied.
// Placing LLMInferenceServiceConfig resources after webhooks ensures the new
// ValidatingWebhookConfiguration is applied first.
func WithApplyOrderLLMInferenceServiceConfigLast() deploy.ActionOpts {
	return deploy.WithSortFn(deploy.SortFn(pkgresources.SortByApplyOrder).Then(sortLLMInferenceServiceConfigLast))
}

func sortLLMInferenceServiceConfigLast(_ context.Context, objects []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	result := objects
	// Stable-sort LLMInferenceServiceConfig resources after everything else
	// so they are applied only once the webhook(s) are updated.
	slices.SortStableFunc(result, func(a, b unstructured.Unstructured) int {
		if isLLMInferenceServiceConfig(a) && isLLMInferenceServiceConfig(b) {
			// Keep the original order.
			return 0
		}
		if isLLMInferenceServiceConfig(a) {
			return 1
		}
		if isLLMInferenceServiceConfig(b) {
			return -1
		}
		return 0
	})
	return result, nil
}

func isLLMInferenceServiceConfig(r unstructured.Unstructured) bool {
	return r.GroupVersionKind().Group == gvk.LLMInferenceServiceConfigV1Alpha2.Group &&
		r.GetKind() == gvk.LLMInferenceServiceConfigV1Alpha2.Kind
}

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

	"github.com/blang/semver/v4"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
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
	b := reconciler.ReconcilerFor(mgr, &componentApi.Kueue{})

	if cluster.GetClusterInfo().Version.GTE(semver.MustParse("4.17.0")) {
		// "own" VAP, because we want it has owner so when kueue is removed it gets cleaned.
		b = b.OwnsGVK(gvk.ValidatingAdmissionPolicy)

		// "watch" VAPB, because we want it to be configurable by user, and it can be left behind
		// when kueue is removed
		b = b.WatchesGVK(gvk.ValidatingAdmissionPolicyBinding)

		// add OCP 4.17.0 specific menifests
		b = b.WithAction(extraInitialize)
	}

	// customized Owns() for Component with new predicates
	b.Owns(&corev1.ConfigMap{}).
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
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.KueueInstanceName)),
			reconciler.WithPredicates(
				component.ForLabel(labels.ODH.Component(LegacyComponentName), labels.True)),
		).
		WithAction(checkPreConditions).
		WithAction(initialize).
		WithAction(devFlags).
		WithAction(releases.NewAction()).
		WithAction(kustomize.NewAction(
			kustomize.WithCache(),
			kustomize.WithLabel(labels.ODH.Component(LegacyComponentName), labels.True),
			kustomize.WithLabel(labels.K8SCommon.PartOf, LegacyComponentName),
		)).
		WithAction(customizeResources).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		WithAction(deployments.NewAction()).
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

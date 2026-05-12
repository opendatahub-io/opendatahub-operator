/*
Copyright 2025.

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

// ModelsAsService component reconciler: .Owns() must cover every namespaced/cluster
// GVK emitted by the maas-controller install kustomize bundle (see upstream
// https://github.com/opendatahub-io/models-as-a-service/tree/main/deployment/base/maas-controller/default
// which pulls in ../crd, ../rbac, ../manager, ../monitoring). CRDs use a separate deploy path
// and are not listed here. Gateway policy YAMLs under ../policies are not part of default.
// Cluster-scoped maas Config (config-default.yaml) must be listed for deploy owner refs + watches.
package modelsasservice

import (
	"context"

	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/component"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &componentApi.ModelsAsService{}).
		// maas-parameters ConfigMap is appended by buildMaasOperatorInstallManifests.
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&networkingv1.NetworkPolicy{}).
		// Upstream monitoring uses PodMonitor, not ServiceMonitor
		// (deployment/base/maas-controller/monitoring/podmonitor.yaml).
		Owns(&promv1.PodMonitor{}).
		// Reserved for future webhooks; not in default bundle today.
		Owns(&admissionregistrationv1.ValidatingWebhookConfiguration{}).
		Owns(&appsv1.Deployment{}, reconciler.WithPredicates(predicates.DefaultDeploymentPredicate)).
		OwnsGVK(gvk.MaasConfig).
		Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.ModelsAsServiceInstanceName),
			),
			reconciler.WithPredicates(
				component.ForLabel(labels.ODH.Component(componentApi.ModelsAsServiceComponentName), labels.True),
			),
		).
		WithAction(renderMaasOperatorInstall).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		WithAction(deployments.NewAction(
			deployments.WithoutAutomaticPartOfDefault(),
			deployments.WithSelectorLabel(labels.ODH.Component(componentApi.ModelsAsServiceComponentName), labels.True),
		)).
		WithAction(gc.NewAction()).
		WithConditions(conditionTypes...).
		Build(ctx)

	if err != nil {
		return err
	}

	return nil
}

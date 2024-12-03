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

package modelmeshserving

import (
	"context"

	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/security"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/updatestatus"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/clusterrole"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

var serviceAccounts = map[cluster.Platform][]string{
	cluster.SelfManagedRhods: {"modelmesh", "modelmesh-controller"},
	cluster.ManagedRhods:     {"modelmesh", "modelmesh-controller"},
	cluster.OpenDataHub:      {"modelmesh", "modelmesh-controller"},
	cluster.Unknown:          {"modelmesh", "modelmesh-controller"},
}

func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(
		mgr,
		&componentsv1.ModelMeshServing{},
	).
		// customized Owns() for Component with new predicates
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&promv1.ServiceMonitor{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&admissionregistrationv1.ValidatingWebhookConfiguration{}).
		Owns(&corev1.Service{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.ClusterRole{}, reconciler.WithPredicates(clusterrole.IgnoreIfAggregationRule())).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&appsv1.Deployment{}, reconciler.WithPredicates(resources.NewDeploymentPredicate())).
		// TODO: uncomment below watch on CRD
		// Watches(&extv1.CustomResourceDefinition{}).
		// Add ModelMeshServing specific actions
		WithAction(initialize).
		WithAction(devFlags).
		WithAction(security.NewUpdatePodSecurityRoleBindingAction(serviceAccounts)).
		WithAction(kustomize.NewAction(
			kustomize.WithCache(),
			kustomize.WithLabel(labels.ODH.Component(ComponentName), "true"),
			kustomize.WithLabel(labels.K8SCommon.PartOf, ComponentName),
		)).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		WithAction(updatestatus.NewAction()).
		WithAction(gc.NewAction()).
		Build(ctx) // include GenerationChangedPredicate no need set in each Owns() above

	if err != nil {
		return err // no need customize error, it is done in the caller main
	}

	return nil
}

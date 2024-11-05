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

package dashboard

import (
	"context"
	"fmt"

	consolev1 "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/updatestatus"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// NewComponentReconciler creates a ComponentReconciler for the Dashboard API.
func NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	componentName := computeComponentName()

	_, err := reconciler.ComponentReconcilerFor(mgr, componentsv1.DashboardInstanceName, &componentsv1.Dashboard{}).
		// operands - owned
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&corev1.ServiceAccount{}).
		// By default, a predicated for changed generation is added by the Owns()
		// method, however for deployments, we also need to retrieve status info
		// hence we need a dedicated predicate to react to replicas status change
		Owns(&appsv1.Deployment{}, builder.WithPredicates(resources.NewDeploymentPredicate())).
		// operands - openshift
		Owns(&routev1.Route{}).
		Owns(&consolev1.ConsoleLink{}).
		// operands - watched
		//
		// By default the Watches functions adds:
		// - an event handler mapping to a cluster scope resource identified by the
		//   components.opendatahub.io/managed-by annotation
		// - a predicate that check for generation change for Delete/Updates events
		//   for to objects that have the label components.opendatahub.io/managed-by
		//   set to the current owner
		//
		Watches(&extv1.CustomResourceDefinition{}).
		// actions
		WithAction(initialize).
		WithAction(devFlags).
		WithAction(render.NewAction(
			render.WithCache(true, render.DefaultCachingKeyFn),
			// Those are the default labels added by the legacy deploy method
			// and should be preserved as the original plugin were affecting
			// deployment selectors that are immutable once created, so it won't
			// be possible to actually amend the labels in a non-disruptive
			// manner.
			//
			// Additional labels/annotations MUST be added by the deploy action
			// so they would affect only objects metadata without side effects
			render.WithLabel(labels.ODH.Component(componentName), "true"),
			render.WithLabel(labels.K8SCommon.PartOf, componentName),
		)).
		WithAction(customizeResources).
		WithAction(updatePodSecurityRolebinding).
		WithAction(deploy.NewAction(
			deploy.WithFieldOwner(componentsv1.DashboardInstanceName),
			deploy.WithLabel(labels.ComponentManagedBy, componentsv1.DashboardInstanceName),
		)).
		WithAction(updatestatus.NewAction(
			updatestatus.WithSelectorLabel(labels.ComponentManagedBy, componentsv1.DashboardInstanceName),
		)).
		WithAction(updateStatus).
		Build(ctx)

	if err != nil {
		return fmt.Errorf("could not create the dashboard controller: %w", err)
	}

	return nil
}

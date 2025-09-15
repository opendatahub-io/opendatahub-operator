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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/component"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// NewComponentReconciler creates a ComponentReconciler for the Dashboard API.
func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	componentName := computeComponentName()

	_, err := reconciler.ReconcilerFor(mgr, &componentApi.Dashboard{}).
		// operands - owned
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Service{}).
		// By default, a predicated for changed generation is added by the Owns()
		// method, however for deployments, we also need to retrieve status info
		// hence we need a dedicated predicate to react to replicas status change
		Owns(&appsv1.Deployment{}, reconciler.WithPredicates(resources.NewDeploymentPredicate())).
		// operands - openshift
		Owns(&routev1.Route{}).
		Owns(&consolev1.ConsoleLink{}).
		// Those APIs are provided by the component itself hence they should
		// be watched dynamically
		OwnsGVK(gvk.AcceleratorProfile, reconciler.Dynamic()).
		OwnsGVK(gvk.OdhApplication, reconciler.Dynamic()).
		OwnsGVK(gvk.OdhDocument, reconciler.Dynamic()).
		OwnsGVK(gvk.OdhQuickStart, reconciler.Dynamic()).
		// CRDs are not owned by the component and should be left on the cluster,
		// so by default, the deploy action won't add all the annotation added to
		// other resources. Hence, a custom handling is required in order to minimize
		// chattering and avoid noisy neighborhoods
		Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.DashboardInstanceName)),
			reconciler.WithPredicates(
				component.ForLabel(labels.ODH.Component(componentName), labels.True)),
		).
		// The OdhDashboardConfig resource is expected to be created by the operator
		// but then owned by the user so we only re-create it with factory values if
		// it gets deleted
		WatchesGVK(gvk.OdhDashboardConfig,
			reconciler.Dynamic(),
			reconciler.WithPredicates(resources.Deleted()),
		).
		WatchesGVK(gvk.DashboardHardwareProfile, reconciler.WithEventHandler(
			handlers.ToNamed(componentApi.DashboardInstanceName),
		), reconciler.WithPredicates(predicate.Funcs{
			GenericFunc: func(tge event.TypedGenericEvent[client.Object]) bool { return false },
			DeleteFunc:  func(tde event.TypedDeleteEvent[client.Object]) bool { return false },
		}), reconciler.Dynamic(reconciler.CrdExists(gvk.DashboardHardwareProfile))).
		WithAction(initialize).
		WithAction(devFlags).
		WithAction(setKustomizedParams).
		WithAction(configureDependencies).
		WithAction(kustomize.NewAction(
			// Those are the default labels added by the legacy deploy method
			// and should be preserved as the original plugin were affecting
			// deployment selectors that are immutable once created, so it won't
			// be possible to actually amend the labels in a non-disruptive
			// manner.
			//
			// Additional labels/annotations MUST be added by the deploy action
			// so they would affect only objects metadata without side effects
			kustomize.WithLabel(labels.ODH.Component(componentName), labels.True),
			kustomize.WithLabel(labels.K8SCommon.PartOf, componentName),
		)).
		WithAction(customizeResources).
		WithAction(deploy.NewAction()).
		WithAction(deployments.NewAction()).
		WithAction(reconcileHardwareProfiles).
		WithAction(updateStatus).
		// must be the final action
		WithAction(gc.NewAction(
			gc.WithUnremovables(gvk.OdhDashboardConfig),
		)).
		// declares the list of additional, controller specific conditions that are
		// contributing to the controller readiness status
		WithConditions(conditionTypes...).
		Build(ctx)

	if err != nil {
		return fmt.Errorf("could not create the dashboard controller: %w", err)
	}

	return nil
}

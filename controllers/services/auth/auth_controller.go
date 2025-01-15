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

package auth

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	componentsApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	odhDashboardConfigCRDName = "odhdashboardconfigs.opendatahub.io"
)

// NewServiceReconciler creates a ServiceReconciler for the Auth API.
func NewServiceReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &serviceApi.Auth{}).
		// operands - owned
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		WatchesGVK(gvk.Dashboard).
		WatchesGVK(
			gvk.CustomResourceDefinition,
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.AuthInstanceName)),
			reconciler.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
				return object.GetName() == odhDashboardConfigCRDName
			}))).
		WatchesGVK(
			gvk.OdhDashboardConfig,
			reconciler.Dynamic(shouldWatchDashboardConfig),
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.AuthInstanceName)),
			reconciler.WithPredicates(predicates.DefaultPredicate)).
		// actions
		WithAction(initialize).
		WithAction(template.NewAction(
			template.WithCache(),
		)).
		WithAction(copyGroups).
		WithAction(managePermissions).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		WithAction(setStatus).
		Build(ctx)

	if err != nil {
		return fmt.Errorf("could not create the auth controller: %w", err)
	}

	return nil
}

func shouldWatchDashboardConfig(ctx context.Context, request *types.ReconciliationRequest) bool {
	d := resources.GvkToUnstructured(gvk.Dashboard)
	if err := request.Client.Get(ctx, client.ObjectKey{Name: componentsApi.DashboardInstanceName}, d); err != nil {
		return false
	}

	c := resources.GvkToUnstructured(gvk.CustomResourceDefinition)
	if err := request.Client.Get(ctx, client.ObjectKey{Name: odhDashboardConfigCRDName}, c); err != nil {
		return false
	}

	return true
}

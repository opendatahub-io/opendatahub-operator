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

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

func NewHandler() *ServiceHandler { return &ServiceHandler{} }

type ServiceHandler struct {
}

func (h *ServiceHandler) Init(_ common.Platform) error {
	return nil
}

func (h *ServiceHandler) GetName() string {
	return ServiceName
}

func (h *ServiceHandler) GetManagementState(platform common.Platform, _ *dsciv2.DSCInitialization) operatorv1.ManagementState {
	return operatorv1.Managed
}

func (h *ServiceHandler) NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &serviceApi.Auth{}).
		// operands - owned
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Watches(
			&corev1.Namespace{},
			reconciler.WithEventHandler(
				handlers.ToNamed(serviceApi.AuthInstanceName),
			),
			reconciler.WithPredicates(resources.CreatedOrUpdatedOrDeletedNamed("models-as-a-service")),
		).
		Watches(
			&corev1.Namespace{},
			reconciler.WithEventHandler(
				handlers.ToNamed(serviceApi.AuthInstanceName),
			),
			reconciler.WithPredicates(resources.CreatedOrUpdatedOrDeletedNamed("kuadrant-system")),
		).
		Watches(
			&rbacv1.ClusterRole{},
			reconciler.WithEventHandler(
				handlers.ToNamed(serviceApi.AuthInstanceName),
			),
		).
		// actions
		WithAction(initialize).
		WithAction(template.NewAction()).
		WithAction(createDefaultGroup).
		WithAction(managePermissions).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		// must be the final action
		WithAction(gc.NewAction()).
		Build(ctx)

	if err != nil {
		return fmt.Errorf("could not create the auth controller: %w", err)
	}

	return nil
}

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

package codeflare

import (
	"context"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/updatestatus"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/component"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// CodeFlareReconciler reconciles a CodeFlare object.

func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(
		mgr,
		&componentApi.CodeFlare{},
	).
		// customized Owns() for Component with new predicates
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Service{}).
		Owns(&admissionregistrationv1.MutatingWebhookConfiguration{}).
		Owns(&admissionregistrationv1.ValidatingWebhookConfiguration{}).
		Owns(&appsv1.Deployment{}, reconciler.WithPredicates(resources.NewDeploymentPredicate())).
		Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.CodeFlareInstanceName)),
			reconciler.WithPredicates(
				component.ForLabel(labels.ODH.Component(LegacyComponentName), labels.True)),
		).
		// Add CodeFlare-specific actions
		WithAction(initialize).
		WithAction(devFlags).
		WithAction(kustomize.NewAction(
			kustomize.WithCache(),
			kustomize.WithLabel(labels.ODH.Component(LegacyComponentName), labels.True),
			kustomize.WithLabel(labels.K8SCommon.PartOf, LegacyComponentName),
		)).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		WithAction(updatestatus.NewAction()).
		// must be final action
		WithAction(gc.NewAction()).
		Build(ctx)

	if err != nil {
		return err // no need customize error, it is done in the caller main
	}

	return nil
}

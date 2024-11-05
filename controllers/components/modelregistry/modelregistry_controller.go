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

package modelregistry

import (
	"context"
	"fmt"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// NewComponentReconciler creates a ComponentReconciler for the Dashboard API.
func NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ComponentReconcilerFor[*componentsv1.ModelRegistry](
		mgr,
		componentsv1.ModelRegistryInstanceName,
		&componentsv1.ModelRegistry{},
	).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&appsv1.Deployment{}, builder.WithPredicates(resources.NewDeploymentPredicate())).
		Owns(&admissionregistrationv1.MutatingWebhookConfiguration{}).
		Owns(&admissionregistrationv1.ValidatingWebhookConfiguration{}).
		Watches(&corev1.Namespace{}).
		Watches(&extv1.CustomResourceDefinition{}).
		// Some ClusterRoles are part of the component deployment, but not owned by
		// the operator (overlays/odh/extras), so in order to properly keep them
		// in sync with the manifests, we should also create an additional watcher
		Watches(&rbacv1.ClusterRole{}).
		// This component copies a secret referenced by the default IngressController
		// to the Istio namespace, hence we need to watch for the related secret
		// placed in the openshift-ingress namespace and with name defined in the
		// IngressController on the .spec.defaultCertificate.name path
		WatchesH(
			&corev1.Secret{},
			handlers.ToNamed(componentsv1.ModelRegistryInstanceName),
			builder.WithPredicates(ingressSecret(ctx, mgr.GetClient()))).
		// actions
		WithAction(checkPreConditions).
		WithAction(initialize).
		WithAction(configureDependencies).
		WithAction(render.NewAction(
			render.WithCache(true, render.DefaultCachingKeyFn),
			render.WithLabel(labels.ODH.Component(ComponentName), "true"),
			render.WithLabel(labels.K8SCommon.PartOf, ComponentName),
		)).
		WithAction(customizeResources).
		WithAction(deploy.NewAction(
			deploy.WithFieldOwner(componentsv1.ModelRegistryInstanceName),
			deploy.WithLabel(labels.ComponentManagedBy, componentsv1.ModelRegistryInstanceName),
		)).
		WithAction(updatestatus.NewAction(
			updatestatus.WithSelectorLabel(labels.ComponentManagedBy, componentsv1.ModelRegistryInstanceName),
		)).
		WithAction(updateStatus).
		Build(ctx)

	if err != nil {
		return fmt.Errorf("could not create the model registry controller: %w", err)
	}

	return nil
}

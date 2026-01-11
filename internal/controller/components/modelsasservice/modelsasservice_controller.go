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

package modelsasservice

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
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

// NewComponentReconciler creates a new ModelsAsService controller.
func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &componentApi.ModelsAsService{}).
		// Core Kubernetes resources deployed by MaaS manifests
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&appsv1.Deployment{}, reconciler.WithPredicates(resources.NewDeploymentPredicate())).
		// RBAC resources
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		// Gateway API resources
		Owns(&gwapiv1.HTTPRoute{}).
		Owns(&gwapiv1.Gateway{}).
		// Note: AuthPolicy (kuadrant.io/v1) is not included here as it's a third-party CRD
		// that may not be available in all environments. It should be handled by the
		// manifest deployment process.
		Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.ModelsAsServiceInstanceName)),
			reconciler.WithPredicates(
				component.ForLabel(labels.ODH.Component(ComponentName), labels.True)),
		).
		WithAction(initialize).
		WithAction(validateGateway).
		WithAction(customizeManifests).
		// WithAction(releases.NewAction()). // TODO: Do we need this? How to fix annotation of "platform.opendatahub.io/version:0.0.0"
		WithAction(kustomize.NewAction(
			// TODO: There are comments in some components mentioning these are legacy labels. Do we still need these?
			kustomize.WithLabel(labels.ODH.Component(ComponentName), labels.True),
			kustomize.WithLabel(labels.K8SCommon.PartOf, ComponentName),
		)).
		WithAction(configureMaaSGatewayHostname).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		WithAction(deployments.NewAction()).
		// must be the final action
		WithAction(gc.NewAction()).
		// declares the list of additional, controller specific conditions that are
		// contributing to the controller readiness status
		WithConditions(conditionTypes...).
		Build(ctx)
	if err != nil {
		return fmt.Errorf("could not create the ModelsAsService controller: %w", err)
	}

	return nil
}

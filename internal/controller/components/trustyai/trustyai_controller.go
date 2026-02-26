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

package trustyai

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
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
	pkgresources "github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	// InferenceServicesCRDName is the name of the InferenceServices CRD that TrustyAI depends on.
	InferenceServicesCRDName = "inferenceservices.serving.kserve.io"
)

// isInferenceServicesCRD checks if the given object is the InferenceServices CRD managed by KServe.
func isInferenceServicesCRD(obj client.Object) bool {
	// Early return: check name first (cheaper comparison)
	if obj.GetName() != InferenceServicesCRDName {
		return false
	}
	// Check if it's managed by KServe using safe label check
	return pkgresources.HasLabel(obj, labels.ODH.Component(componentApi.KserveComponentName), labels.True)
}

func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &componentApi.TrustyAI{}).
		// customized Owns() for Component with new predicates
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.Deployment{}, reconciler.WithPredicates(resources.NewDeploymentPredicate())).
		Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.TrustyAIInstanceName)),
			reconciler.WithPredicates(predicate.Or(
				component.ForLabel(labels.ODH.Component(LegacyComponentName), labels.True), // if TrustyAI CR is changed
				predicate.Funcs{ // OR if ISVC CRD from kserve is created or deleted
					CreateFunc: func(e event.CreateEvent) bool {
						// React when InferenceServices CRD is created (dependency becomes available)
						return isInferenceServicesCRD(e.Object)
					},
					UpdateFunc: func(e event.UpdateEvent) bool {
						// Don't react to updates - checkPreConditions only checks if CRD exists, not its version/spec
						// This also prevents continuous reconciliation on CRD status updates
						return false
					},
					DeleteFunc: func(e event.DeleteEvent) bool {
						// React when InferenceServices CRD is deleted (dependency becomes unavailable)
						// This triggers checkPreConditions which will detect the missing CRD and set conditions to False
						return isInferenceServicesCRD(e.Object)
					},
					GenericFunc: func(e event.GenericEvent) bool {
						// Don't match Generic events
						return false
					},
				},
			)),
		).
		WithAction(checkPreConditions).
		WithAction(initialize).
		WithAction(createConfigMap).
		WithAction(releases.NewAction()).
		WithAction(kustomize.NewAction(
			kustomize.WithLabel(labels.ODH.Component(LegacyComponentName), labels.True),
			kustomize.WithLabel(labels.K8SCommon.PartOf, LegacyComponentName),
		)).
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
		return err // no need customize error, it is done in the caller main
	}

	return nil
}

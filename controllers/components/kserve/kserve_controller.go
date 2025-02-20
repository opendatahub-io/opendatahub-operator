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

package kserve

import (
	"context"

	templatev1 "github.com/openshift/api/template/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	featuresv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/releases"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/updatestatus"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/clusterrole"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/component"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/generation"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/hash"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// NewComponentReconciler creates a ComponentReconciler for the Dashboard API.
func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	ownedViaFTMapFunc := ownedViaFT(mgr.GetClient())

	_, err := reconciler.ReconcilerFor(mgr, &componentApi.Kserve{}).
		// operands - owned
		Owns(&corev1.Secret{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.ClusterRole{}, reconciler.WithPredicates(clusterrole.IgnoreIfAggregationRule())).
		Owns(&rbacv1.ClusterRoleBinding{}).
		// The ovms template gets a new resourceVersion periodically without any other
		// changes. The compareHashPredicate ensures that we don't needlessly enqueue
		// requests if there are no changes that we don't care about.
		Owns(&templatev1.Template{}, reconciler.WithPredicates(hash.Updated())).
		Owns(&featuresv1.FeatureTracker{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&monitoringv1.ServiceMonitor{}).
		Owns(&admissionregistrationv1.MutatingWebhookConfiguration{}).
		Owns(&admissionregistrationv1.ValidatingWebhookConfiguration{}).
		Owns(&appsv1.Deployment{}, reconciler.WithPredicates(resources.NewDeploymentPredicate())).
		// operands - watched
		//
		// By default the Watches functions adds:
		// - an event handler mapping to a cluster scope resource identified by the
		//   components.platform.opendatahub.io/managed-by annotation
		// - a predicate that check for generation change for Delete/Updates events
		//   for to objects that have the label components.platform.opendatahub.io/managed-by
		//   set to the current owner
		Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(
				handlers.ToNamed(componentApi.KserveInstanceName)),
			reconciler.WithPredicates(predicate.And(
				component.ForLabel(labels.ODH.Component(LegacyComponentName), labels.True),
				predicate.Funcs{
					UpdateFunc: func(event event.UpdateEvent) bool {
						// The KServe and ModelMesh are shipping the same CRDs as part of their manifests
						// but with different versions, this cause the respective component reconcilers to
						// keep trying to install their respective version, ending in an infinite loop.
						switch event.ObjectNew.GetName() {
						case "inferenceservices.serving.kserve.io":
							return false
						case "servingruntimes.serving.kserve.io":
							return false
						}
						return true
					},
				},
			)),
		).
		// resource
		Watches(
			&dsciv1.DSCInitialization{},
			reconciler.WithEventHandler(handlers.ToNamed(componentApi.KserveInstanceName)),
			reconciler.WithPredicates(predicate.Or(generation.New(), resources.DSCIReadiness)),
		).
		// operands - dynamically watched
		//
		// A watch will be created dynamically for these kinds, if they exist on the cluster
		// (they come from ServiceMesh and Serverless operators).
		//
		// They're owned by FeatureTrackers, which are owned by a Kserve; so there's a
		// custom event mapper to enqueue a reconcile request for a Kserve object, if
		// applicable.
		//
		// They also don't have the "partOf" label that Watches expects in the
		// implicit predicate, so the simpler "DefaultPredicate" is also added.
		WatchesGVK(
			gvk.KnativeServing,
			reconciler.Dynamic(),
			reconciler.WithEventMapper(ownedViaFTMapFunc),
			reconciler.WithPredicates(predicates.DefaultPredicate)).
		WatchesGVK(
			gvk.ServiceMeshMember,
			reconciler.Dynamic(),
			reconciler.WithEventMapper(ownedViaFTMapFunc),
			reconciler.WithPredicates(predicates.DefaultPredicate)).
		WatchesGVK(
			gvk.EnvoyFilter,
			reconciler.Dynamic(),
			reconciler.WithEventMapper(ownedViaFTMapFunc),
			reconciler.WithPredicates(predicates.DefaultPredicate)).
		WatchesGVK(
			gvk.AuthorizationPolicy,
			reconciler.Dynamic(),
			reconciler.WithEventMapper(ownedViaFTMapFunc),
			reconciler.WithPredicates(predicates.DefaultPredicate)).
		WatchesGVK(
			gvk.Gateway,
			reconciler.Dynamic(),
			reconciler.WithEventMapper(ownedViaFTMapFunc),
			reconciler.WithPredicates(predicates.DefaultPredicate)).

		// actions
		WithAction(checkPreConditions).
		WithAction(initialize).
		WithAction(devFlags).
		WithAction(releases.NewAction()).
		WithAction(removeLegacyFeatureTrackerOwnerRef).
		WithAction(configureServerless).
		WithAction(configureServiceMesh).
		WithAction(kustomize.NewAction(
			kustomize.WithCache(),
			// These are the default labels added by the legacy deploy method
			// and should be preserved as the original plugin were affecting
			// deployment selectors that are immutable once created, so it won't
			// be possible to actually amend the labels in a non-disruptive
			// manner.
			//
			// Additional labels/annotations MUST be added by the deploy action
			// so they would affect only objects metadata without side effects
			kustomize.WithLabel(labels.ODH.Component(LegacyComponentName), labels.True),
			kustomize.WithLabel(labels.K8SCommon.PartOf, LegacyComponentName),
		)).
		WithAction(customizeKserveConfigMap).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		WithAction(setStatusFields).
		WithAction(updatestatus.NewAction()).
		// must be the final action
		WithAction(gc.NewAction()).
		Build(ctx)

	return err
}

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

// Package datasciencecluster contains controller logic of CRD DataScienceCluster
package datasciencecluster

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/dependent"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func NewDataScienceClusterReconciler(ctx context.Context, mgr ctrl.Manager) error {
	componentsPredicate := dependent.New(dependent.WithWatchStatus(true))

	b := reconciler.ReconcilerFor(mgr, &dscv2.DataScienceCluster{}).
		Owns(&componentApi.Dashboard{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.Workbenches{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.Ray{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.ModelRegistry{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.TrustyAI{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.Kueue{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.TrainingOperator{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.Trainer{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.DataSciencePipelines{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.Kserve{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.ModelController{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.ModelsAsService{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.FeastOperator{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.OGX{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.MLflowOperator{}, reconciler.WithPredicates(componentsPredicate)).
		Owns(&componentApi.SparkOperator{}, reconciler.WithPredicates(componentsPredicate)).
		WatchesGVK(gvk.Tenant,
			reconciler.Dynamic(reconciler.CrdExists(gvk.Tenant)),
			reconciler.WithEventMapper(func(ctx context.Context, _ client.Object) []reconcile.Request {
				return watchDataScienceClusters(ctx, mgr.GetClient())
			}),
			reconciler.WithPredicates(componentsPredicate),
		)

	// Watch module CRs so that status changes (e.g. AIGateway transitioning
	// to Ready) trigger DSC reconciliation and propagate to ModulesReady.
	// ForAll iterates every registered module regardless of enabled state;
	// the Dynamic predicate defers actual watch setup until the CRD exists.
	for _, moduleGVK := range collectModuleGVKs(modules.DefaultRegistry()) {
		b = b.WatchesGVK(moduleGVK,
			reconciler.Dynamic(reconciler.CrdExists(moduleGVK)),
			reconciler.WithEventMapper(func(ctx context.Context, _ client.Object) []reconcile.Request {
				return watchDataScienceClusters(ctx, mgr.GetClient())
			}),
			reconciler.WithPredicates(componentsPredicate),
		)
	}

	_, err := b.Watches(
		&dsciv2.DSCInitialization{},
		reconciler.WithEventMapper(func(ctx context.Context, _ client.Object) []reconcile.Request {
			return watchDataScienceClusters(ctx, mgr.GetClient())
		})).
		Watches(
			&serviceApi.GatewayConfig{},
			reconciler.WithEventMapper(func(ctx context.Context, _ client.Object) []reconcile.Request {
				return watchDataScienceClusters(ctx, mgr.GetClient())
			}),
			reconciler.WithPredicates(resources.GatewayConfigDomainChanged())).
		Watches(
			&corev1.ConfigMap{},
			reconciler.WithEventMapper(func(ctx context.Context, _ client.Object) []reconcile.Request {
				return watchDataScienceClusters(ctx, mgr.GetClient())
			}),
			reconciler.WithPredicates(
				resources.CreatedOrUpdatedOrDeletedNamed(gates.AcksConfigMap),
			)).
		WithAction(initialize).
		WithAction(checkPreConditions).
		WithAction(updateStatus).
		WithAction(checkUpgradeGates).
		WithAction(provisionComponents).
		WithAction(deploy.NewAction(
			deploy.WithCache()),
		).
		WithAction(gc.NewAction(
			gc.WithTypePredicate(
				func(rr *types.ReconciliationRequest, objGVK schema.GroupVersionKind) (bool, error) {
					return rr.Controller.Owns(objGVK), nil
				},
			),
		)).
		WithConditions(status.ConditionTypeComponentsReady, status.ConditionTypeModulesReady).
		Build(ctx)

	if err != nil {
		return err
	}

	return nil
}

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

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/dependent"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

func NewDataScienceClusterReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &dscv2.DataScienceCluster{}).
		WithDynamicOwnership(
			reconciler.WithDefaultPredicates(dependent.New(dependent.WithWatchStatus(true))),
		).
		WatchesGVK(gvk.MaasTenantConfig,
			reconciler.Dynamic(reconciler.CrdExists(gvk.MaasTenantConfig)),
			reconciler.WithEventMapper(func(ctx context.Context, _ client.Object) []reconcile.Request {
				return watchDataScienceClusters(ctx, mgr.GetClient())
			}),
			reconciler.WithPredicates(dependent.New(dependent.WithWatchStatus(true))),
		).
		Watches(
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
		WithAction(initialize).
		WithAction(checkPreConditions).
		WithAction(updateStatus).
		WithAction(syncPlatformCR).
		WithAction(cleanupDisabledComponents).
		WithAction(cleanupDisabledModuleCRs).
		WithAction(provisionComponents).
		WithAction(provisionModuleCRs).
		WithAction(deploy.NewAction(
			deploy.WithContinueOnError(),
			deploy.WithCache(),
		)).
		WithConditions(status.ConditionTypeComponentsReady, status.ConditionTypeModulesReady).
		Build(ctx)

	if err != nil {
		return err
	}

	return nil
}

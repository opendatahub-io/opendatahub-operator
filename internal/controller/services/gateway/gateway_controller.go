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

package gateway

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

// NewReconciler creates and configures a new reconciler for GatewayConfig resources.
// It sets up ownership relationships and action chains for complete gateway lifecycle management.
func (h *ServiceHandler) NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	// Note: Input validation for mgr == nil is handled by the reconciler.ReconcilerFor method
	// which will panic as expected by existing tests

	// Build reconciler with optimized chain structure
	reconcilerBuilder := reconciler.ReconcilerFor(mgr, &serviceApi.GatewayConfig{}).
		// Core Gateway API resources
		OwnsGVK(gvk.GatewayClass).
		OwnsGVK(gvk.KubernetesGateway).
		// Service mesh resources (conditionally owned based on CRD existence)
		OwnsGVK(gvk.EnvoyFilter,
			reconciler.Dynamic(reconciler.CrdExists(gvk.EnvoyFilter))).
		OwnsGVK(gvk.DestinationRule,
			reconciler.Dynamic(reconciler.CrdExists(gvk.DestinationRule)))

	// Add Kubernetes native resources for auth proxy
	reconcilerBuilder = reconcilerBuilder.
		OwnsGVK(gvk.Deployment). // Auth proxy deployment
		OwnsGVK(gvk.Service).    // Auth proxy service
		OwnsGVK(gvk.Secret).     // Auth proxy credentials
		OwnsGVK(gvk.HTTPRoute)   // OAuth callback route only

	// Only watch OAuthClient if cluster uses IntegratedOAuth (not OIDC or None)
	// This prevents errors in ROSA environments where OAuthClient CRD doesn't exist
	if isIntegratedOAuth, err := cluster.IsIntegratedOAuth(ctx, mgr.GetClient()); err == nil && isIntegratedOAuth {
		reconcilerBuilder = reconcilerBuilder.OwnsGVK(gvk.OAuthClient) // OpenShift OAuth integration
	}

	// Watch DSCInitialization to trigger reconciliation when DSCI becomes available
	reconcilerBuilder = reconcilerBuilder.
		Watches(
			&dsciv2.DSCInitialization{},
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayInstanceName)),
			reconciler.WithPredicates(predicate.GenerationChangedPredicate{}),
		)

	// Watch ingress certificate secrets to trigger reconciliation when certificates are rotated
	// This ensures gateway certificates are automatically updated when the source certificate changes
	reconcilerBuilder = reconcilerBuilder.
		Watches(
			&corev1.Secret{},
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayInstanceName)),
			reconciler.WithPredicates(
				predicate.Funcs{
					CreateFunc: func(e event.CreateEvent) bool {
						return isIngressCertificateSecret(ctx, mgr.GetClient(), e.Object)
					},
					UpdateFunc: func(e event.UpdateEvent) bool {
						return isIngressCertificateSecret(ctx, mgr.GetClient(), e.ObjectNew)
					},
					DeleteFunc: func(e event.DeleteEvent) bool {
						return isIngressCertificateSecret(ctx, mgr.GetClient(), e.Object)
					},
				},
			),
		)

	// Configure action chain for resource lifecycle
	reconcilerBuilder = reconcilerBuilder.
		WithAction(createGatewayInfrastructure).          // Core gateway setup
		WithAction(createKubeAuthProxyInfrastructure).    // Authentication proxy
		WithAction(createEnvoyFilter).                    // Service mesh integration
		WithAction(createDestinationRule).                // Traffic management
		WithAction(template.NewAction()).                 // Template rendering
		WithAction(deploy.NewAction(deploy.WithCache())). // Resource deployment with caching
		WithAction(syncGatewayConfigStatus).              // Status synchronization
		WithAction(gc.NewAction())                        // Garbage collection

	// Build and validate the reconciler
	if _, err := reconcilerBuilder.Build(ctx); err != nil {
		return fmt.Errorf("could not create the Gateway controller: %w", err)
	}

	return nil
}

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

	operatorv1 "github.com/openshift/api/operator/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/auth"
	sr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

//nolint:gochecknoinits
func init() {
	sr.Add(&ServiceHandler{})
}

// ServiceHandler implements the ServiceHandler interface for Gateway services.
// It manages the lifecycle of GatewayConfig resources and their associated infrastructure.
type ServiceHandler struct{}

// Init initializes the ServiceHandler for the given platform.
// Currently no platform-specific initialization is required.
func (h *ServiceHandler) Init(platform common.Platform) error {
	return nil
}

// GetName returns the service name for this handler.
func (h *ServiceHandler) GetName() string {
	return ServiceName
}

// GetManagementState returns the management state for Gateway services.
// Gateway services are always managed regardless of platform or DSCI configuration.
func (h *ServiceHandler) GetManagementState(platform common.Platform, _ *dsciv2.DSCInitialization) operatorv1.ManagementState {
	return operatorv1.Managed
}

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
	if isIntegratedOAuth, err := auth.IsDefaultAuthMethod(ctx, mgr.GetClient()); err == nil && isIntegratedOAuth {
		reconcilerBuilder = reconcilerBuilder.OwnsGVK(gvk.OAuthClient) // OpenShift OAuth integration
	}
	// Note: Dashboard HTTPRoute and ReferenceGrant are user's responsibility

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

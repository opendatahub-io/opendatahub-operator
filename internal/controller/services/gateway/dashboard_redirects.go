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

package gateway

// Dashboard redirect RBAC permissions
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

import (
	"context"

	routev1 "github.com/openshift/api/route/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	dashboardRedirectConfigMapTemplate          = "resources/dashboard-redirect-configmap.tmpl.yaml"
	dashboardRedirectDeploymentTemplate         = "resources/dashboard-redirect-deployment.tmpl.yaml"
	dashboardRedirectServiceTemplate            = "resources/dashboard-redirect-service.tmpl.yaml"
	dashboardRedirectDashboardRouteTemplate     = "resources/dashboard-redirect-dashboard-route.tmpl.yaml"
	dashboardRedirectLegacyGatewayRouteTemplate = "resources/dashboard-redirect-legacy-gateway-route.tmpl.yaml"
)

// createDashboardRedirects creates nginx-based redirect resources for legacy dashboard and gateway URLs.
// This helps users transition from old route URLs to the new Gateway API URLs without breaking bookmarks.
func createDashboardRedirects(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createDashboardRedirects")

	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return err
	}

	// Only create redirects in OcpRoute mode (redirects don't make sense for LoadBalancer mode)
	if gatewayConfig.Spec.IngressMode != serviceApi.IngressModeOcpRoute {
		l.V(1).Info("IngressMode is not OcpRoute, skipping dashboard redirect creation")
		return nil
	}

	shouldCreate, err := shouldCreateDashboardRedirects(ctx, rr)
	if err != nil {
		return err
	}

	if !shouldCreate {
		l.V(1).Info("Dashboard redirects not needed, skipping")
		return nil
	}

	// Determine if we should create legacy gateway redirect
	createLegacyGateway := shouldCreateLegacyGatewayRedirect(gatewayConfig)

	l.V(1).Info("Creating dashboard redirect resources",
		"dashboardRouteName", getDashboardRouteName(),
		"namespace", cluster.GetApplicationNamespace(),
		"createLegacyGatewayRedirect", createLegacyGateway,
		"currentSubdomain", getCurrentSubdomain(gatewayConfig))

	// Add templates to reconciliation request
	rr.Templates = append(rr.Templates,
		odhtypes.TemplateInfo{FS: gatewayResources, Path: dashboardRedirectConfigMapTemplate},
		odhtypes.TemplateInfo{FS: gatewayResources, Path: dashboardRedirectDeploymentTemplate},
		odhtypes.TemplateInfo{FS: gatewayResources, Path: dashboardRedirectServiceTemplate},
		odhtypes.TemplateInfo{FS: gatewayResources, Path: dashboardRedirectDashboardRouteTemplate},
	)

	// Conditionally add legacy gateway redirect route
	if createLegacyGateway {
		rr.Templates = append(rr.Templates,
			odhtypes.TemplateInfo{FS: gatewayResources, Path: dashboardRedirectLegacyGatewayRouteTemplate},
		)
	}

	return nil
}

// shouldCreateDashboardRedirects determines whether dashboard redirect resources should be created.
// Uses auto-detection: returns true if old dashboard route exists.
func shouldCreateDashboardRedirects(ctx context.Context, rr *odhtypes.ReconciliationRequest) (bool, error) {
	l := logf.FromContext(ctx).WithName("shouldCreateDashboardRedirects")

	// Auto-detect: check if old dashboard route exists
	oldRouteName := getDashboardRouteName()
	appNamespace := cluster.GetApplicationNamespace()

	route := &routev1.Route{}
	err := rr.Client.Get(ctx, client.ObjectKey{
		Name:      oldRouteName,
		Namespace: appNamespace,
	}, route)

	if k8serr.IsNotFound(err) {
		l.V(1).Info("Old dashboard route not found, skipping redirects",
			"routeName", oldRouteName,
			"namespace", appNamespace)
		return false, nil // Old route doesn't exist - skip redirects
	}
	if err != nil {
		l.Error(err, "Failed to check for old dashboard route",
			"routeName", oldRouteName,
			"namespace", appNamespace)
		return false, err
	}

	// Log route discovery and current ownership for debugging SSA takeover
	l.Info("Found existing dashboard route, will create redirects",
		"routeName", oldRouteName,
		"namespace", appNamespace,
		"routeHost", route.Spec.Host)

	// Log current ownership references (helps debug SSA ownership takeover)
	if len(route.OwnerReferences) > 0 {
		for _, owner := range route.OwnerReferences {
			l.V(1).Info("Existing route owner reference (will be replaced by SSA)",
				"routeName", oldRouteName,
				"ownerKind", owner.Kind,
				"ownerName", owner.Name,
				"ownerUID", owner.UID,
				"controller", owner.Controller != nil && *owner.Controller,
				"blockOwnerDeletion", owner.BlockOwnerDeletion != nil && *owner.BlockOwnerDeletion)
		}
	} else {
		l.V(1).Info("Existing route has no owner references", "routeName", oldRouteName)
	}

	return true, nil // Old route exists - create redirects
}

// getDashboardRouteName returns the platform-specific dashboard route name.
// ODH: "odh-dashboard"
// RHOAI (self-managed and managed): "rhods-dashboard".
func getDashboardRouteName() string {
	release := cluster.GetRelease()

	switch release.Name {
	case cluster.OpenDataHub:
		return "odh-dashboard"
	case cluster.SelfManagedRhoai, cluster.ManagedRhoai:
		return "rhods-dashboard"
	default:
		return "odh-dashboard" // Fallback to ODH
	}
}

// shouldCreateLegacyGatewayRedirect determines if we should create the legacy gateway redirect route.
// Returns true if current subdomain is NOT "data-science-gateway" (meaning we need to redirect FROM legacy).
func shouldCreateLegacyGatewayRedirect(gc *serviceApi.GatewayConfig) bool {
	currentSubdomain := getCurrentSubdomain(gc)

	// Only create legacy gateway redirect if current subdomain is NOT data-science-gateway
	return currentSubdomain != LegacyGatewaySubdomain
}

// getCurrentSubdomain extracts the current subdomain from GatewayConfig or returns the default.
func getCurrentSubdomain(gc *serviceApi.GatewayConfig) string {
	if gc != nil && gc.Spec.Subdomain != "" {
		return gc.Spec.Subdomain
	}
	return DefaultGatewaySubdomain
}

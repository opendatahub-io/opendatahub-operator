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

// Dashboard redirect feature can be disabled by setting the DISABLE_DASHBOARD_REDIRECTS
// environment variable to "true" in the operator's Subscription:
//
//   apiVersion: operators.coreos.com/v1alpha1
//   kind: Subscription
//   metadata:
//     name: rhods-operator
//     namespace: redhat-ods-operator
//   spec:
//     config:
//       env:
//         - name: DISABLE_DASHBOARD_REDIRECTS
//           value: "true"
//
// By default (when not set or set to any value other than "true"), dashboard redirects
// are ENABLED for all Gateway configurations.

import (
	"context"
	"os"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

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

	// Check if feature is explicitly disabled via operator environment variable
	if os.Getenv("DISABLE_DASHBOARD_REDIRECTS") == "true" {
		l.Info("Dashboard redirects disabled via DISABLE_DASHBOARD_REDIRECTS environment variable")
		return nil
	}

	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return err
	}

	l.V(1).Info("Creating dashboard redirect resources",
		"dashboardRouteName", getDashboardRouteName(),
		"namespace", cluster.GetApplicationNamespace(),
		"currentSubdomain", getCurrentSubdomain(gatewayConfig))

	// Add templates to reconciliation request
	// Note: Legacy gateway redirect template uses {{- if .LegacySubdomain }} to conditionally render
	rr.Templates = append(rr.Templates,
		odhtypes.TemplateInfo{FS: gatewayResources, Path: dashboardRedirectConfigMapTemplate},
		odhtypes.TemplateInfo{FS: gatewayResources, Path: dashboardRedirectDeploymentTemplate},
		odhtypes.TemplateInfo{FS: gatewayResources, Path: dashboardRedirectServiceTemplate},
		odhtypes.TemplateInfo{FS: gatewayResources, Path: dashboardRedirectDashboardRouteTemplate},
		odhtypes.TemplateInfo{FS: gatewayResources, Path: dashboardRedirectLegacyGatewayRouteTemplate},
	)

	return nil
}

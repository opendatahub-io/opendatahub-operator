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
	"fmt"
	"os"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
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
//
// When Dashboard is removed (or redirects are disabled), this action explicitly deletes
// the redirect resources rather than relying on GC. The GC action uses the owner CR's
// metadata.generation to identify stale resources, but Dashboard removal does not change
// GatewayConfig's generation, so GC would never clean them up.
func createDashboardRedirects(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createDashboardRedirects")

	// Check if feature is explicitly disabled via operator environment variable
	if os.Getenv("DISABLE_DASHBOARD_REDIRECTS") == "true" {
		l.Info("Dashboard redirects disabled via DISABLE_DASHBOARD_REDIRECTS environment variable")
		return deleteDashboardRedirectResources(ctx, rr)
	}

	// When the Dashboard component is not deployed there is no dashboard route
	// to redirect from, so the redirect pods serve no purpose.
	dashboard := &componentApi.Dashboard{}
	if err := rr.Client.Get(ctx, client.ObjectKey{Name: componentApi.DashboardInstanceName}, dashboard); err != nil {
		if k8serr.IsNotFound(err) {
			l.Info("Dashboard CR not found, cleaning up dashboard redirect resources")
			return deleteDashboardRedirectResources(ctx, rr)
		}
		return fmt.Errorf("failed to check Dashboard CR: %w", err)
	}

	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return err
	}

	l.Info("Creating dashboard redirect resources",
		"dashboardRouteName", GetDashboardRouteName(),
		"namespace", cluster.GetApplicationNamespace(),
		"currentSubdomain", getCurrentSubdomain(gatewayConfig))

	// Add templates to reconciliation request
	// Note: Legacy gateway redirect template uses {{- if .LegacyHostname }} to conditionally render
	rr.Templates = append(rr.Templates,
		odhtypes.TemplateInfo{FS: gatewayResources, Path: dashboardRedirectConfigMapTemplate},
		odhtypes.TemplateInfo{FS: gatewayResources, Path: dashboardRedirectDeploymentTemplate},
		odhtypes.TemplateInfo{FS: gatewayResources, Path: dashboardRedirectServiceTemplate},
		odhtypes.TemplateInfo{FS: gatewayResources, Path: dashboardRedirectDashboardRouteTemplate},
		odhtypes.TemplateInfo{FS: gatewayResources, Path: dashboardRedirectLegacyGatewayRouteTemplate},
	)

	return nil
}

// deleteDashboardRedirectResources explicitly removes redirect resources that were
// previously created by createDashboardRedirects.
func deleteDashboardRedirectResources(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("deleteDashboardRedirectResources")
	ns := cluster.GetApplicationNamespace()

	names := []struct {
		gvk  schema.GroupVersionKind
		name string
	}{
		{gvk: gvk.Deployment, name: DashboardRedirectName},
		{gvk: gvk.Service, name: DashboardRedirectName},
		{gvk: gvk.ConfigMap, name: DashboardRedirectConfigName},
		{gvk: gvk.Route, name: GetDashboardRouteName()},
		{gvk: gvk.Route, name: LegacyGatewaySubdomain},
	}

	for _, r := range names {
		obj := resources.GvkToUnstructured(r.gvk)
		obj.SetName(r.name)
		obj.SetNamespace(ns)

		// Check existence via cached Get to avoid unnecessary Delete API calls on every reconcile
		if err := rr.Client.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			if k8serr.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to get dashboard redirect resource %s/%s: %w", obj.GetKind(), r.name, err)
		}
		if err := rr.Client.Delete(ctx, obj); err != nil {
			if k8serr.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to delete dashboard redirect resource %s/%s: %w", obj.GetKind(), r.name, err)
		}
		l.Info("Deleted dashboard redirect resource", "kind", obj.GetKind(), "name", r.name, "namespace", ns)
	}

	return nil
}

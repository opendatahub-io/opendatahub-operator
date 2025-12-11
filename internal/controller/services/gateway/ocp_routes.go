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

// +kubebuilder:rbac:groups=route.openshift.io,resources=routes/custom-host,verbs=create;patch

import (
	"context"
	"fmt"

	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

const (
	// Label to track which HTTPRoute an OCP Route was created for
	HTTPRouteLabelKey          = "gateway.opendatahub.io/httproute-name"
	HTTPRouteNamespaceLabelKey = "gateway.opendatahub.io/httproute-namespace"
)

// GatewayServiceFullName is the name of the auto-created Gateway service.
// Format: <gateway-name>-<gatewayclass-name>
var GatewayServiceFullName = DefaultGatewayName + "-" + GatewayClassName

// httpRouteReferencesGateway checks if an HTTPRoute references our gateway.
func httpRouteReferencesGateway(httpRoute *gwapiv1.HTTPRoute) bool {
	for _, ref := range httpRoute.Spec.ParentRefs {
		// Check if it references our gateway
		refNamespace := GatewayNamespace
		if ref.Namespace != nil {
			refNamespace = string(*ref.Namespace)
		}
		if string(ref.Name) == DefaultGatewayName && refNamespace == GatewayNamespace {
			return true
		}
	}
	return false
}

// HTTPRouteGatewayRefPredicate filters HTTPRoutes that reference our gateway.
func HTTPRouteGatewayRefPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			httpRoute, ok := e.Object.(*gwapiv1.HTTPRoute)
			if !ok {
				return false
			}
			return httpRouteReferencesGateway(httpRoute)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			httpRoute, ok := e.ObjectNew.(*gwapiv1.HTTPRoute)
			if !ok {
				return false
			}
			return httpRouteReferencesGateway(httpRoute)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			httpRoute, ok := e.Object.(*gwapiv1.HTTPRoute)
			if !ok {
				return false
			}
			return httpRouteReferencesGateway(httpRoute)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			httpRoute, ok := e.Object.(*gwapiv1.HTTPRoute)
			if !ok {
				return false
			}
			return httpRouteReferencesGateway(httpRoute)
		},
	}
}

// getOCPRouteName generates the OCP Route name from HTTPRoute namespace and name.
func getOCPRouteName(httpRouteNamespace, httpRouteName string) string {
	return fmt.Sprintf("%s-%s", httpRouteNamespace, httpRouteName)
}

// createOCPRoutes creates or deletes OCP Routes based on HTTPRoutes attached to the gateway.
// This action is called during GatewayConfig reconciliation.
func createOCPRoutes(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createOCPRoutes")

	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return err
	}

	// Only create OCP Routes when in OcpRoute mode
	if gatewayConfig.Spec.IngressMode != serviceApi.IngressModeOcpRoute {
		l.V(1).Info("IngressMode is not OcpRoute, skipping OCP Route reconciliation")
		return nil
	}

	// List all HTTPRoutes that reference our gateway
	httpRouteList := &gwapiv1.HTTPRouteList{}
	if err := rr.Client.List(ctx, httpRouteList); err != nil {
		return fmt.Errorf("failed to list HTTPRoutes: %w", err)
	}

	// Track which OCP Routes should exist
	desiredRoutes := make(map[string]struct{})

	for i := range httpRouteList.Items {
		httpRoute := &httpRouteList.Items[i]
		if !httpRouteReferencesGateway(httpRoute) {
			continue
		}

		// Create OCP Route for each hostname in the HTTPRoute
		for _, hostname := range httpRoute.Spec.Hostnames {
			routeName := getOCPRouteName(httpRoute.Namespace, httpRoute.Name)
			desiredRoutes[routeName] = struct{}{}

			if err := createOrUpdateOCPRoute(ctx, rr, gatewayConfig, httpRoute, string(hostname)); err != nil {
				return fmt.Errorf("failed to create OCP Route for HTTPRoute %s/%s: %w",
					httpRoute.Namespace, httpRoute.Name, err)
			}
			l.V(1).Info("Ensured OCP Route exists",
				"route", routeName,
				"hostname", string(hostname),
				"httpRoute", httpRoute.Name,
				"httpRouteNamespace", httpRoute.Namespace)
		}
	}

	// Clean up orphaned OCP Routes (routes we created but HTTPRoute no longer exists)
	if err := cleanupOrphanedOCPRoutes(ctx, rr, gatewayConfig, desiredRoutes); err != nil {
		return fmt.Errorf("failed to cleanup orphaned OCP Routes: %w", err)
	}

	return nil
}

// createOrUpdateOCPRoute creates or updates an OCP Route for an HTTPRoute hostname.
func createOrUpdateOCPRoute(
	ctx context.Context,
	rr *odhtypes.ReconciliationRequest,
	gatewayConfig *serviceApi.GatewayConfig,
	httpRoute *gwapiv1.HTTPRoute,
	hostname string,
) error {
	routeName := getOCPRouteName(httpRoute.Namespace, httpRoute.Name)
	weight := int32(100)

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: GatewayNamespace,
			Labels: map[string]string{
				labels.PlatformPartOf:      PartOfGatewayConfig,
				HTTPRouteLabelKey:          httpRoute.Name,
				HTTPRouteNamespaceLabelKey: httpRoute.Namespace,
			},
			Annotations: map[string]string{
				"router.openshift.io/service-ca-certificate": "true",
			},
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, rr.Client, route, func() error {
		route.Spec = routev1.RouteSpec{
			Host: hostname,
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   GatewayServiceFullName,
				Weight: &weight,
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromInt(StandardHTTPSPort),
			},
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationReencrypt,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
			},
		}
		// Set GatewayConfig as owner so routes are cleaned up when GatewayConfig is deleted
		return controllerutil.SetControllerReference(gatewayConfig, route, rr.Client.Scheme())
	})

	return err
}

// cleanupOrphanedOCPRoutes removes OCP Routes that were created by us but are no longer needed.
func cleanupOrphanedOCPRoutes(
	ctx context.Context,
	rr *odhtypes.ReconciliationRequest,
	gatewayConfig *serviceApi.GatewayConfig,
	desiredRoutes map[string]struct{},
) error {
	l := logf.FromContext(ctx).WithName("cleanupOrphanedOCPRoutes")

	// List all OCP Routes in the gateway namespace with our label
	routeList := &routev1.RouteList{}
	if err := rr.Client.List(ctx, routeList,
		client.InNamespace(GatewayNamespace),
		client.MatchingLabels{labels.PlatformPartOf: PartOfGatewayConfig},
	); err != nil {
		return fmt.Errorf("failed to list OCP Routes: %w", err)
	}

	for i := range routeList.Items {
		route := &routeList.Items[i]
		// Check if this route was created for an HTTPRoute (has our tracking labels)
		if _, hasLabel := route.Labels[HTTPRouteLabelKey]; !hasLabel {
			continue
		}

		// Check if this route should still exist
		if _, exists := desiredRoutes[route.Name]; exists {
			continue
		}

		// Route is orphaned, delete it
		l.V(1).Info("Deleting orphaned OCP Route", "route", route.Name)
		if err := rr.Client.Delete(ctx, route); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete orphaned OCP Route %s: %w", route.Name, err)
		}
	}

	return nil
}

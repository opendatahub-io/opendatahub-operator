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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// GatewayServiceFullName is the name of the auto-created Gateway service.
// Format: <gateway-name>-<gatewayclass-name>.
var GatewayServiceFullName = DefaultGatewayName + "-" + GatewayClassName

// httpRouteReferencesGateway checks if an HTTPRoute references our gateway.
func httpRouteReferencesGateway(httpRoute *gwapiv1.HTTPRoute) bool {
	for _, ref := range httpRoute.Spec.ParentRefs {
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

// createOCPRoutes creates a single OCP Route for the Gateway when in OcpRoute mode.
func createOCPRoutes(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createOCPRoutes")

	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return err
	}

	if gatewayConfig.Spec.IngressMode != serviceApi.IngressModeOcpRoute {
		l.V(1).Info("IngressMode is not OcpRoute, skipping OCP Route creation")
		return nil
	}

	fqdn, err := GetFQDN(ctx, rr.Client, gatewayConfig)
	if err != nil {
		return fmt.Errorf("failed to get FQDN for OCP Route: %w", err)
	}

	weight := int32(100)
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultGatewayName,
			Namespace: GatewayNamespace,
			Labels: map[string]string{
				labels.PlatformPartOf: PartOfGatewayConfig,
			},
			Annotations: map[string]string{
				"router.openshift.io/service-ca-certificate": "true",
			},
		},
		Spec: routev1.RouteSpec{
			Host: fqdn,
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
		},
	}

	if err := rr.AddResources(route); err != nil {
		return fmt.Errorf("failed to add OCP Route for Gateway: %w", err)
	}

	l.V(1).Info("Added Gateway OCP Route to reconciliation",
		"route", DefaultGatewayName,
		"hostname", fqdn)

	return nil
}

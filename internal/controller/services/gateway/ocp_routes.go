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

// +kubebuilder:rbac:groups=route.openshift.io,resources=routes/custom-host,verbs=create;patch;update
// +kubebuilder:rbac:groups=operator.openshift.io,resources=ingresscontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups=config.openshift.io,resources=ingresses,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=create;delete;get;list;patch;update;watch

import (
	"bytes"
	"context"
	"fmt"
	gotemplate "text/template"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	templateutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/template"
)

const (
	serviceCAAnnotation = "router.openshift.io/service-ca-certificate"
)

// resolveOCPRouteServerTimeout preserves explicit overrides and otherwise applies
// a 60s minimum based on the default IngressController. Automatic values are only
// rendered onto the shared Route, so later router changes can restore inheritance.
func resolveOCPRouteServerTimeout(ctx context.Context, cli client.Client, config *serviceApi.GatewayConfig) (string, error) {
	if config.Spec.IngressMode != serviceApi.IngressModeOcpRoute {
		return "", nil
	}
	if config.Spec.OCPRoute != nil && config.Spec.OCPRoute.ServerTimeout != "" {
		return config.Spec.OCPRoute.ServerTimeout, nil
	}

	ingress := &operatorv1.IngressController{}
	err := cli.Get(ctx, cluster.IngressControllerName, ingress)
	switch {
	case k8serr.IsNotFound(err), meta.IsNoMatchError(err):
		logf.FromContext(ctx).V(1).Info("Default IngressController unavailable; leaving Route timeout unset")
		return "", nil
	case err != nil:
		return "", fmt.Errorf("failed to read default IngressController server timeout: %w", err)
	}

	// OpenShift uses 30s when serverTimeout is unset or non-positive.
	serverTimeout := 30 * time.Second
	if configured := ingress.Spec.TuningOptions.ServerTimeout; configured != nil && configured.Duration > 0 {
		serverTimeout = configured.Duration
	}
	if serverTimeout < time.Minute {
		return "60s", nil
	}
	return "", nil
}

// createOCPRoutes adds OCP Route template when in OcpRoute mode.
func createOCPRoutes(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createOCPRoutes")

	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return err
	}
	if rejectUnsupportedKubernetesGatewaySpec(rr, gatewayConfig) {
		return nil
	}

	if gatewayConfig.Spec.IngressMode != serviceApi.IngressModeOcpRoute {
		l.V(1).Info("IngressMode is not OcpRoute, skipping OCP Route creation")
		return nil
	}

	unresolved, err := isGatewayDomainUnresolved(ctx, rr, gatewayConfig)
	if err != nil {
		return err
	}
	if unresolved {
		l.V(1).Info("Gateway domain not configured, skipping OCP Route creation")
		recordAllAdditionalIngressRouteConditions(rr, gatewayConfig.Spec.AdditionalIngresses,
			additionalIngressReasonDependencyUnavailable, "Gateway domain is not available yet")
		return nil
	}

	l.V(1).Info("Adding OCP Route templates for Gateway")

	rr.Templates = append(rr.Templates,
		odhtypes.TemplateInfo{
			FS:   gatewayResources,
			Path: ocpRouteTemplate,
		},
	)

	if err := createAdditionalIngressRoutes(ctx, rr, gatewayConfig); err != nil {
		return err
	}

	return nil
}

func createAdditionalIngressRoutes(
	ctx context.Context,
	rr *odhtypes.ReconciliationRequest,
	gatewayConfig *serviceApi.GatewayConfig,
) error {
	ingresses := gatewayConfig.Spec.AdditionalIngresses
	if len(ingresses) == 0 {
		return nil
	}

	l := logf.FromContext(ctx).WithName("createAdditionalIngressRoutes")
	for _, ingress := range ingresses {
		if additionalIngressHasGatewayConflict(gatewayConfig, ingress.Name) {
			continue
		}
		route, err := buildAdditionalIngressRoute(ingress)
		if err != nil {
			recordAdditionalIngressRouteCondition(rr, ingress.Name, metav1.ConditionUnknown,
				additionalIngressReasonNotReady, "The bridge Route could not be rendered")
			return fmt.Errorf("failed to render additional ingress Route for %q: %w", ingress.Name, err)
		}
		manageable, err := canManageGatewayResource(ctx, rr.Client, gatewayConfig, route)
		if err != nil {
			return fmt.Errorf("failed to verify additional ingress Route %q ownership: %w", route.Name, err)
		}
		if !manageable {
			l.Info("Skipping additional ingress Route because existing Route is not owned by GatewayConfig",
				"ingress", ingress.Name, "route", route.Name)
			recordAdditionalIngressRouteCondition(rr, ingress.Name, metav1.ConditionFalse,
				additionalIngressReasonOwnershipConflict,
				fmt.Sprintf("Route %q is not controlled by this GatewayConfig", route.Name))
			continue
		}
		if err := rr.AddResources(route); err != nil {
			recordAdditionalIngressRouteCondition(rr, ingress.Name, metav1.ConditionUnknown,
				additionalIngressReasonNotReady, "The bridge Route could not be added to the desired resources")
			return fmt.Errorf("failed to add additional ingress Route for %q: %w", ingress.Name, err)
		}
	}
	return nil
}

func canManageGatewayResource(
	ctx context.Context,
	cli client.Client,
	gatewayConfig *serviceApi.GatewayConfig,
	desired client.Object,
) (bool, error) {
	existing, ok := desired.DeepCopyObject().(client.Object)
	if !ok {
		return false, fmt.Errorf("cannot copy resource %T", desired)
	}
	err := cli.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if k8serr.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get existing resource %s: %w", client.ObjectKeyFromObject(desired), err)
	}

	return isOwnedByGatewayConfig(existing, gatewayConfig), nil
}

func isOwnedByGatewayConfig(obj client.Object, gatewayConfig *serviceApi.GatewayConfig) bool {
	controller := metav1.GetControllerOf(obj)
	return controller != nil && controller.UID == gatewayConfig.UID &&
		controller.Name == gatewayConfig.Name && controller.Kind == serviceApi.GatewayConfigKind
}

func buildAdditionalIngressRoute(ingress serviceApi.AdditionalIngress) (*routev1.Route, error) {
	data := map[string]any{
		"GatewayName":        ingress.Name,
		"GatewayNamespace":   GetGatewayNamespace(),
		"GatewayHostname":    ingress.Hostname,
		"GatewayServiceName": GetGatewayServiceFullName(ingress.Name),
		"StandardHTTPSPort":  StandardHTTPSPort,
		"RouteLabels":        gatewayRouteLabels(ingress.RouteLabels),
		"RouteServerTimeout": "",
	}

	content, err := gatewayResources.ReadFile(ocpRouteTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to read Route template: %w", err)
	}
	tmpl, err := gotemplate.New(ocpRouteTemplate).
		Funcs(templateutils.TextTemplateFuncMap()).
		Option("missingkey=error").
		Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse Route template: %w", err)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, data); err != nil {
		return nil, fmt.Errorf("failed to execute Route template: %w", err)
	}

	route := &routev1.Route{}
	if err := k8syaml.Unmarshal(rendered.Bytes(), route); err != nil {
		return nil, fmt.Errorf("failed to decode Route template: %w", err)
	}
	return route, nil
}

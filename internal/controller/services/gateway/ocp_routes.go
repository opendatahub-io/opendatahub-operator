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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	gotemplate "text/template"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	metadatalabels "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	templateutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/template"
)

// GatewayServiceFullName is the name of the auto-created Gateway service.
// Format: <gateway-name>-<gatewayclass-name>.
var GatewayServiceFullName = DefaultGatewayName + "-" + GatewayClassName

const (
	additionalIngressRouteNamePrefix = "gateway-additional-"
	additionalIngressRouteHashLength = 8
	serviceCAAnnotation              = "router.openshift.io/service-ca-certificate"
)

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

	if err := createAdditionalIngressRoutes(ctx, rr, gatewayConfig.Spec.AdditionalIngresses); err != nil {
		return err
	}

	return nil
}

func createAdditionalIngressRoutes(
	ctx context.Context,
	rr *odhtypes.ReconciliationRequest,
	ingresses serviceApi.AdditionalIngresses,
) error {
	if len(ingresses) == 0 {
		return nil
	}

	l := logf.FromContext(ctx).WithName("createAdditionalIngressRoutes")
	services := additionalIngressRouteServices(rr)

	gatewayService, found, err := findGatewayService(ctx, rr.Client)
	if err != nil {
		recordAllAdditionalIngressRouteConditions(rr, ingresses,
			additionalIngressReasonStatusReadFailed, "Shared Gateway Service could not be inspected")
		l.Error(err, "Failed to inspect shared Gateway Service; skipping additional ingress Routes")
		return odherrors.NewRequeueAfterError(30 * time.Second)
	}
	if !found {
		l.Info("Skipping additional ingress Routes because shared Gateway Service is unavailable")
		recordAllAdditionalIngressRouteConditions(rr, ingresses,
			additionalIngressReasonDependencyUnavailable, "Shared Gateway Service is not available yet")
		return nil
	}

	gatewayNamespace := &corev1.Namespace{}
	if err := rr.Client.Get(ctx, client.ObjectKey{Name: GetGatewayNamespace()}, gatewayNamespace); err != nil {
		if k8serr.IsNotFound(err) {
			l.Info("Skipping additional ingress Routes because Gateway namespace is unavailable",
				"namespace", GetGatewayNamespace())
			recordAllAdditionalIngressRouteConditions(rr, ingresses,
				additionalIngressReasonDependencyUnavailable, "Gateway namespace is not available yet")
			return nil
		}
		recordAllAdditionalIngressRouteConditions(rr, ingresses,
			additionalIngressReasonStatusReadFailed, "Gateway namespace could not be inspected")
		l.Error(err, "Failed to get Gateway namespace; skipping additional ingress Routes", "namespace", GetGatewayNamespace())
		return odherrors.NewRequeueAfterError(30 * time.Second)
	}

	ingressControllers := &operatorv1.IngressControllerList{}
	if err := rr.Client.List(ctx, ingressControllers,
		client.InNamespace(cluster.IngressControllerName.Namespace),
	); err != nil {
		recordAllAdditionalIngressRouteConditions(rr, ingresses,
			additionalIngressReasonStatusReadFailed, "IngressControllers could not be inspected")
		l.Error(err, "Failed to list IngressControllers; skipping additional ingress Routes")
		return odherrors.NewRequeueAfterError(30 * time.Second)
	}

	controllersByName := make(map[string]*operatorv1.IngressController, len(ingressControllers.Items))
	for i := range ingressControllers.Items {
		controller := &ingressControllers.Items[i]
		controllersByName[controller.Name] = controller
	}

	retry := false
	for _, ingress := range ingresses {
		_, found := controllersByName[ingress.IngressControllerName]
		if !found {
			l.Info("Skipping additional ingress Route because target IngressController is unavailable",
				"ingress", ingress.Name, "ingressController", ingress.IngressControllerName)
			rejectAdditionalIngressRoute(rr, ingress.Name,
				additionalIngressReasonDependencyUnavailable,
				fmt.Sprintf("Target IngressController %q was not found", ingress.IngressControllerName))
			continue
		}

		if !serviceExposesIngressPort(gatewayService, ingress.ListenerPort) {
			l.Info("Skipping additional ingress Route because shared Gateway Service does not expose listener port",
				"ingress", ingress.Name, "listenerPort", ingress.ListenerPort, "service", gatewayService.Name)
			rejectAdditionalIngressRoute(rr, ingress.Name,
				additionalIngressReasonConfigurationInvalid,
				fmt.Sprintf("Shared Gateway Service %q does not expose listener port %d", gatewayService.Name, ingress.ListenerPort))
			continue
		}

		route, err := buildAdditionalIngressRoute(ingress, gatewayService)
		if err != nil {
			recordAdditionalIngressRouteCondition(rr, ingress.Name, metav1.ConditionUnknown,
				additionalIngressReasonNotReady, "The bridge Route could not be rendered")
			return fmt.Errorf("failed to render additional ingress Route for %q: %w", ingress.Name, err)
		}
		manageable, err := canManageAdditionalIngressRoute(ctx, rr, route)
		if err != nil {
			l.Info("Skipping additional ingress Route because existing Route ownership could not be verified",
				"ingress", ingress.Name, "error", err)
			recordAdditionalIngressRouteCondition(rr, ingress.Name, metav1.ConditionUnknown,
				additionalIngressReasonStatusReadFailed, "Existing bridge Route ownership could not be verified")
			retry = true
			continue
		}
		if !manageable {
			l.Info("Skipping additional ingress Route because existing Route is not owned by GatewayConfig",
				"ingress", ingress.Name, "route", route.Name)
			rejectAdditionalIngressRoute(rr, ingress.Name,
				additionalIngressReasonOwnershipConflict,
				fmt.Sprintf("Route %q is not controlled by this GatewayConfig", route.Name))
			continue
		}
		if err := rr.AddResources(route); err != nil {
			recordAdditionalIngressRouteCondition(rr, ingress.Name, metav1.ConditionUnknown,
				additionalIngressReasonNotReady, "The bridge Route could not be added to the desired resources")
			return fmt.Errorf("failed to add additional ingress Route for %q: %w", ingress.Name, err)
		}
		services[ingress.Name] = gatewayService.Name
		recordAdditionalIngressRouteCondition(rr, ingress.Name, metav1.ConditionUnknown,
			serviceApi.AdditionalIngressReconciliationPendingReason, "Waiting for the bridge Route admission status")
	}

	if retry {
		return odherrors.NewRequeueAfterError(30 * time.Second)
	}
	return nil
}

func rejectAdditionalIngressRoute(
	rr *odhtypes.ReconciliationRequest,
	ingressName, reason, message string,
) {
	recordAdditionalIngressRouteCondition(rr, ingressName, metav1.ConditionFalse, reason, message)
}

// cleanupAdditionalIngressRoutes deletes owned bridge Routes that were removed
// from the spec or failed a confirmed configuration check. Unknown dependency
// state retains the configured Route until it can be checked safely.
func cleanupAdditionalIngressRoutes(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	if rr.SkipDeploy || cluster.GetClusterInfo().Type == cluster.ClusterTypeKubernetes {
		return nil
	}
	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return err
	}

	desiredRoutes := make(map[string]struct{})
	services := additionalIngressRouteServices(rr)
	for _, ingress := range gatewayConfig.Spec.AdditionalIngresses {
		status := additionalIngressStatusByName(gatewayConfig, ingress.Name)
		if status != nil {
			condition := conditions.FindStatusCondition(status, serviceApi.AdditionalIngressRouteAdmittedConditionType)
			if services[ingress.Name] == "" && condition != nil && condition.ObservedGeneration == gatewayConfig.Generation &&
				condition.Status == metav1.ConditionFalse {
				continue
			}
		}
		desiredRoutes[GetAdditionalIngressRouteName(ingress.Name)] = struct{}{}
	}

	return cleanupStaleAdditionalIngressRoutes(ctx, rr, desiredRoutes)
}

func findGatewayService(ctx context.Context, cli client.Client) (*corev1.Service, bool, error) {
	gateway := &gwapiv1.Gateway{}
	if err := cli.Get(ctx, client.ObjectKey{
		Name:      GetDefaultGatewayName(),
		Namespace: GetGatewayNamespace(),
	}, gateway); err != nil {
		if k8serr.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to get managed Gateway: %w", err)
	}

	services := &corev1.ServiceList{}
	if err := cli.List(ctx, services,
		client.InNamespace(GetGatewayNamespace()),
		client.MatchingLabels{metadatalabels.GatewayAPI.GatewayName: GetDefaultGatewayName()},
	); err != nil {
		return nil, false, fmt.Errorf("failed to list shared Gateway Services: %w", err)
	}

	if len(services.Items) != 1 {
		return nil, false, nil
	}

	service := &services.Items[0]
	// Istio versions emit Gateway owner references as v1beta1 or v1 and may omit the controller bit.
	// Match group, kind, name, and UID so this survives API version changes and rejects recreated Gateways.
	hasGatewayOwner := false
	for _, owner := range service.OwnerReferences {
		ownerGroupVersion, err := schema.ParseGroupVersion(owner.APIVersion)
		if err != nil || ownerGroupVersion.Group != gvk.KubernetesGateway.Group {
			continue
		}
		if owner.Kind == gvk.KubernetesGateway.Kind && owner.Name == gateway.Name && owner.UID == gateway.UID {
			hasGatewayOwner = true
			break
		}
	}
	if !hasGatewayOwner {
		return nil, false, nil
	}
	if service.Spec.ClusterIP == "" || service.Spec.ClusterIP == corev1.ClusterIPNone ||
		(service.Spec.Type != "" && service.Spec.Type != corev1.ServiceTypeClusterIP) {
		return nil, false, nil
	}

	return service, true, nil
}

func serviceExposesIngressPort(service *corev1.Service, listenerPort int32) bool {
	for _, servicePort := range service.Spec.Ports {
		if servicePort.Port == listenerPort {
			return true
		}
	}
	return false
}

func cleanupStaleAdditionalIngressRoutes(
	ctx context.Context,
	rr *odhtypes.ReconciliationRequest,
	desiredRoutes map[string]struct{},
) error {
	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return nil
	}

	routes := &routev1.RouteList{}
	if err := rr.Client.List(ctx, routes, client.InNamespace(GetGatewayNamespace())); err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("failed to list additional ingress Routes for cleanup: %w", err)
	}

	for i := range routes.Items {
		route := &routes.Items[i]
		if !route.DeletionTimestamp.IsZero() || !strings.HasPrefix(route.Name, additionalIngressRouteNamePrefix) {
			continue
		}
		if _, desired := desiredRoutes[route.Name]; desired || !isOwnedByGatewayConfig(route, gatewayConfig) {
			continue
		}
		if err := rr.Client.Delete(ctx, route); err != nil && !k8serr.IsNotFound(err) {
			return fmt.Errorf("failed to delete stale additional ingress Route %q: %w", route.Name, err)
		}
	}
	return nil
}

func canManageAdditionalIngressRoute(
	ctx context.Context,
	rr *odhtypes.ReconciliationRequest,
	desired *routev1.Route,
) (bool, error) {
	existing := &routev1.Route{}
	err := rr.Client.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if k8serr.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get existing bridge Route: %w", err)
	}

	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return false, err
	}
	return isOwnedByGatewayConfig(existing, gatewayConfig), nil
}

func isOwnedByGatewayConfig(route *routev1.Route, gatewayConfig *serviceApi.GatewayConfig) bool {
	controller := metav1.GetControllerOf(route)
	return controller != nil && controller.UID == gatewayConfig.UID &&
		controller.Name == gatewayConfig.Name && controller.Kind == serviceApi.GatewayConfigKind
}

func buildAdditionalIngressRoute(ingress serviceApi.AdditionalIngress, service *corev1.Service) (*routev1.Route, error) {
	data := map[string]any{
		"GatewayName":        GetAdditionalIngressRouteName(ingress.Name),
		"GatewayNamespace":   GetGatewayNamespace(),
		"GatewayHostname":    ingress.Hostname,
		"GatewayServiceName": service.Name,
		"StandardHTTPSPort":  ingress.ListenerPort,
		"RouteLabels":        gatewayRouteLabels(ingress.RouteLabels),
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

// GetAdditionalIngressRouteName returns the stable Route name for an additional ingress.
func GetAdditionalIngressRouteName(ingressName string) string {
	digest := sha256.Sum256([]byte(ingressName))
	hash := hex.EncodeToString(digest[:additionalIngressRouteHashLength/2])
	maxIngressNameLength := 63 - len(additionalIngressRouteNamePrefix) - len(hash) - 1
	name := ingressName
	if len(name) > maxIngressNameLength {
		name = strings.TrimRight(name[:maxIngressNameLength], "-")
	}
	return fmt.Sprintf("%s%s-%s", additionalIngressRouteNamePrefix, name, hash)
}

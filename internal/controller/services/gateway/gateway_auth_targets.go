package gateway

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	oauthv1 "github.com/openshift/api/oauth/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	actionerrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// Only targets configured in this reconciliation can be protected from GC.
func gatewayAuthState(rr *odhtypes.ReconciliationRequest) map[string]authProxyTarget {
	const key = "gateway.authentication"
	if rr.Extensions == nil {
		rr.Extensions = map[string]any{}
	}
	state, ok := rr.Extensions[key].(map[string]authProxyTarget)
	if !ok {
		state = map[string]authProxyTarget{}
		rr.Extensions[key] = state
	}
	return state
}

// Preserve existing auth resources when a target's preparation failed this pass.
// Match kind and name so unrelated resources and removed targets still use normal GC.
func gatewayAuthResourceCollectable(rr *odhtypes.ReconciliationRequest, resource unstructured.Unstructured) (bool, error) {
	for _, target := range gatewayAuthState(rr) {
		if name, found := target.resourceNames()[resource.GroupVersionKind()]; found && name == resource.GetName() {
			return false, nil
		}
	}
	return gc.DefaultObjectPredicate(rr, resource)
}

type authProxyTarget struct {
	// IngressName is empty for the default Gateway and names an additional ingress otherwise.
	IngressName         string
	GatewayName         string
	Hostname            string
	ProxyName           string
	SecretName          string
	TLSName             string
	ClientID            string
	CallbackName        string
	FilterName          string
	DestinationRuleName string
	CookieName          string
	MaxReplicas         int32
	OIDC                *serviceApi.OIDCConfig
}

func (t authProxyTarget) isDefault() bool {
	return t.IngressName == ""
}

func (t authProxyTarget) resourceNames() map[schema.GroupVersionKind]string {
	return map[schema.GroupVersionKind]string{
		gvk.Deployment:              t.ProxyName,
		gvk.Service:                 t.ProxyName,
		gvk.ServiceAccount:          t.ProxyName,
		gvk.NetworkPolicy:           t.ProxyName,
		gvk.HorizontalPodAutoscaler: t.ProxyName,
		gvk.Secret:                  t.SecretName,
		gvk.OAuthClient:             t.ClientID,
		gvk.HTTPRoute:               t.CallbackName,
		gvk.EnvoyFilter:             t.FilterName,
		gvk.DestinationRule:         t.DestinationRuleName,
		gvk.ClusterRoleBinding:      t.ProxyName + "-tokenreview",
		gvk.CertManagerCertificate:  t.TLSName,
	}
}

func checkAuthProxyDependencies(ctx context.Context, rr *odhtypes.ReconciliationRequest, config *serviceApi.GatewayConfig, target authProxyTarget) error {
	for kind, name := range target.resourceNames() {
		// Credentials have a fail-closed preparation path below. Additional ingresses
		// use OpenShift serving certificates, rather than cert-manager Certificates.
		if kind == gvk.Secret || kind == gvk.OAuthClient || kind == gvk.CertManagerCertificate {
			continue
		}
		if kind == gvk.NetworkPolicy && config.Spec.NetworkPolicy != nil && config.Spec.NetworkPolicy.Ingress != nil && !config.Spec.NetworkPolicy.Ingress.Enabled {
			continue
		}
		resource := &unstructured.Unstructured{}
		resource.SetGroupVersionKind(kind)
		resource.SetName(name)
		if kind != gvk.ClusterRoleBinding {
			resource.SetNamespace(GetGatewayNamespace())
		}
		manageable, err := canManageGatewayResource(ctx, rr.Client, config, resource)
		if err != nil {
			return fmt.Errorf("failed to check authentication resource %s/%s: %w", kind.Kind, name, err)
		}
		if !manageable {
			return fmt.Errorf("authentication resource %s/%s is not controlled by this GatewayConfig", kind.Kind, name)
		}
	}
	return nil
}

func checkAuthProxyCredentials(
	ctx context.Context,
	rr *odhtypes.ReconciliationRequest,
	config *serviceApi.GatewayConfig,
	target authProxyTarget,
	mode cluster.AuthenticationMode,
) error {
	if target.isDefault() {
		return nil // Preserve the default Gateway's ownership behavior.
	}
	credentials := []client.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      target.SecretName,
				Namespace: GetGatewayNamespace(),
			},
		},
	}
	if mode == cluster.AuthModeIntegratedOAuth {
		credentials = append(credentials, &oauthv1.OAuthClient{
			ObjectMeta: metav1.ObjectMeta{Name: target.ClientID},
		})
	}
	for _, resource := range credentials {
		manageable, err := canManageGatewayResource(ctx, rr.Client, config, resource)
		if err != nil {
			return fmt.Errorf("failed to check authentication credential %s: %w", client.ObjectKeyFromObject(resource), err)
		}
		if !manageable {
			return fmt.Errorf("generated authentication credential %s is not controlled by this GatewayConfig", client.ObjectKeyFromObject(resource))
		}
	}
	return nil
}

func defaultAuthProxyTarget(hostname string, config *serviceApi.GatewayConfig) authProxyTarget {
	return authProxyTarget{
		GatewayName:         GetDefaultGatewayName(),
		Hostname:            hostname,
		ProxyName:           KubeAuthProxyName,
		SecretName:          KubeAuthProxySecretsName,
		TLSName:             KubeAuthProxyTLSName,
		ClientID:            AuthClientID,
		CallbackName:        OAuthCallbackRouteName,
		FilterName:          AuthnFilterName,
		DestinationRuleName: DestinationRuleName,
		CookieName:          AuthProxyCookieName,
		MaxReplicas:         getGatewayAuthProxyMaxReplicas(config),
		OIDC:                config.Spec.OIDC,
	}
}

func additionalAuthProxyTarget(ingress serviceApi.AdditionalIngress, config *serviceApi.GatewayConfig) authProxyTarget {
	name := KubeAuthProxyName + "-" + ingress.Name
	target := authProxyTarget{
		IngressName:         ingress.Name,
		GatewayName:         ingress.Name,
		Hostname:            ingress.Hostname,
		ProxyName:           name,
		SecretName:          ingress.Name + "-auth-creds",
		TLSName:             ingress.Name + "-auth-tls",
		ClientID:            AuthClientID + "-" + ingress.Name,
		CallbackName:        OAuthCallbackRouteName + "-" + ingress.Name,
		FilterName:          additionalGatewayEnvoyFilterName(ingress.Name),
		DestinationRuleName: ingress.Name + "-auth-destination",
		CookieName:          AuthProxyCookieName + "_" + strings.ReplaceAll(ingress.Name, "-", "_"),
		MaxReplicas:         10,
	}
	if ingress.Auth.MaxReplicas != nil {
		target.MaxReplicas = *ingress.Auth.MaxReplicas
	}
	if ingress.Auth.OIDC != nil && config.Spec.OIDC != nil {
		target.OIDC = config.Spec.OIDC.DeepCopy()
		target.OIDC.ClientID = ingress.Auth.OIDC.ClientID
		target.OIDC.ClientSecretRef = ingress.Auth.OIDC.ClientSecretRef
		target.OIDC.SecretNamespace = ingress.Auth.OIDC.SecretNamespace
	}
	return target
}

func authSecretNamespace(namespace string) string {
	if namespace == "" {
		return GetGatewayNamespace()
	}
	return namespace
}

func recordAuthFailure(config *serviceApi.GatewayConfig, name, reason, message string) {
	if status := additionalIngressStatusByName(config, name); status != nil {
		setAdditionalIngressCondition(status, config.Generation, serviceApi.AdditionalIngressAuthenticationReadyConditionType,
			metav1.ConditionFalse, reason, message)
	}
}

func authTargetTemplateData(base map[string]any, target authProxyTarget, authConfigHash string) map[string]any {
	// Keep shared provider settings, overriding target-specific values on a copy.
	// Rendering an additional ingress must not mutate the default Gateway's data.
	data := maps.Clone(base)
	// Preserve the existing wildcard TLS policy for default Gateway compatibility.
	// Additional ingresses scope their policies to their own auth proxy Services.
	data["DestinationRuleHost"] = "*"
	if !target.isDefault() {
		data["DestinationRuleHost"] = target.ProxyName + "." + GetGatewayNamespace() + ".svc.cluster.local"
	}
	data["GatewayName"], data["GatewayHostname"] = target.GatewayName, target.Hostname
	data["KubeAuthProxyServiceName"], data["KubeAuthProxySecretsName"], data["KubeAuthProxyTLSName"] = target.ProxyName, target.SecretName, target.TLSName
	data["OAuthCallbackRouteName"], data["DestinationRuleName"] = target.CallbackName, target.DestinationRuleName
	data["AuthProxyCookieName"], data["AuthProxyMaxReplicas"] = target.CookieName, target.MaxReplicas
	data["RedirectURL"] = "https://" + target.Hostname + OAuthCallbackPath
	data["GatewayFilters"] = []gatewayEnvoyFilterTarget{{
		Name:        target.FilterName,
		GatewayName: target.GatewayName,
	}}
	data["AuthConfigHash"] = authConfigHash
	return data
}

func prepareAuthProxies(
	ctx context.Context,
	rr *odhtypes.ReconciliationRequest,
	config *serviceApi.GatewayConfig,
	mode cluster.AuthenticationMode,
	deploymentTemplate string,
	targets []authProxyTarget,
) error {
	state := gatewayAuthState(rr)
	base, err := getTemplateData(ctx, rr)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if !target.isDefault() && additionalIngressHasGatewayConflict(config, target.IngressName) {
			continue
		}
		if !target.isDefault() {
			if err := checkAuthProxyDependencies(ctx, rr, config, target); err != nil {
				recordAuthFailure(config, target.IngressName, additionalIngressReasonOwnershipConflict, err.Error())
				return err
			}
		} else if mode != cluster.AuthModeNone && (mode != cluster.AuthModeOIDC || target.OIDC != nil) {
			rr.Conditions.MarkUnknown(ReadyConditionType)
		}
		// Check credentials before prepareAuthProxy writes its Secret immediately.
		var reason, message, authConfigHash string
		if err := checkAuthProxyCredentials(ctx, rr, config, target, mode); err != nil {
			reason, message = additionalIngressReasonOwnershipConflict, err.Error()
		}
		sub := *rr
		sub.Templates, sub.Resources = nil, nil
		if message == "" && mode != cluster.AuthModeNone {
			var err error
			authConfigHash, err = prepareAuthProxy(ctx, &sub, config, target, mode, deploymentTemplate)
			if err != nil {
				reason = additionalIngressReasonAuthenticationUnavailable
				message = "Authentication credentials could not be reconciled for " + target.SecretName + ": " + err.Error()
				if mode == cluster.AuthModeOIDC && target.OIDC == nil {
					reason = additionalIngressReasonInvalidAuthenticationConfig
				}
				if target.isDefault() {
					if ready := rr.Conditions.GetCondition(ReadyConditionType); ready == nil || ready.Status != metav1.ConditionFalse {
						markGatewayNotReady(rr, message)
					}
					if len(targets) == 1 {
						return err
					}
				}
			}
		}
		if message != "" {
			// Keep existing resources; render only policies when preparation fails.
			sub.Resources, sub.Templates = nil, nil
		}
		if err := createEnvoyFilter(ctx, &sub); err != nil {
			return err
		}
		// The default Gateway keeps its NetworkPolicy even when preparation fails.
		if message == "" || target.isDefault() {
			if err := createNetworkPolicy(ctx, &sub); err != nil {
				return err
			}
		}
		if status := additionalIngressStatusByName(config, target.IngressName); message == "" && status != nil {
			setAdditionalIngressCondition(status, config.Generation, serviceApi.AdditionalIngressAuthenticationReadyConditionType, metav1.ConditionUnknown,
				serviceApi.AdditionalIngressReconciliationPendingReason, "Authentication resources are being deployed")
		}
		data := authTargetTemplateData(base, target, authConfigHash)
		err := template.NewAction(template.WithData(data),
			template.WithNamespaceFn(func(context.Context, *odhtypes.ReconciliationRequest) (string, error) {
				return GetGatewayNamespace(), nil
			}))(ctx, &sub)
		if err != nil {
			return fmt.Errorf("failed to render authentication resources for %s: %w", target.GatewayName, err)
		}
		rr.Resources = append(rr.Resources, sub.Resources...)
		if message != "" {
			state[target.GatewayName] = target
			recordAuthFailure(config, target.IngressName, reason, message)
		}
	}
	if len(state) != 0 {
		return actionerrors.NewRequeueAfterError(30 * time.Second)
	}
	return nil
}

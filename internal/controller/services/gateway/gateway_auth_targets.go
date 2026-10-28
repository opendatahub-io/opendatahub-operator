package gateway

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	actionerrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// Results belong to this reconciliation; persisted conditions are status output only.
type authProxyResult struct {
	Target    authProxyTarget
	Condition common.Condition
}

func (r authProxyResult) failedPreparation() bool {
	return r.Condition.Status == metav1.ConditionFalse &&
		r.Condition.Reason == additionalIngressReasonAuthenticationUnavailable
}

func gatewayAuthState(rr *odhtypes.ReconciliationRequest) map[string]authProxyResult {
	const key = "gateway.authentication"
	if rr.Extensions == nil {
		rr.Extensions = map[string]any{}
	}
	state, ok := rr.Extensions[key].(map[string]authProxyResult)
	if !ok {
		state = map[string]authProxyResult{}
		rr.Extensions[key] = state
	}
	return state
}

// Preserve the existing ingress stack when authentication preparation failed this pass.
// Match kind and name so unrelated resources and removed targets still use normal GC.
func gatewayAuthResourceCollectable(rr *odhtypes.ReconciliationRequest, resource unstructured.Unstructured) (bool, error) {
	for _, result := range gatewayAuthState(rr) {
		if !result.failedPreparation() {
			continue
		}
		if name, found := result.Target.gcResourceNames()[resource.GroupVersionKind()]; found && name == resource.GetName() {
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

func (t authProxyTarget) gcResourceNames() map[schema.GroupVersionKind]string {
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

func recordAuthResult(rr *odhtypes.ReconciliationRequest, target authProxyTarget, state metav1.ConditionStatus, reason, message string) {
	gatewayAuthState(rr)[target.GatewayName] = authProxyResult{
		Target: target,
		Condition: common.Condition{
			Type:               serviceApi.AdditionalIngressAuthenticationReadyConditionType,
			Status:             state,
			ObservedGeneration: rr.Instance.GetGeneration(),
			Reason:             reason,
			Message:            message,
		},
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
	if providerCA, _ := base["ProviderCA"].([]byte); len(providerCA) > 0 {
		authConfigHash = extendAuthConfigHash(authConfigHash, providerCA)
	}
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
		if !target.isDefault() {
			if mode == cluster.AuthModeNone {
				recordAuthResult(rr, target, metav1.ConditionFalse, additionalIngressReasonAuthenticationBlocked,
					"Authentication is unavailable; traffic is rejected until an authentication provider is configured")
			}
			if additionalIngressHasGatewayConflict(config, target.IngressName) {
				continue
			}
		} else if mode != cluster.AuthModeNone && (mode != cluster.AuthModeOIDC || target.OIDC != nil) {
			rr.Conditions.MarkUnknown(ReadyConditionType)
		}
		var authConfigHash, reason, message string
		sub := *rr
		sub.Templates, sub.Resources = nil, nil
		if mode != cluster.AuthModeNone {
			var err error
			authConfigHash, err = prepareAuthProxy(ctx, &sub, config, target, mode, deploymentTemplate)
			if err != nil {
				var ownershipErr *controllerutil.AlreadyOwnedError
				if !errors.Is(err, errInvalidAuthenticationConfig) && !errors.As(err, &ownershipErr) {
					recordAuthResult(rr, target, metav1.ConditionUnknown, additionalIngressReasonStatusReadFailed, err.Error())
					return err // Stop before deployment and GC; retry through the normal error path.
				}
				// Keep existing resources; render only policies when preparation fails.
				sub.Resources, sub.Templates = nil, nil
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
			} else if !target.isDefault() {
				recordAuthResult(rr, target, metav1.ConditionUnknown, serviceApi.AdditionalIngressReconciliationPendingReason,
					"Authentication resources are being deployed")
			}
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
		data := authTargetTemplateData(base, target, authConfigHash)
		err = template.NewAction(template.WithData(data),
			template.WithNamespaceFn(func(context.Context, *odhtypes.ReconciliationRequest) (string, error) {
				return GetGatewayNamespace(), nil
			}))(ctx, &sub)
		if err != nil {
			return fmt.Errorf("failed to render authentication resources for %s: %w", target.GatewayName, err)
		}
		rr.Resources = append(rr.Resources, sub.Resources...)
		if message != "" {
			recordAuthResult(rr, target, metav1.ConditionFalse, reason, message)
		}
	}
	for _, result := range state {
		if result.failedPreparation() {
			return actionerrors.NewRequeueAfterError(30 * time.Second)
		}
	}
	return nil
}

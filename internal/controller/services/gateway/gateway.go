package gateway

import (
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
)

const (
	ServiceName = serviceApi.GatewayServiceName
)

type AuthMode string

const (
	AuthModeIntegratedOAuth AuthMode = "IntegratedOAuth"
	AuthModeOIDC            AuthMode = "OIDC"
	AuthModeNone            AuthMode = "None"
	AuthClientID                     = "odh"
)

const (
	gatewayNamespace = "openshift-ingress"
	gatewayName      = "data-science-gateway"
	gatewayClassName = "data-science-gateway-class"

	kubeAuthProxyName        = "kube-auth-proxy"
	kubeAuthProxyCredsSecret = "kube-auth-proxy-creds" //nolint:gosec
	kubeAuthProxyTLSSecret   = "kube-auth-proxy-tls"   //nolint:gosec
	DestinationRuleName      = "data-science-tls-rule"
)

const (
	KubeAuthProxyDeploymentTemplate = "resources/kube-auth-proxy-deployment.tmpl.yaml"
	KubeAuthProxyServiceTemplate    = "resources/kube-auth-proxy-svc.tmpl.yaml"
	KubeAuthProxyHTTPRouteTemplate  = "resources/kube-auth-proxy-httproute.tmpl.yaml"
	EnvoyFilterTemplate             = "resources/envoyfilter-authn.tmpl.yaml"
	DestinationRuleTemplate         = "resources/destinationrule-tls.tmpl.yaml"
)

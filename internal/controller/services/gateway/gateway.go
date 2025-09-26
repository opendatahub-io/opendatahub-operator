package gateway

import (
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
)

const (
	ServiceName = serviceApi.GatewayServiceName
)

const (
	AuthClientID = "data-science-gateway"
)

const (
	// for gateway part.
	GatewayNamespace            = "openshift-ingress"
	GatewayName                 = "data-science-gateway"
	GatewayClassName            = "data-science-gateway-class"
	DefaultGatewayTLSSecretName = "data-science-gateway-tls"
	// for auth part.
	KubeAuthProxyName        = "kube-auth-proxy"
	KubeAuthProxyCredsSecret = "kube-auth-proxy-creds" //nolint:gosec
	KubeAuthProxyTLSSecret   = "kube-auth-proxy-tls"   //nolint:gosec
	OAuthCallbackRouteName   = "oauth-callback-route"
	AuthnFilterName          = "authn-filter"
	DestinationRuleName      = "data-science-tls-rule"
)

const (
	kubeAuthProxyDeploymentTemplate = "resources/kube-auth-proxy-deployment.tmpl.yaml"
	kubeAuthProxyServiceTemplate    = "resources/kube-auth-proxy-svc.tmpl.yaml"
	kubeAuthProxyHTTPRouteTemplate  = "resources/kube-auth-proxy-httproute.tmpl.yaml"
	envoyFilterTemplate             = "resources/envoyfilter-authn.tmpl.yaml"
	destinationRuleTemplate         = "resources/destinationrule-tls.tmpl.yaml"
)

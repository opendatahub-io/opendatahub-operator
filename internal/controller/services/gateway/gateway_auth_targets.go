package gateway

import serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"

const authConfigHashKeyPrefix = "gateway.opendatahub.io/auth-config-hash"

type authProxyTarget struct {
	Hostname   string
	ProxyName  string
	SecretName string
	TLSName    string
	ClientID   string
	OIDC       *serviceApi.OIDCConfig
}

func (t authProxyTarget) extensionsKey() string {
	return authConfigHashKeyPrefix + "/" + t.SecretName
}

func defaultAuthProxyTarget(hostname string, config *serviceApi.GatewayConfig) authProxyTarget {
	return authProxyTarget{
		Hostname:   hostname,
		ProxyName:  KubeAuthProxyName,
		SecretName: KubeAuthProxySecretsName,
		TLSName:    KubeAuthProxyTLSName,
		ClientID:   AuthClientID,
		OIDC:       config.Spec.OIDC,
	}
}

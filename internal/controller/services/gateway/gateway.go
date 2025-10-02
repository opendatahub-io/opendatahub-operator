package gateway

import (
	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	sr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/registry"
)

const (
	kubeAuthProxyDeploymentOidcTemplate  = "resources/kube-auth-proxy-oidc-deployment.tmpl.yaml"
	kubeAuthProxyDeploymentOauthTemplate = "resources/kube-auth-proxy-oauth-deployment.tmpl.yaml"
	kubeAuthProxyServiceTemplate         = "resources/kube-auth-proxy-svc.tmpl.yaml"
	kubeAuthProxyHTTPRouteTemplate       = "resources/kube-auth-proxy-httproute.tmpl.yaml"
	envoyFilterTemplate                  = "resources/envoyfilter-authn.tmpl.yaml"
	destinationRuleTemplate              = "resources/kube-auth-proxy-destinationrule-tls.tmpl.yaml"
	componentHttpRouteTemplatePath       = "resources/httproutes"
)

type ServiceHandler struct {
}

//nolint:gochecknoinits
func init() {
	sr.Add(&ServiceHandler{})
}

func (h *ServiceHandler) Init(platform common.Platform) error {
	return nil
}

func (h *ServiceHandler) GetName() string {
	return ServiceName
}

func (h *ServiceHandler) GetManagementState(platform common.Platform, _ *dsciv2.DSCInitialization) operatorv1.ManagementState {
	return operatorv1.Managed
}

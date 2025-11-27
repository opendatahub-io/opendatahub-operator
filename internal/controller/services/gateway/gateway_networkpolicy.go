package gateway

import (
	"context"
	"fmt"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	// NetworkPolicyTemplate is the path to the NetworkPolicy YAML template file.
	NetworkPolicyTemplate = "resources/kube-auth-proxy-networkpolicy.yaml"
)

// createNetworkPolicy creates a NetworkPolicy for kube-auth-proxy
// by adding the template to be rendered with values from constants.
// Ingress is enabled by default. Set Ingress.Enabled=false to disable.
func createNetworkPolicy(ctx context.Context, rr *odhtypes.ReconciliationRequest, config *serviceApi.NetworkPolicyConfig) error {
	l := logf.FromContext(ctx).WithName("createNetworkPolicy")

	// Ingress is enabled by default (when config is nil or Ingress is nil)
	// If Ingress is specified, use the explicit Enabled value
	ingressEnabled := true
	if config != nil && config.Ingress != nil {
		ingressEnabled = config.Ingress.Enabled
	}

	// Only skip NetworkPolicy creation if ingress is explicitly disabled
	if !ingressEnabled {
		l.V(1).Info("Ingress disabled, skipping NetworkPolicy creation")
		return nil
	}

	l.V(1).Info("Creating NetworkPolicy for kube-auth-proxy", "ingress", ingressEnabled)

	// Validate template file exists before adding it
	if !common.FileExists(gatewayResources, NetworkPolicyTemplate) {
		return fmt.Errorf("NetworkPolicy template file not found: %s", NetworkPolicyTemplate)
	}

	// Add template to be rendered by template.NewAction() in the reconciler chain
	// Template will be rendered with data from getNetworkPolicyTemplateData()
	rr.Templates = append(rr.Templates, odhtypes.TemplateInfo{
		FS:   gatewayResources,
		Path: NetworkPolicyTemplate,
	})

	l.V(1).Info("NetworkPolicy template added (ingress only)")
	return nil
}

// getNetworkPolicyTemplateData provides template data from constants.
func getNetworkPolicyTemplateData(_ context.Context, _ *odhtypes.ReconciliationRequest) (map[string]any, error) {
	return map[string]any{
		"NetworkPolicyName":      KubeAuthProxyName,
		"NetworkPolicyNamespace": GatewayNamespace,
		"GatewayName":            DefaultGatewayName,
		"HTTPSPort":              AuthProxyHTTPSPort,
		"MetricsPort":            AuthProxyMetricsPort,
	}, nil
}

package gateway

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// Use the default Gateway's auth checks for every target, without changing its platform behavior.
func checkGatewayAuthentication(ctx context.Context, cli client.Client, target authProxyTarget) (string, error) {
	if cluster.GetClusterInfo().Type != cluster.ClusterTypeKubernetes || target.OIDC == nil {
		return "", nil
	}
	message, err := checkCertManagerCertificate(ctx, cli, target.TLSName)
	if err != nil || message != "" {
		return message, err
	}
	return checkKubeAuthProxyDeployment(ctx, cli, target.ProxyName)
}

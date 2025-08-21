package gateway

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
)

const (
	ServiceName = serviceApi.GatewayServiceName
)

// getGatewayDomain returns the domain for the gateway based on the cluster configuration
func getGatewayDomain(ctx context.Context, cli client.Client) (string, error) {
	// For now, we'll derive the domain from the cluster's default ingress domain
	// This could be made configurable in the future

	// workaround ...
	//return "odh.apps-crc.testing", nil
	return "gateway.apps-crc.testing", nil

	/*
		// Try to get the cluster platform type
		platform, err := cluster.GetPlatform(ctx, cli)
		if err != nil {
			return "gateway.local", nil
		}

		// Check if this is OpenShift-based platform
		if platform == cluster.SelfManagedRhoai || platform == cluster.ManagedRhoai {
			// On OpenShift, use the default apps domain
			return "apps.cluster.local", nil
		}

		// For non-OpenShift clusters, use a default domain
		return "gateway.local", nil
	*/
}

/*
// generateGatewayName generates a consistent gateway name
func generateGatewayName(namespace string) string {
	return fmt.Sprintf("odh-gateway-%s", namespace)
}

// generateGatewayClassName returns the appropriate gateway class name
func generateGatewayClassName() string {
	// Use the default gateway class - this could be made configurable
	return "openshift-default"
}
*/

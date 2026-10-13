package gatewayconfig

import (
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
)

const (
	GatewayUnsupportedCertTypeOnKubernetesMessage    = "certificate.type OpenshiftDefaultIngress is not supported on Kubernetes clusters; use SelfSigned or Provided"
	GatewayUnsupportedIngressModeOnKubernetesMessage = "ingressMode OcpRoute is not supported on Kubernetes clusters; use LoadBalancer"
)

// KubernetesValidationErrors returns messages for GatewayConfig values that
// require OpenShift and are unsupported on vanilla Kubernetes.
func KubernetesValidationErrors(gatewayConfig *serviceApi.GatewayConfig) []string {
	if gatewayConfig == nil {
		return nil
	}

	var messages []string
	if gatewayConfig.Spec.Certificate != nil && gatewayConfig.Spec.Certificate.Type == infrav1.OpenshiftDefaultIngress {
		messages = append(messages, GatewayUnsupportedCertTypeOnKubernetesMessage)
	}
	if gatewayConfig.Spec.IngressMode == serviceApi.IngressModeOcpRoute {
		messages = append(messages, GatewayUnsupportedIngressModeOnKubernetesMessage)
	}
	return messages
}

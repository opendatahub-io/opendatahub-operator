package gatewayconfig

import (
	"fmt"
	"sort"
	"strings"

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
	if len(gatewayConfig.Spec.AdditionalIngresses) > 0 && gatewayConfig.Spec.IngressMode == "" {
		messages = append(messages, "additional ingresses require an explicit ingress mode on Kubernetes; received ingressMode=\"\"")
	}
	return messages
}

// AdditionalIngressValidationErrors returns admission errors for constraints
// that depend on multiple GatewayConfig fields or multiple ingress entries.
func AdditionalIngressValidationErrors(gatewayConfig *serviceApi.GatewayConfig, defaultSecretNamespace string) []string {
	if gatewayConfig == nil || len(gatewayConfig.Spec.AdditionalIngresses) == 0 {
		return nil
	}

	var messages []string
	if err := gatewayConfig.Spec.ValidateAdditionalIngresses(); err != nil {
		messages = append(messages, err.Error())
	}

	additionalIngresses := gatewayConfig.Spec.AdditionalIngresses
	seenFields := make(map[string]string, len(additionalIngresses)*3)
	seenClientIDs := make(map[string]string, len(additionalIngresses)+1)
	seenSecretRefs := make(map[string]string, len(additionalIngresses)+1)

	if gatewayConfig.Spec.OIDC != nil {
		seenClientIDs[gatewayConfig.Spec.OIDC.ClientID] = "spec.oidc"
		seenSecretRefs[oidcSecretReferenceKey(gatewayConfig.Spec.OIDC.ClientSecretRef.Name,
			gatewayConfig.Spec.OIDC.SecretNamespace, defaultSecretNamespace)] = "spec.oidc"
	}

	for index, ingress := range additionalIngresses {
		path := fmt.Sprintf("spec.additionalIngresses[%d]", index)
		messages = appendDuplicate(messages, seenFields, "name:"+ingress.Name, path+".name")
		messages = appendDuplicate(messages, seenFields, "hostname:"+ingress.Hostname, path+".hostname")
		messages = appendDuplicate(messages, seenFields, "routeLabels:"+canonicalLabelsKey(ingress.RouteLabels), path+".routeLabels")

		if ingress.Auth.OIDC == nil {
			if gatewayConfig.Spec.OIDC != nil {
				messages = append(messages, path+".auth.oidc is required when spec.oidc is configured")
			}
			continue
		}
		if gatewayConfig.Spec.OIDC == nil {
			messages = append(messages, path+".auth.oidc requires spec.oidc")
		}
		messages = append(messages, validateOIDCIdentity(ingress, index, defaultSecretNamespace, seenClientIDs, seenSecretRefs)...)
	}

	return messages
}

func validateOIDCIdentity(
	ingress serviceApi.AdditionalIngress,
	index int,
	defaultSecretNamespace string,
	seenClientIDs, seenSecretRefs map[string]string,
) []string {
	if ingress.Auth.OIDC == nil {
		return nil
	}

	var messages []string
	path := fmt.Sprintf("spec.additionalIngresses[%d].auth.oidc", index)
	messages = appendDuplicate(messages, seenClientIDs, ingress.Auth.OIDC.ClientID, path+".clientID")
	secretRefKey := oidcSecretReferenceKey(ingress.Auth.OIDC.ClientSecretRef.Name,
		ingress.Auth.OIDC.SecretNamespace, defaultSecretNamespace)
	messages = appendDuplicate(messages, seenSecretRefs, secretRefKey, path+".clientSecretRef")
	return messages
}

func appendDuplicate(messages []string, seen map[string]string, key, path string) []string {
	previous, found := seen[key]
	if !found {
		seen[key] = path
		return messages
	}
	return append(messages, fmt.Sprintf("%s duplicates %s", path, previous))
}

func oidcSecretReferenceKey(name, namespace, defaultNamespace string) string {
	if namespace == "" {
		namespace = defaultNamespace
	}
	return namespace + "/" + name
}

func canonicalLabelsKey(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(labels[key])
		builder.WriteByte('\x00')
	}
	return builder.String()
}

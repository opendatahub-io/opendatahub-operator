//nolint:testpackage
package gatewayconfig

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"

	. "github.com/onsi/gomega"
)

func TestAdditionalIngressValidationErrors(t *testing.T) {
	validIngress := func(name, hostname, labelValue string) serviceApi.AdditionalIngress {
		return serviceApi.AdditionalIngress{
			Name:                  name,
			Hostname:              hostname,
			IngressControllerName: "shard-" + name,
			RouteLabels:           map[string]string{"example.com/ingress": labelValue},
		}
	}
	validOIDC := func(clientID, secretName, secretKey, namespace string) *serviceApi.AdditionalIngressOIDCConfig {
		return &serviceApi.AdditionalIngressOIDCConfig{
			ClientID: clientID,
			ClientSecretRef: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  secretKey,
			},
			SecretNamespace: namespace,
		}
	}

	tests := []struct {
		name    string
		config  *serviceApi.GatewayConfig
		wantErr string
	}{
		{
			name: "nil config",
		},
		{
			name: "empty additional ingress list",
			config: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{},
			},
		},
		{
			name: "duplicate hostname",
			config: &serviceApi.GatewayConfig{Spec: serviceApi.GatewayConfigSpec{
				IngressMode:         serviceApi.IngressModeOcpRoute,
				AdditionalIngresses: serviceApi.AdditionalIngresses{validIngress("alpha", "shared.example.com", "alpha"), validIngress("beta", "shared.example.com", "beta")},
			}},
			wantErr: "hostname",
		},
		{
			name: "mixed authentication modes",
			config: &serviceApi.GatewayConfig{Spec: serviceApi.GatewayConfigSpec{
				IngressMode: serviceApi.IngressModeOcpRoute,
				OIDC: &serviceApi.OIDCConfig{
					ClientID: "default-client",
					ClientSecretRef: corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "default-secret"},
						Key:                  "clientSecret",
					},
				},
				AdditionalIngresses: serviceApi.AdditionalIngresses{validIngress("alpha", "alpha.example.com", "alpha")},
			}},
			wantErr: "auth.oidc is required",
		},
		{
			name: "duplicate client ID and effective secret reference",
			config: &serviceApi.GatewayConfig{Spec: serviceApi.GatewayConfigSpec{
				IngressMode: serviceApi.IngressModeOcpRoute,
				OIDC: &serviceApi.OIDCConfig{
					ClientID: "default-client",
					ClientSecretRef: corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "shared-secret"},
						Key:                  "default-key",
					},
					SecretNamespace: "openshift-ingress",
				},
				AdditionalIngresses: func() serviceApi.AdditionalIngresses {
					alpha := validIngress("alpha", "alpha.example.com", "alpha")
					alpha.Auth.OIDC = validOIDC("default-client", "shared-secret", "additional-key", "")
					return serviceApi.AdditionalIngresses{alpha}
				}(),
			}},
			wantErr: "clientID",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			messages := AdditionalIngressValidationErrors(test.config, "openshift-ingress")
			if test.wantErr == "" {
				g.Expect(messages).To(BeEmpty())
				return
			}
			g.Expect(messages).To(ContainElement(ContainSubstring(test.wantErr)))
		})
	}
}

func TestAppendDuplicate(t *testing.T) {
	g := NewWithT(t)
	seen := map[string]string{}

	messages := appendDuplicate(nil, seen, "name:alpha", "spec.additionalIngresses[0].name")
	g.Expect(messages).To(BeEmpty())
	messages = appendDuplicate(messages, seen, "name:alpha", "spec.additionalIngresses[1].name")
	g.Expect(messages).To(ConsistOf("spec.additionalIngresses[1].name duplicates spec.additionalIngresses[0].name"))
}

func TestOIDCSecretReferenceKey(t *testing.T) {
	g := NewWithT(t)
	g.Expect(oidcSecretReferenceKey("client", "", "openshift-ingress")).To(Equal("openshift-ingress/client"))
	g.Expect(oidcSecretReferenceKey("client", "custom", "openshift-ingress")).To(Equal("custom/client"))
	g.Expect(oidcSecretReferenceKey("client", "custom", "other")).To(Equal("custom/client"))
}

func TestCanonicalLabelsKeyIsDeterministic(t *testing.T) {
	g := NewWithT(t)
	first := canonicalLabelsKey(map[string]string{"b.example/key": "b", "a.example/key": "a"})
	second := canonicalLabelsKey(map[string]string{"a.example/key": "a", "b.example/key": "b"})
	g.Expect(first).To(Equal(second))
}

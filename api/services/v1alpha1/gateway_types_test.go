package v1alpha1

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

func TestAdditionalIngressAuthSerializationAndDeepCopy(t *testing.T) {
	g := NewWithT(t)
	maximum := int32(4)
	defaultMaximum := int32(6)
	config := &GatewayConfig{Spec: GatewayConfigSpec{
		AuthProxyMaxReplicas: &defaultMaximum,
		AdditionalIngresses: AdditionalIngresses{{
			Name: "alpha",
			Auth: AdditionalIngressAuth{
				MaxReplicas: &maximum,
				OIDC: &AdditionalIngressOIDCConfig{
					ClientID: "alpha-client", SecretNamespace: "identity",
					ClientSecretRef: corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "alpha-oidc"},
						Key:                  "clientSecret",
					},
				},
			},
		}},
	}}
	encoded, err := json.Marshal(config)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(encoded)).To(ContainSubstring(`"auth":{"oidc":{"clientID":"alpha-client"`))
	g.Expect(string(encoded)).NotTo(ContainSubstring("issuerURL"))
	var decoded GatewayConfig
	g.Expect(json.Unmarshal(encoded, &decoded)).To(Succeed())
	g.Expect(decoded.Spec).To(Equal(config.Spec))

	copied := config.DeepCopy()
	*copied.Spec.AuthProxyMaxReplicas = 2
	*copied.Spec.AdditionalIngresses[0].Auth.MaxReplicas = 8
	copied.Spec.AdditionalIngresses[0].Auth.OIDC.ClientID = "other-client"
	copied.Spec.AdditionalIngresses[0].Auth.OIDC.ClientSecretRef.Name = "other-secret"
	copied.Spec.AdditionalIngresses[0].Auth.OIDC.ClientSecretRef.Key = "other-key"
	g.Expect(*config.Spec.AuthProxyMaxReplicas).To(Equal(int32(6)))
	g.Expect(*config.Spec.AdditionalIngresses[0].Auth.MaxReplicas).To(Equal(int32(4)))
	g.Expect(config.Spec.AdditionalIngresses[0].Auth.OIDC.ClientID).To(Equal("alpha-client"))
	g.Expect(config.Spec.AdditionalIngresses[0].Auth.OIDC.ClientSecretRef.Name).To(Equal("alpha-oidc"))
	g.Expect(config.Spec.AdditionalIngresses[0].Auth.OIDC.ClientSecretRef.Key).To(Equal("clientSecret"))
}

func TestAdditionalIngressesValidate(t *testing.T) {
	valid := func(name, hostname string) AdditionalIngress {
		return AdditionalIngress{
			Name:                  name,
			Hostname:              hostname,
			IngressControllerName: "shard-a",
			RouteLabels:           map[string]string{"example.com/ingress": name},
		}
	}

	tests := []struct {
		name      string
		ingresses AdditionalIngresses
		mode      IngressMode
		wantErr   string
	}{
		{
			name:      "valid entries",
			ingresses: AdditionalIngresses{valid("alpha", "alpha.example.com"), valid("beta", "beta.example.com")},
			mode:      IngressModeOcpRoute,
		},
		{
			name:      "additional ingress requires OcpRoute",
			ingresses: AdditionalIngresses{valid("alpha", "alpha.example.com")},
			mode:      IngressModeLoadBalancer,
			wantErr:   "require OcpRoute",
		},
		{
			name:      "invalid hostname",
			ingresses: AdditionalIngresses{valid("alpha", "not a hostname")},
			mode:      IngressModeOcpRoute,
			wantErr:   "invalid hostname",
		},
		{
			name: "invalid controller name",
			ingresses: AdditionalIngresses{{
				Name: "alpha", Hostname: "alpha.example.com", IngressControllerName: "bad_name", RouteLabels: map[string]string{"example.com/ingress": "alpha"},
			}},
			mode:    IngressModeOcpRoute,
			wantErr: "invalid IngressController name",
		},
		{
			name: "missing route labels",
			ingresses: AdditionalIngresses{{
				Name: "alpha", Hostname: "alpha.example.com", IngressControllerName: "shard-a",
			}},
			mode:    IngressModeOcpRoute,
			wantErr: "must define route labels",
		},
		{
			name: "invalid route label",
			ingresses: AdditionalIngresses{{
				Name: "alpha", Hostname: "alpha.example.com", IngressControllerName: "shard-a", RouteLabels: map[string]string{"bad key": "alpha"},
			}},
			mode:    IngressModeOcpRoute,
			wantErr: "invalid route label key",
		},
		{
			name: "invalid route label value",
			ingresses: AdditionalIngresses{{
				Name: "alpha", Hostname: "alpha.example.com", IngressControllerName: "shard-a", RouteLabels: map[string]string{"example.com/ingress": "bad value"},
			}},
			mode:    IngressModeOcpRoute,
			wantErr: "invalid route label value",
		},
		{
			name: "reserved route label",
			ingresses: AdditionalIngresses{{
				Name: "alpha", Hostname: "alpha.example.com", IngressControllerName: "shard-a", RouteLabels: map[string]string{"app.kubernetes.io/part-of": "custom"},
			}},
			mode:    IngressModeOcpRoute,
			wantErr: "reserved route label key",
		},
		{
			name: "reserved platform route label",
			ingresses: AdditionalIngresses{{
				Name: "alpha", Hostname: "alpha.example.com", IngressControllerName: "shard-a", RouteLabels: map[string]string{"platform.opendatahub.io/part-of": "custom"},
			}},
			mode:    IngressModeOcpRoute,
			wantErr: "reserved route label key",
		},
		{
			name:      "duplicate hostname",
			ingresses: AdditionalIngresses{valid("alpha", "shared.example.com"), valid("beta", "shared.example.com")},
			mode:      IngressModeOcpRoute,
			wantErr:   "hostname \"shared.example.com\" conflicts with",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			err := tc.ingresses.Validate(tc.mode)
			if tc.wantErr == "" {
				g.Expect(err).NotTo(HaveOccurred())
				return
			}
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring(tc.wantErr))
		})
	}
}

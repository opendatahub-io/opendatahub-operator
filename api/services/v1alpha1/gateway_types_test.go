package v1alpha1

import (
	"testing"

	. "github.com/onsi/gomega"
)

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

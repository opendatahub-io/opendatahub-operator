package v1alpha1

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestAdditionalIngressesValidate(t *testing.T) {
	valid := func(name, hostname string, port int32) AdditionalIngress {
		return AdditionalIngress{
			Name:                  name,
			Hostname:              hostname,
			ListenerPort:          port,
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
			ingresses: AdditionalIngresses{valid("alpha", "alpha.example.com", 9443), valid("beta", "beta.example.com", 9444)},
			mode:      IngressModeOcpRoute,
		},
		{
			name:      "additional ingress requires OcpRoute",
			ingresses: AdditionalIngresses{valid("alpha", "alpha.example.com", 9443)},
			mode:      IngressModeLoadBalancer,
			wantErr:   "require OcpRoute",
		},
		{
			name:      "invalid name",
			ingresses: AdditionalIngresses{valid("bad_name", "alpha.example.com", 9443)},
			mode:      IngressModeOcpRoute,
			wantErr:   "invalid name",
		},
		{
			name:      "invalid hostname",
			ingresses: AdditionalIngresses{valid("alpha", "not a hostname", 9443)},
			mode:      IngressModeOcpRoute,
			wantErr:   "invalid hostname",
		},
		{
			name: "invalid controller name",
			ingresses: AdditionalIngresses{{
				Name: "alpha", Hostname: "alpha.example.com", ListenerPort: 9443,
				IngressControllerName: "bad_name", RouteLabels: map[string]string{"example.com/ingress": "alpha"},
			}},
			mode:    IngressModeOcpRoute,
			wantErr: "invalid IngressController name",
		},
		{
			name: "missing route labels",
			ingresses: AdditionalIngresses{{
				Name: "alpha", Hostname: "alpha.example.com", ListenerPort: 9443,
				IngressControllerName: "shard-a",
			}},
			mode:    IngressModeOcpRoute,
			wantErr: "must define route labels",
		},
		{
			name: "invalid route label",
			ingresses: AdditionalIngresses{{
				Name: "alpha", Hostname: "alpha.example.com", ListenerPort: 9443,
				IngressControllerName: "shard-a", RouteLabels: map[string]string{"bad key": "alpha"},
			}},
			mode:    IngressModeOcpRoute,
			wantErr: "invalid route label key",
		},
		{
			name: "invalid route label value",
			ingresses: AdditionalIngresses{{
				Name: "alpha", Hostname: "alpha.example.com", ListenerPort: 9443,
				IngressControllerName: "shard-a", RouteLabels: map[string]string{"example.com/ingress": "bad value"},
			}},
			mode:    IngressModeOcpRoute,
			wantErr: "invalid route label value",
		},
		{
			name:      "reserved name",
			ingresses: AdditionalIngresses{valid(DefaultGatewayListenerName, "alpha.example.com", 9443)},
			mode:      IngressModeOcpRoute,
			wantErr:   "reserved listener name",
		},
		{
			name:      "reserved legacy name",
			ingresses: AdditionalIngresses{valid(LegacyGatewayListenerName, "alpha.example.com", 9443)},
			mode:      IngressModeOcpRoute,
			wantErr:   "reserved listener name",
		},
		{
			name:      "duplicate name",
			ingresses: AdditionalIngresses{valid("alpha", "alpha.example.com", 9443), valid("alpha", "beta.example.com", 9444)},
			mode:      IngressModeOcpRoute,
			wantErr:   "duplicate name",
		},
		{
			name:      "duplicate hostname",
			ingresses: AdditionalIngresses{valid("alpha", "shared.example.com", 9443), valid("beta", "shared.example.com", 9444)},
			mode:      IngressModeOcpRoute,
			wantErr:   "hostname \"shared.example.com\" conflicts with",
		},
		{
			name:      "invalid port",
			ingresses: AdditionalIngresses{valid("alpha", "alpha.example.com", 0)},
			mode:      IngressModeOcpRoute,
			wantErr:   "invalid listener port",
		},
		{
			name:      "reserved port",
			ingresses: AdditionalIngresses{valid("alpha", "alpha.example.com", DefaultGatewayListenerPort)},
			mode:      IngressModeOcpRoute,
			wantErr:   "conflicts with",
		},
		{
			name:      "duplicate port",
			ingresses: AdditionalIngresses{valid("alpha", "alpha.example.com", 9443), valid("beta", "beta.example.com", 9443)},
			mode:      IngressModeOcpRoute,
			wantErr:   "conflicts with",
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

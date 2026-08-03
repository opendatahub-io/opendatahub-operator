/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gateway_test

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	pkgtls "github.com/opendatahub-io/opendatahub-operator/v2/pkg/tls"
)

func TestTlsProfileSpecFromSecurityProfile(t *testing.T) {
	t.Parallel()

	intermediateSpec := configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	oldSpec := configv1.TLSProfiles[configv1.TLSProfileOldType]
	modernSpec := configv1.TLSProfiles[configv1.TLSProfileModernType]

	customCiphers := []string{"ECDHE-RSA-AES128-GCM-SHA256"}
	customSpec := &configv1.TLSProfileSpec{
		Ciphers:       customCiphers,
		MinTLSVersion: configv1.VersionTLS11,
	}

	tests := []struct {
		name     string
		profile  *configv1.TLSSecurityProfile
		expected *configv1.TLSProfileSpec
	}{
		{
			name:     "nil profile defaults to intermediate",
			profile:  nil,
			expected: intermediateSpec,
		},
		{
			name: "intermediate type",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
			expected: intermediateSpec,
		},
		{
			name: "old type",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileOldType,
			},
			expected: oldSpec,
		},
		{
			name: "modern type",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileModernType,
			},
			expected: modernSpec,
		},
		{
			name: "custom type with spec",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{
					TLSProfileSpec: *customSpec,
				},
			},
			expected: customSpec,
		},
		{
			name: "custom type without spec falls back to intermediate",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
			},
			expected: intermediateSpec,
		},
		{
			name: "unknown type falls back to intermediate",
			profile: &configv1.TLSSecurityProfile{
				Type: "Unknown",
			},
			expected: intermediateSpec,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := pkgtls.ProfileSpecFromSecurityProfile(tt.profile)
			require.NotNil(t, got)
			assert.Equal(t, tt.expected.MinTLSVersion, got.MinTLSVersion)
			assert.Equal(t, tt.expected.Ciphers, got.Ciphers)
		})
	}
}

// intermediateIANACiphers is the expected comma-joined IANA cipher string for the
// Intermediate TLS profile. Derived directly from the openshift/api TLSProfileIntermediateType
// cipher list passed through the openshift/library-go OpenSSL->IANA mapping.
// It is intentionally NOT computed from the production CipherSuitesFromSpec
// so that it serves as an independent oracle.
const intermediateIANACiphers = "TLS_AES_128_GCM_SHA256," +
	"TLS_AES_256_GCM_SHA384," +
	"TLS_CHACHA20_POLY1305_SHA256," +
	"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256," +
	"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256," +
	"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384," +
	"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384," +
	"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256," +
	"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256"

// oldIANACiphers is the expected comma-joined IANA cipher string for the Old TLS profile,
// derived with the same methodology as intermediateIANACiphers.
const oldIANACiphers = "TLS_AES_128_GCM_SHA256," +
	"TLS_AES_256_GCM_SHA384," +
	"TLS_CHACHA20_POLY1305_SHA256," +
	"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256," +
	"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256," +
	"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384," +
	"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384," +
	"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256," +
	"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256," +
	"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256," +
	"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256," +
	"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA," +
	"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA," +
	"TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA," +
	"TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA," +
	"TLS_RSA_WITH_AES_128_GCM_SHA256," +
	"TLS_RSA_WITH_AES_256_GCM_SHA384," +
	"TLS_RSA_WITH_AES_128_CBC_SHA256," +
	"TLS_RSA_WITH_AES_128_CBC_SHA," +
	"TLS_RSA_WITH_AES_256_CBC_SHA," +
	"TLS_RSA_WITH_3DES_EDE_CBC_SHA"

func TestKubeAuthProxyTLSFromProfile(t *testing.T) {
	t.Parallel()

	minVersion, cipherSuites := gateway.KubeAuthProxyTLSFromProfile(context.Background(), nil)
	assert.Equal(t, "TLS1.2", minVersion)
	assert.Equal(t, intermediateIANACiphers, cipherSuites)

	minVersion, cipherSuites = gateway.KubeAuthProxyTLSFromProfile(context.Background(), &configv1.TLSSecurityProfile{Type: configv1.TLSProfileOldType})
	assert.Equal(t, "TLS1.2", minVersion)
	assert.Equal(t, intermediateIANACiphers, cipherSuites)

	minVersion, cipherSuites = gateway.KubeAuthProxyTLSFromProfile(context.Background(), &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType})
	assert.Equal(t, "TLS1.2", minVersion)
	assert.Equal(t, intermediateIANACiphers, cipherSuites)

	customProfile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{
			TLSProfileSpec: configv1.TLSProfileSpec{
				Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
				MinTLSVersion: configv1.VersionTLS12,
			},
		},
	}
	minVersion, cipherSuites = gateway.KubeAuthProxyTLSFromProfile(context.Background(), customProfile)
	assert.Equal(t, "TLS1.2", minVersion)
	assert.Equal(t, "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", cipherSuites)

	minVersion, cipherSuites = gateway.KubeAuthProxyTLSFromProfile(context.Background(), &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType})
	assert.Equal(t, "TLS1.3", minVersion)
	assert.Equal(t, "TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256", cipherSuites)
}

func TestGetKubeAuthProxyTLSFromAPIServer(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, configv1.Install(scheme))

	t.Run("missing APIServer uses intermediate defaults", func(t *testing.T) {
		t.Parallel()
		cli := fake.NewClientBuilder().WithScheme(scheme).Build()

		minVersion, cipherSuites, err := gateway.GetKubeAuthProxyTLSFromAPIServer(context.Background(), cli)
		require.NoError(t, err)
		assert.Equal(t, "TLS1.2", minVersion)
		assert.Equal(t, intermediateIANACiphers, cipherSuites)
	})

	t.Run("APIServer without tlsSecurityProfile uses intermediate defaults", func(t *testing.T) {
		t.Parallel()
		apiServer := &configv1.APIServer{
			ObjectMeta: metav1.ObjectMeta{Name: cluster.ClusterAPIServerObj},
		}
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiServer).Build()

		minVersion, cipherSuites, err := gateway.GetKubeAuthProxyTLSFromAPIServer(context.Background(), cli)
		require.NoError(t, err)
		assert.Equal(t, "TLS1.2", minVersion)
		assert.Equal(t, intermediateIANACiphers, cipherSuites)
	})

	t.Run("APIServer with old profile", func(t *testing.T) {
		t.Parallel()
		apiServer := &configv1.APIServer{
			ObjectMeta: metav1.ObjectMeta{Name: cluster.ClusterAPIServerObj},
			Spec: configv1.APIServerSpec{
				TLSSecurityProfile: &configv1.TLSSecurityProfile{
					Type: configv1.TLSProfileOldType,
				},
			},
		}
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiServer).Build()

		minVersion, cipherSuites, err := gateway.GetKubeAuthProxyTLSFromAPIServer(context.Background(), cli)
		require.NoError(t, err)
		assert.Equal(t, "TLS1.2", minVersion)
		assert.Equal(t, intermediateIANACiphers, cipherSuites)
	})

	t.Run("APIServer with custom profile that has unsupported TLS version floors ciphers too", func(t *testing.T) {
		t.Parallel()
		apiServer := &configv1.APIServer{
			ObjectMeta: metav1.ObjectMeta{Name: cluster.ClusterAPIServerObj},
			Spec: configv1.APIServerSpec{
				TLSSecurityProfile: &configv1.TLSSecurityProfile{
					Type: configv1.TLSProfileCustomType,
					Custom: &configv1.CustomTLSProfile{
						TLSProfileSpec: configv1.TLSProfileSpec{
							Ciphers:       []string{"ECDHE-RSA-AES256-GCM-SHA384", "DES-CBC3-SHA"},
							MinTLSVersion: configv1.VersionTLS11,
						},
					},
				},
			},
		}
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiServer).Build()

		minVersion, cipherSuites, err := gateway.GetKubeAuthProxyTLSFromAPIServer(context.Background(), cli)
		require.NoError(t, err)
		assert.Equal(t, "TLS1.2", minVersion)
		assert.Equal(t, intermediateIANACiphers, cipherSuites,
			"unsupported MinTLSVersion must floor ciphers to Intermediate, not retain custom weak ciphers")
	})

	t.Run("APIServer with custom profile", func(t *testing.T) {
		t.Parallel()
		customSpec := configv1.TLSProfileSpec{
			Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
			MinTLSVersion: configv1.VersionTLS12,
		}
		apiServer := &configv1.APIServer{
			ObjectMeta: metav1.ObjectMeta{Name: cluster.ClusterAPIServerObj},
			Spec: configv1.APIServerSpec{
				TLSSecurityProfile: &configv1.TLSSecurityProfile{
					Type: configv1.TLSProfileCustomType,
					Custom: &configv1.CustomTLSProfile{
						TLSProfileSpec: customSpec,
					},
				},
			},
		}
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiServer).Build()

		minVersion, cipherSuites, err := gateway.GetKubeAuthProxyTLSFromAPIServer(context.Background(), cli)
		require.NoError(t, err)
		assert.Equal(t, "TLS1.2", minVersion)
		assert.Equal(t, "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", cipherSuites)
	})
}

func TestTLSCipherSuitesFromSpec(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("nil spec falls back to intermediate", func(t *testing.T) {
		t.Parallel()
		got := pkgtls.CipherSuitesFromSpec(ctx, nil)
		assert.Equal(t, intermediateIANACiphers, got)
	})

	t.Run("profile with only unmappable DHE ciphers falls back to intermediate", func(t *testing.T) {
		t.Parallel()
		spec := &configv1.TLSProfileSpec{
			Ciphers:       []string{"DHE-RSA-AES128-GCM-SHA256", "DHE-RSA-AES256-GCM-SHA384"},
			MinTLSVersion: configv1.VersionTLS12,
		}
		got := pkgtls.CipherSuitesFromSpec(ctx, spec)
		assert.Equal(t, intermediateIANACiphers, got,
			"all-DHE profile should fall back to Intermediate, not produce an empty string")
	})

	t.Run("empty ciphers slice falls back to intermediate", func(t *testing.T) {
		t.Parallel()
		spec := &configv1.TLSProfileSpec{
			Ciphers:       []string{},
			MinTLSVersion: configv1.VersionTLS12,
		}
		got := pkgtls.CipherSuitesFromSpec(ctx, spec)
		assert.Equal(t, intermediateIANACiphers, got,
			"empty cipher list should fall back to Intermediate, not produce an empty string")
	})

	t.Run("profile with mixed ciphers retains only mappable ones", func(t *testing.T) {
		t.Parallel()
		spec := &configv1.TLSProfileSpec{
			Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256", "DHE-RSA-AES128-GCM-SHA256"},
			MinTLSVersion: configv1.VersionTLS12,
		}
		got := pkgtls.CipherSuitesFromSpec(ctx, spec)
		assert.Equal(t, "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", got,
			"only the mappable cipher should remain; no fallback when at least one cipher maps")
	})

	t.Run("intermediate profile produces expected IANA ciphers", func(t *testing.T) {
		t.Parallel()
		got := pkgtls.CipherSuitesFromSpec(ctx, configv1.TLSProfiles[configv1.TLSProfileIntermediateType])
		assert.Equal(t, intermediateIANACiphers, got)
	})

	t.Run("old profile produces expected IANA ciphers", func(t *testing.T) {
		t.Parallel()
		got := pkgtls.CipherSuitesFromSpec(ctx, configv1.TLSProfiles[configv1.TLSProfileOldType])
		assert.Equal(t, oldIANACiphers, got)
	})
}

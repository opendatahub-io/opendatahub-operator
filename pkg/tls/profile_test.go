package tls_test

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgtls "github.com/opendatahub-io/opendatahub-operator/v2/pkg/tls"
)

func TestMinVersionFromSpec(t *testing.T) {
	tests := []struct {
		name     string
		version  configv1.TLSProtocolVersion
		format   pkgtls.VersionFormat
		expected string
	}{
		{name: "TLS 1.2 short", version: configv1.VersionTLS12, format: pkgtls.FormatShort, expected: "TLS1.2"},
		{name: "TLS 1.3 short", version: configv1.VersionTLS13, format: pkgtls.FormatShort, expected: "TLS1.3"},
		{name: "TLS 1.2 Go", version: configv1.VersionTLS12, format: pkgtls.FormatGo, expected: "VersionTLS12"},
		{name: "TLS 1.3 Go", version: configv1.VersionTLS13, format: pkgtls.FormatGo, expected: "VersionTLS13"},
		{name: "TLS 1.0 floors to 1.2 short", version: configv1.VersionTLS10, format: pkgtls.FormatShort, expected: "TLS1.2"},
		{name: "TLS 1.0 floors to 1.2 Go", version: configv1.VersionTLS10, format: pkgtls.FormatGo, expected: "VersionTLS12"},
		{name: "TLS 1.1 floors to 1.2 short", version: configv1.VersionTLS11, format: pkgtls.FormatShort, expected: "TLS1.2"},
		{name: "TLS 1.1 floors to 1.2 Go", version: configv1.VersionTLS11, format: pkgtls.FormatGo, expected: "VersionTLS12"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := &configv1.TLSProfileSpec{MinTLSVersion: tt.version}
			assert.Equal(t, tt.expected, pkgtls.MinVersionFromSpec(context.Background(), spec, tt.format))
		})
	}
}

func TestMinVersionFromSpec_NilSpec(t *testing.T) {
	assert.Equal(t, "TLS1.2", pkgtls.MinVersionFromSpec(context.Background(), nil, pkgtls.FormatShort))
	assert.Equal(t, "VersionTLS12", pkgtls.MinVersionFromSpec(context.Background(), nil, pkgtls.FormatGo))
}

func TestFromProfile_Nil(t *testing.T) {
	minVersion, cipherSuites := pkgtls.FromProfile(context.Background(), nil, pkgtls.FormatShort)
	assert.Equal(t, "TLS1.2", minVersion)
	assert.NotEmpty(t, cipherSuites)

	minVersion, cipherSuites = pkgtls.FromProfile(context.Background(), nil, pkgtls.FormatGo)
	assert.Equal(t, "VersionTLS12", minVersion)
	assert.NotEmpty(t, cipherSuites)
}

func TestFromProfile_Old(t *testing.T) {
	minVersion, cipherSuites := pkgtls.FromProfile(context.Background(), &configv1.TLSSecurityProfile{Type: configv1.TLSProfileOldType}, pkgtls.FormatShort)
	assert.Equal(t, "TLS1.2", minVersion)
	assert.NotEmpty(t, cipherSuites)
}

func TestFromProfile_Modern(t *testing.T) {
	minVersion, _ := pkgtls.FromProfile(context.Background(), &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}, pkgtls.FormatShort)
	assert.Equal(t, "TLS1.3", minVersion)

	minVersion, _ = pkgtls.FromProfile(context.Background(), &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}, pkgtls.FormatGo)
	assert.Equal(t, "VersionTLS13", minVersion)
}

func TestIsVersionSupported(t *testing.T) {
	assert.True(t, pkgtls.IsVersionSupported(configv1.VersionTLS12))
	assert.True(t, pkgtls.IsVersionSupported(configv1.VersionTLS13))
	assert.False(t, pkgtls.IsVersionSupported(configv1.VersionTLS10))
	assert.False(t, pkgtls.IsVersionSupported(configv1.VersionTLS11))
}

func TestShouldHonorClusterTLSProfile(t *testing.T) {
	assert.False(t, pkgtls.ShouldHonorClusterTLSProfile(configv1.TLSAdherencePolicyNoOpinion))
	assert.False(t, pkgtls.ShouldHonorClusterTLSProfile(configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly))
	assert.True(t, pkgtls.ShouldHonorClusterTLSProfile(configv1.TLSAdherencePolicyStrictAllComponents))
	assert.True(t, pkgtls.ShouldHonorClusterTLSProfile("FuturePolicy"))
}

func TestFromProfileStrict(t *testing.T) {
	tests := []struct {
		name        string
		profile     *configv1.TLSSecurityProfile
		format      pkgtls.VersionFormat
		wantVersion string
		wantCiphers string
		wantErr     string
	}{
		{
			name:        "nil profile uses intermediate defaults",
			profile:     nil,
			format:      pkgtls.FormatShort,
			wantVersion: "TLS1.2",
			wantCiphers: "TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384," +
				"TLS_CHACHA20_POLY1305_SHA256," +
				"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256," +
				"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256," +
				"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384," +
				"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384," +
				"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256," +
				"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
		},
		{
			name:        "modern profile short format",
			profile:     &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType},
			format:      pkgtls.FormatShort,
			wantVersion: "TLS1.3",
			wantCiphers: "TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256",
		},
		{
			name:        "modern profile Go format",
			profile:     &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType},
			format:      pkgtls.FormatGo,
			wantVersion: "VersionTLS13",
			wantCiphers: "TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256",
		},
		{
			name: "custom TLS 1.2 profile",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
					Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
					MinTLSVersion: configv1.VersionTLS12,
				}},
			},
			format:      pkgtls.FormatGo,
			wantVersion: "VersionTLS12",
			wantCiphers: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
		},
		{
			name: "custom TLS 1.3 profile with empty ciphers",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
					MinTLSVersion: configv1.VersionTLS13,
				}},
			},
			format:      pkgtls.FormatShort,
			wantVersion: "TLS1.3",
		},
		{
			name: "custom TLS 1.3 cipher subset is informational",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
					Ciphers:       []string{"TLS_AES_128_GCM_SHA256"},
					MinTLSVersion: configv1.VersionTLS13,
				}},
			},
			format:      pkgtls.FormatShort,
			wantVersion: "TLS1.3",
			wantCiphers: "TLS_AES_128_GCM_SHA256",
		},
		{
			name: "custom TLS 1.3 unmappable ciphers are informational",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
					Ciphers:       []string{"DHE-RSA-AES128-GCM-SHA256"},
					MinTLSVersion: configv1.VersionTLS13,
				}},
			},
			format:      pkgtls.FormatShort,
			wantVersion: "TLS1.3",
		},
		{
			name: "partially unmappable TLS 1.2 ciphers keep supported entries",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
					Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256", "DHE-RSA-AES128-GCM-SHA256"},
					MinTLSVersion: configv1.VersionTLS12,
				}},
			},
			format:      pkgtls.FormatShort,
			wantVersion: "TLS1.2",
			wantCiphers: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
		},
		{
			name: "custom profile with nil specification",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
			},
			wantErr: "custom TLS profile has no custom specification",
		},
		{
			name:    "unsupported profile type",
			profile: &configv1.TLSSecurityProfile{Type: "Unsupported"},
			wantErr: "unsupported TLS profile type",
		},
		{
			name: "TLS 1.0 is rejected",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
					Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
					MinTLSVersion: configv1.VersionTLS10,
				}},
			},
			wantErr: "minimum version",
		},
		{
			name: "TLS 1.1 is rejected",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
					Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
					MinTLSVersion: configv1.VersionTLS11,
				}},
			},
			wantErr: "minimum version",
		},
		{
			name: "empty TLS 1.2 cipher list is rejected",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
					MinTLSVersion: configv1.VersionTLS12,
				}},
			},
			wantErr: "no cipher suites",
		},
		{
			name: "all unmappable ciphers are rejected",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
					Ciphers:       []string{"DHE-RSA-AES128-GCM-SHA256", "DHE-RSA-AES256-GCM-SHA384"},
					MinTLSVersion: configv1.VersionTLS12,
				}},
			},
			wantErr: "no cipher suites supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minVersion, cipherSuites, err := pkgtls.FromProfileStrict(context.Background(), tt.profile, tt.format)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Empty(t, minVersion)
				assert.Empty(t, cipherSuites)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantVersion, minVersion)
			assert.Equal(t, tt.wantCiphers, cipherSuites)
		})
	}
}

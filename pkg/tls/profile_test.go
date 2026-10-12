package tls_test

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

func TestFromProfileUnsupportedCurvesPreservesLegacySettings(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
			MinTLSVersion: configv1.VersionTLS12,
			Groups:        []configv1.TLSGroup{"unsupported-group"},
		}},
	}

	minVersion, cipherSuites := pkgtls.FromProfile(context.Background(), profile, pkgtls.FormatShort)
	assert.Equal(t, "TLS1.2", minVersion)
	assert.Equal(t, "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", cipherSuites)
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

func TestCurvePreferencesFromSpec(t *testing.T) {
	tests := []struct {
		name    string
		spec    *configv1.TLSProfileSpec
		want    string
		wantErr bool
	}{
		{name: "nil spec", spec: nil, want: ""},
		{name: "no groups", spec: &configv1.TLSProfileSpec{}, want: ""},
		{
			name:    "all groups unsupported",
			spec:    &configv1.TLSProfileSpec{Groups: []configv1.TLSGroup{"unsupported-group"}},
			wantErr: true,
		},
		{
			name: "supported groups",
			spec: &configv1.TLSProfileSpec{Groups: []configv1.TLSGroup{
				configv1.TLSGroupX25519,
				configv1.TLSGroupSecP256r1,
			}},
			want: "29,23",
		},
		{
			name: "unsupported groups are dropped",
			spec: &configv1.TLSProfileSpec{Groups: []configv1.TLSGroup{
				"unsupported-group",
				configv1.TLSGroupX25519,
			}},
			want: "29",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pkgtls.CurvePreferencesFromSpec(context.Background(), tt.spec)
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFromProfileStrictWithCurvePreferences(t *testing.T) {
	// nil profile: strict path defaults to Intermediate, which has supported groups.
	minVersion, cipherSuites, curves, err := pkgtls.FromProfileStrictWithCurvePreferences(
		context.Background(), nil, pkgtls.FormatShort)
	require.NoError(t, err)
	assert.Equal(t, "TLS1.2", minVersion)
	assert.NotEmpty(t, cipherSuites)
	assert.NotEmpty(t, curves)

	// strict rejection of an unsupported minimum propagates.
	profile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
			MinTLSVersion: configv1.VersionTLS10,
		}},
	}
	minVersion, cipherSuites, curves, err = pkgtls.FromProfileStrictWithCurvePreferences(context.Background(), profile, pkgtls.FormatShort)
	require.Error(t, err)
	assert.Empty(t, minVersion)
	assert.Empty(t, cipherSuites)
	assert.Empty(t, curves)
}

func TestFromProfileStrictWithCurvePreferences_RejectsUnsupportedGroups(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
			MinTLSVersion: configv1.VersionTLS12,
			Groups:        []configv1.TLSGroup{"unsupported-group"},
		}},
	}
	minVersion, cipherSuites, curves, err := pkgtls.FromProfileStrictWithCurvePreferences(
		context.Background(), profile, pkgtls.FormatShort)
	require.Error(t, err)
	assert.Empty(t, minVersion)
	assert.Empty(t, cipherSuites)
	assert.Empty(t, curves)
}

func TestFromProfileUnsupportedCurvesFloorsVersion(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS10,
			Groups:        []configv1.TLSGroup{"unsupported-group"},
		}},
	}

	minVersion, cipherSuites := pkgtls.FromProfile(context.Background(), profile, pkgtls.FormatShort)
	assert.Equal(t, "TLS1.2", minVersion)
	assert.NotEmpty(t, cipherSuites)

	minVersion, _ = pkgtls.FromProfile(context.Background(), profile, pkgtls.FormatGo)
	assert.Equal(t, "VersionTLS12", minVersion)
}

// stubReader implements client.Reader over a fixed set of objects for APIServer resolution tests.
type stubReader struct {
	objects []client.Object
	getErr  error
}

func (s *stubReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	if s.getErr != nil {
		return s.getErr
	}
	for _, o := range s.objects {
		if o.GetName() == key.Name {
			if apiServer, ok := o.(*configv1.APIServer); ok {
				if as, ok := obj.(*configv1.APIServer); ok {
					apiServer.DeepCopyInto(as)
					return nil
				}
				return nil
			}
		}
	}
	return k8serr.NewNotFound(schema.GroupResource{Group: "config.openshift.io", Resource: "apiservers"}, key.Name)
}

func (s *stubReader) List(ctx context.Context, list client.ObjectList, _ ...client.ListOption) error {
	return nil
}

func apiServerWith(profile *configv1.TLSSecurityProfile, adherence configv1.TLSAdherencePolicy) *configv1.APIServer {
	return &configv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec: configv1.APIServerSpec{
			TLSAdherence:       adherence,
			TLSSecurityProfile: profile,
		},
	}
}

func TestFromAPIServerWithCurvePreferences(t *testing.T) {
	strictProfile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
			MinTLSVersion: configv1.VersionTLS12,
			Groups:        []configv1.TLSGroup{configv1.TLSGroupX25519},
		}},
	}

	tests := []struct {
		name          string
		reader        *stubReader
		wantVersion   string
		wantCurves    string
		wantErr       bool
		wantErrString string
	}{
		{
			name:        "no APIServer object uses intermediate defaults",
			reader:      &stubReader{},
			wantVersion: "TLS1.2",
			wantCurves:  "4588,29,23,24",
		},
		{
			name:        "NoOpinion adherence uses intermediate defaults",
			reader:      &stubReader{objects: []client.Object{apiServerWith(strictProfile, configv1.TLSAdherencePolicyNoOpinion)}},
			wantVersion: "TLS1.2",
			wantCurves:  "4588,29,23,24",
		},
		{
			name:        "strict adherence applies the profile",
			reader:      &stubReader{objects: []client.Object{apiServerWith(strictProfile, configv1.TLSAdherencePolicyStrictAllComponents)}},
			wantVersion: "TLS1.2",
			wantCurves:  "29",
		},
		{
			name: "unsupported strict profile fails closed",
			reader: &stubReader{objects: []client.Object{
				apiServerWith(&configv1.TLSSecurityProfile{Type: "Unsupported"}, configv1.TLSAdherencePolicyStrictAllComponents),
			}},
			wantVersion: "",
			wantErr:     true,
		},
		{
			name:          "denied API read fails closed",
			reader:        &stubReader{getErr: k8serr.NewForbidden(schema.GroupResource{Group: "config.openshift.io", Resource: "apiservers"}, "cluster", nil)},
			wantVersion:   "",
			wantErr:       true,
			wantErrString: "failed to get APIServer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minVersion, _, curves, err := pkgtls.FromAPIServerWithCurvePreferences(context.Background(), tt.reader, pkgtls.FormatShort)
			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrString != "" {
					assert.Contains(t, err.Error(), tt.wantErrString)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantVersion, minVersion)
			assert.Equal(t, tt.wantCurves, curves)
		})
	}
}

func TestFromAPIServer(t *testing.T) {
	// no APIServer object at all: falls back to the legacy resolver.
	minVersion, cipherSuites, err := pkgtls.FromAPIServer(context.Background(), &stubReader{}, pkgtls.FormatShort)
	require.NoError(t, err)
	assert.Equal(t, "TLS1.2", minVersion)
	assert.NotEmpty(t, cipherSuites)

	// strict profile present: resolves through the strict path.
	profile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
			MinTLSVersion: configv1.VersionTLS12,
		}},
	}
	reader := &stubReader{objects: []client.Object{apiServerWith(profile, configv1.TLSAdherencePolicyStrictAllComponents)}}
	minVersion, cipherSuites, err = pkgtls.FromAPIServer(context.Background(), reader, pkgtls.FormatGo)
	require.NoError(t, err)
	assert.Equal(t, "VersionTLS12", minVersion)
	assert.Equal(t, "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", cipherSuites)

	// a denied read fails closed instead of defaulting.
	reader = &stubReader{getErr: k8serr.NewForbidden(schema.GroupResource{Group: "config.openshift.io", Resource: "apiservers"}, "cluster", nil)}
	_, _, err = pkgtls.FromAPIServer(context.Background(), reader, pkgtls.FormatShort)
	require.Error(t, err)
}

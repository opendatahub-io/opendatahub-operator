package tls_test

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"

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

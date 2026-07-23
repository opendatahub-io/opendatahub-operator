package tls

import (
	"context"
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	ocpcrypto "github.com/openshift/library-go/pkg/crypto"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// VersionFormat controls the output format of TLS version strings.
type VersionFormat int

const (
	// FormatShort outputs "TLS1.2", "TLS1.3" (used by kube-auth-proxy).
	FormatShort VersionFormat = iota
	// FormatGo outputs "VersionTLS12", "VersionTLS13" (used by upstream kube-rbac-proxy).
	FormatGo
)

// ProfileSpecFromSecurityProfile resolves a TLSSecurityProfile to a concrete TLSProfileSpec.
// Returns the Intermediate profile for nil input or unknown types.
func ProfileSpecFromSecurityProfile(profile *configv1.TLSSecurityProfile) *configv1.TLSProfileSpec {
	if profile == nil {
		return configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	}

	switch profile.Type {
	case configv1.TLSProfileCustomType:
		if profile.Custom != nil {
			return &profile.Custom.TLSProfileSpec
		}
	case configv1.TLSProfileOldType, configv1.TLSProfileIntermediateType, configv1.TLSProfileModernType:
		if spec := configv1.TLSProfiles[profile.Type]; spec != nil {
			return spec
		}
	}

	return configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
}

func minVersionToShort(v configv1.TLSProtocolVersion) string {
	switch v {
	case configv1.VersionTLS12:
		return "TLS1.2"
	case configv1.VersionTLS13:
		return "TLS1.3"
	default:
		return ""
	}
}

func minVersionToGo(v configv1.TLSProtocolVersion) string {
	switch v {
	case configv1.VersionTLS12:
		return "VersionTLS12"
	case configv1.VersionTLS13:
		return "VersionTLS13"
	default:
		return ""
	}
}

// MinVersionFromSpec returns the TLS minimum version string for the given profile spec.
// Unsupported versions (TLS 1.0, 1.1) fall back to TLS 1.2.
func MinVersionFromSpec(ctx context.Context, spec *configv1.TLSProfileSpec, format VersionFormat) string {
	l := logf.FromContext(ctx).WithName("MinVersionFromSpec")
	minVersion := configv1.TLSProfiles[configv1.TLSProfileIntermediateType].MinTLSVersion

	if spec != nil && spec.MinTLSVersion != "" {
		minVersion = spec.MinTLSVersion
	}

	var name string
	switch format {
	case FormatGo:
		name = minVersionToGo(minVersion)
	default:
		name = minVersionToShort(minVersion)
	}

	if name == "" {
		l.V(1).Info("unsupported MinTLSVersion, using TLS 1.2 as floor", "minVersion", minVersion)
		if format == FormatGo {
			return "VersionTLS12"
		}
		return "TLS1.2"
	}
	return name
}

// CipherSuitesFromSpec returns a comma-separated list of IANA cipher suite names
// for the given profile spec. Unsupported OpenSSL names are logged and dropped.
func CipherSuitesFromSpec(ctx context.Context, spec *configv1.TLSProfileSpec) string {
	l := logf.FromContext(ctx).WithName("CipherSuitesFromSpec")
	if spec == nil {
		spec = configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	}

	var dropped []string
	for _, c := range spec.Ciphers {
		if len(ocpcrypto.OpenSSLToIANACipherSuites([]string{c})) == 0 {
			dropped = append(dropped, c)
		}
	}
	if len(dropped) > 0 {
		l.V(1).Info("cipher suites unsupported by Go crypto/tls were dropped",
			"dropped", dropped, "droppedCount", len(dropped), "totalRequested", len(spec.Ciphers))
	}

	ianaCiphers := ocpcrypto.OpenSSLToIANACipherSuites(spec.Ciphers)
	if len(ianaCiphers) == 0 {
		l.V(1).Info("no mappable cipher suites in profile, falling back to Intermediate profile ciphers")
		ianaCiphers = ocpcrypto.OpenSSLToIANACipherSuites(
			configv1.TLSProfiles[configv1.TLSProfileIntermediateType].Ciphers,
		)
	}

	return strings.Join(ianaCiphers, ",")
}

// IsVersionSupported returns true if the MinTLSVersion can be mapped to a proxy flag value.
func IsVersionSupported(v configv1.TLSProtocolVersion) bool {
	return minVersionToShort(v) != ""
}

// FromProfile resolves a TLSSecurityProfile to version and cipher strings.
// If the profile's MinTLSVersion is unsupported (TLS 1.0/1.1), both version
// and ciphers are floored to the Intermediate profile.
func FromProfile(ctx context.Context, profile *configv1.TLSSecurityProfile, format VersionFormat) (string, string) {
	l := logf.FromContext(ctx).WithName("FromProfile")
	spec := ProfileSpecFromSecurityProfile(profile)

	if !IsVersionSupported(spec.MinTLSVersion) {
		l.V(1).Info("unsupported MinTLSVersion; flooring version and ciphers to Intermediate profile",
			"requestedMinVersion", spec.MinTLSVersion)
		spec = configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	}

	return MinVersionFromSpec(ctx, spec, format), CipherSuitesFromSpec(ctx, spec)
}

// FromAPIServer fetches the cluster TLS profile and returns version and cipher strings.
func FromAPIServer(ctx context.Context, cli client.Reader, format VersionFormat) (string, string, error) {
	apiServer := &configv1.APIServer{}
	if err := cli.Get(ctx, client.ObjectKey{Name: cluster.ClusterAPIServerObj}, apiServer); err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			minVersion, cipherSuites := FromProfile(ctx, nil, format)
			return minVersion, cipherSuites, nil
		}
		return "", "", fmt.Errorf("failed to get APIServer %q: %w", cluster.ClusterAPIServerObj, err)
	}

	minVersion, cipherSuites := FromProfile(ctx, apiServer.Spec.TLSSecurityProfile, format)
	return minVersion, cipherSuites, nil
}

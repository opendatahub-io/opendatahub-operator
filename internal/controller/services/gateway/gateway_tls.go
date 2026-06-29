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

package gateway

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

func tlsProfileSpecFromSecurityProfile(profile *configv1.TLSSecurityProfile) *configv1.TLSProfileSpec {
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

func tlsMinVersionFromProfileSpec(ctx context.Context, spec *configv1.TLSProfileSpec) string {
	l := logf.FromContext(ctx).WithName("tlsMinVersionFromProfileSpec")
	// default to intermediate profile
	minVersion := configv1.TLSProfiles[configv1.TLSProfileIntermediateType].MinTLSVersion

	if spec != nil && spec.MinTLSVersion != "" {
		minVersion = spec.MinTLSVersion
	}
	minVersionName := tlsMinVersionToName(minVersion)
	if minVersionName == "" {
		l.V(1).Info("unsupported MinTLSVersion, using TLS1.2 as floor", "minVersion", minVersion)
		return "TLS1.2"
	}
	return minVersionName
}

func tlsMinVersionToName(minVersion configv1.TLSProtocolVersion) string {
	switch minVersion {
	case configv1.VersionTLS12:
		return "TLS1.2"
	case configv1.VersionTLS13:
		return "TLS1.3"
	default:
		return ""
	}
}
func TLSCipherSuitesFromProfileSpec(ctx context.Context, spec *configv1.TLSProfileSpec) string {
	l := logf.FromContext(ctx).WithName("TLSCipherSuitesFromProfileSpec")
	if spec == nil {
		spec = configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	}

	// Detect OpenSSL cipher names that Go's crypto/tls does not support (e.g. DHE variants).
	// OpenSSLToIANACipherSuites silently drops them, so we log them here to make the
	// divergence from the cluster config observable.
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
		// All requested ciphers were unmappable or the list was empty; fall back to
		// Intermediate profile ciphers to avoid emitting --tls-cipher-suite= with no value.
		l.V(1).Info("no mappable cipher suites in profile, falling back to Intermediate profile ciphers")
		ianaCiphers = ocpcrypto.OpenSSLToIANACipherSuites(
			configv1.TLSProfiles[configv1.TLSProfileIntermediateType].Ciphers,
		)
	}

	return strings.Join(ianaCiphers, ",")
}

func KubeAuthProxyTLSFromProfile(ctx context.Context, profile *configv1.TLSSecurityProfile) (string, string) {
	l := logf.FromContext(ctx).WithName("KubeAuthProxyTLSFromProfile")
	spec := tlsProfileSpecFromSecurityProfile(profile)

	// If the MinTLSVersion is not directly supported by the proxy (TLS 1.0 / TLS 1.1),
	// floor the entire spec — not just the version — to the Intermediate profile.
	// Flooring only the version while keeping the original cipher list produces an
	// inconsistent config: e.g. "TLS 1.2 minimum" but still accepting 3DES
	// (TLS_RSA_WITH_3DES_EDE_CBC_SHA) from the Old profile.
	if tlsMinVersionToName(spec.MinTLSVersion) == "" {
		l.V(1).Info("unsupported MinTLSVersion; flooring version and ciphers to Intermediate profile",
			"requestedMinVersion", spec.MinTLSVersion)
		spec = configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	}

	return tlsMinVersionFromProfileSpec(ctx, spec), TLSCipherSuitesFromProfileSpec(ctx, spec)
}

func getKubeAuthProxyTLSFromAPIServer(ctx context.Context, cli client.Reader) (string, string, error) {
	apiServer := &configv1.APIServer{}
	if err := cli.Get(ctx, client.ObjectKey{Name: cluster.ClusterAPIServerObj}, apiServer); err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			minVersion, cipherSuites := KubeAuthProxyTLSFromProfile(ctx, nil)
			return minVersion, cipherSuites, nil
		}
		return "", "", fmt.Errorf("failed to get APIServer %q: %w", cluster.ClusterAPIServerObj, err)
	}

	minVersion, cipherSuites := KubeAuthProxyTLSFromProfile(ctx, apiServer.Spec.TLSSecurityProfile)
	return minVersion, cipherSuites, nil
}

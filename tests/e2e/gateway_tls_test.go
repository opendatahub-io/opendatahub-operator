package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	ocpcrypto "github.com/openshift/library-go/pkg/crypto"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

// kubeAuthProxyTLSDeploymentArgs is an independent test oracle: it computes the
// expected --tls-min-version and --tls-cipher-suite deployment args for a given
// TLS security profile without calling any production controller functions
// (KubeAuthProxyTLSFromProfile, TLSCipherSuitesFromProfileSpec, etc.).
func kubeAuthProxyTLSDeploymentArgs(profile *configv1.TLSSecurityProfile) (string, string) {
	spec := oracleProfileSpec(profile)
	return fmt.Sprintf("--tls-min-version=%s", oracleMinVersion(spec.MinTLSVersion)),
		fmt.Sprintf("--tls-cipher-suite=%s", oracleCipherSuites(spec.Ciphers))
}

// oracleProfileSpec resolves a TLSSecurityProfile to its TLSProfileSpec,
// mirroring the production fallback rules but independently of production code.
// When MinTLSVersion is not directly supported (TLS 1.0 / TLS 1.1), the entire
// spec is floored to Intermediate — matching the coupled floor in KubeAuthProxyTLSFromProfile.
func oracleProfileSpec(profile *configv1.TLSSecurityProfile) *configv1.TLSProfileSpec {
	if profile == nil {
		return configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	}

	var spec *configv1.TLSProfileSpec
	switch profile.Type {
	case configv1.TLSProfileCustomType:
		if profile.Custom != nil {
			spec = &profile.Custom.TLSProfileSpec
		}
	case configv1.TLSProfileOldType, configv1.TLSProfileIntermediateType, configv1.TLSProfileModernType:
		spec = configv1.TLSProfiles[profile.Type]
	}
	if spec == nil {
		return configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	}

	// If MinTLSVersion is not TLS 1.2 or TLS 1.3 (i.e. it would be floored), also
	// floor the ciphers so the oracle matches production behaviour.
	if spec.MinTLSVersion != configv1.VersionTLS12 && spec.MinTLSVersion != configv1.VersionTLS13 {
		return configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	}
	return spec
}

// oracleMinVersion maps a TLS protocol version to the proxy flag value,
// flooring unsupported versions (TLS 1.0, TLS 1.1) to TLS 1.2.
func oracleMinVersion(v configv1.TLSProtocolVersion) string {
	if v == configv1.VersionTLS13 {
		return "TLS1.3"
	}
	return "TLS1.2"
}

// oracleCipherSuites maps OpenSSL cipher names to IANA names via the library-go
// utility, with an Intermediate fallback when the result would otherwise be empty.
func oracleCipherSuites(ciphers []string) string {
	iana := ocpcrypto.OpenSSLToIANACipherSuites(ciphers)
	if len(iana) == 0 {
		iana = ocpcrypto.OpenSSLToIANACipherSuites(
			configv1.TLSProfiles[configv1.TLSProfileIntermediateType].Ciphers,
		)
	}
	return strings.Join(iana, ",")
}

func (tc *GatewayTestCtx) fetchClusterAPIServer(t *testing.T) (*configv1.APIServer, bool) {
	t.Helper()

	apiServer := &configv1.APIServer{}
	err := tc.Client().Get(tc.Context(), types.NamespacedName{Name: cluster.ClusterAPIServerObj}, apiServer)
	if err == nil {
		return apiServer, true
	}
	if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
		return nil, false
	}
	require.NoError(t, err, "failed to get cluster APIServer config")
	return nil, false
}

func (tc *GatewayTestCtx) expectedKubeAuthProxyTLSDeploymentArgs(t *testing.T) (string, string) {
	t.Helper()

	apiServer, found := tc.fetchClusterAPIServer(t)
	if !found {
		return kubeAuthProxyTLSDeploymentArgs(nil)
	}
	return kubeAuthProxyTLSDeploymentArgs(apiServer.Spec.TLSSecurityProfile)
}

func (tc *GatewayTestCtx) eventuallyKubeAuthProxyDeploymentHasTLSArgs(minVersionArg, cipherSuitesArg string) {
	tc.g.Eventually(func(g Gomega) {
		deployment := &appsv1.Deployment{}
		g.Expect(tc.Client().Get(tc.Context(), types.NamespacedName{
			Name:      kubeAuthProxyName,
			Namespace: gatewayNamespace,
		}, deployment)).To(Succeed())

		g.Expect(deployment.Spec.Template.Spec.Containers).NotTo(BeEmpty(), "Deployment should have at least one container")
		args := deployment.Spec.Template.Spec.Containers[0].Args
		g.Expect(args).To(And(
			ContainElement(minVersionArg),
			ContainElement(cipherSuitesArg),
		))
	}).WithTimeout(tc.TestTimeouts.defaultEventuallyTimeout).
		WithPolling(tc.TestTimeouts.defaultEventuallyPollInterval).
		Should(Succeed())
}

// ValidateKubeAuthProxyTLSArgsMatchAPIServer verifies kube-auth-proxy TLS flags match the cluster APIServer tlsSecurityProfile.
func (tc *GatewayTestCtx) ValidateKubeAuthProxyTLSArgsMatchAPIServer(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)
	t.Log("Validating kube-auth-proxy TLS args match cluster APIServer tlsSecurityProfile")

	minArg, cipherArg := tc.expectedKubeAuthProxyTLSDeploymentArgs(t)
	tc.eventuallyKubeAuthProxyDeploymentHasTLSArgs(minArg, cipherArg)
	tc.EnsureDeploymentReady(types.NamespacedName{Name: kubeAuthProxyName, Namespace: gatewayNamespace}, 2)
}

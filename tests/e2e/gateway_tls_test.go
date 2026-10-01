package e2e_test

import (
	"fmt"
	"strconv"
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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

// kubeAuthProxyTLSDeploymentArgs is an independent test oracle: it computes the
// expected --tls-min-version, --tls-cipher-suite and --tls-curve-preferences
// deployment args for a given TLS security profile without calling any
// production controller functions (KubeAuthProxyTLSFromProfile,
// TLSCipherSuitesFromProfileSpec, etc.). The curve preferences arg is empty
// when the profile has no supported groups, in which case the flag is omitted
// from the deployment.
func kubeAuthProxyTLSDeploymentArgs(profile *configv1.TLSSecurityProfile) (string, string, string) {
	spec := oracleProfileSpec(profile)
	return fmt.Sprintf("--tls-min-version=%s", oracleMinVersion(spec.MinTLSVersion)),
		fmt.Sprintf("--tls-cipher-suite=%s", oracleCipherSuites(spec.Ciphers)),
		oracleCurvePreferences(spec.Groups)
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

// oracleCurvePreferences maps profile groups to numeric Go crypto/tls CurveID
// values via the library-go utility. Returns an empty string when no group is
// supported, matching the flag being omitted in the deployment.
func oracleCurvePreferences(groups []configv1.TLSGroup) string {
	curves, _ := ocpcrypto.TLSGroupsToCurveIDs(groups)
	if len(curves) == 0 {
		return ""
	}
	ids := make([]string, 0, len(curves))
	for _, id := range curves {
		ids = append(ids, strconv.FormatInt(int64(id), 10))
	}
	return fmt.Sprintf("--tls-curve-preferences=%s", strings.Join(ids, ","))
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

// kubeAuthProxyCurvePreferencesMatcher returns the jq matcher that asserts the
// --tls-curve-preferences arg: a presence match when the profile resolves to
// supported groups, an absence match otherwise.
func kubeAuthProxyCurvePreferencesMatcher(curvePreferencesArg string) *jq.Matcher {
	if curvePreferencesArg == "" {
		return jq.Match(`.spec.template.spec.containers[0].args | map(select(startswith("--tls-curve-preferences="))) | length == 0`)
	}
	return jq.Match(`.spec.template.spec.containers[0].args | any(. == "%s")`, curvePreferencesArg)
}

func (tc *GatewayTestCtx) expectedKubeAuthProxyTLSDeploymentArgs(t *testing.T) (string, string, string) {
	t.Helper()

	apiServer, found := tc.fetchClusterAPIServer(t)
	if !found {
		return kubeAuthProxyTLSDeploymentArgs(nil)
	}
	if !oracleShouldHonorClusterTLSProfile(apiServer.Spec.TLSAdherence) {
		return kubeAuthProxyTLSDeploymentArgs(nil)
	}
	return kubeAuthProxyTLSDeploymentArgs(apiServer.Spec.TLSSecurityProfile)
}

func oracleShouldHonorClusterTLSProfile(adherence configv1.TLSAdherencePolicy) bool {
	switch adherence {
	case configv1.TLSAdherencePolicyNoOpinion, configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly:
		return false
	case configv1.TLSAdherencePolicyStrictAllComponents:
		return true
	default:
		return true
	}
}

func (tc *GatewayTestCtx) eventuallyKubeAuthProxyDeploymentHasTLSArgs(minVersionArg, cipherSuitesArg, curvePreferencesArg string) {
	tc.g.Eventually(func(g Gomega) {
		deployment := &appsv1.Deployment{}
		g.Expect(tc.Client().Get(tc.Context(), types.NamespacedName{
			Name:      kubeAuthProxyName,
			Namespace: tc.gatewayNamespace(),
		}, deployment)).To(Succeed())

		g.Expect(deployment.Spec.Template.Spec.Containers).NotTo(BeEmpty(), "Deployment should have at least one container")
		args := deployment.Spec.Template.Spec.Containers[0].Args
		g.Expect(args).To(And(
			ContainElement(minVersionArg),
			ContainElement(cipherSuitesArg),
		))
		if curvePreferencesArg != "" {
			g.Expect(args).To(ContainElement(curvePreferencesArg))
		} else {
			for _, a := range args {
				g.Expect(strings.HasPrefix(a, "--tls-curve-preferences=")).
					To(BeFalse(), "unexpected curve preferences flag %q", a)
			}
		}
	}).WithTimeout(tc.TestTimeouts.defaultEventuallyTimeout).
		WithPolling(tc.TestTimeouts.defaultEventuallyPollInterval).
		Should(Succeed())
}

// ValidateKubeAuthProxyTLSArgsMatchAPIServer verifies kube-auth-proxy TLS flags match the cluster APIServer tlsSecurityProfile.
func (tc *GatewayTestCtx) ValidateKubeAuthProxyTLSArgsMatchAPIServer(t *testing.T) {
	t.Helper()
	tc.SkipIfXKSCluster(t)
	skipUnless(t, Tier1)
	t.Log("Validating kube-auth-proxy TLS args match cluster APIServer tlsSecurityProfile")

	minArg, cipherArg, curveArg := tc.expectedKubeAuthProxyTLSDeploymentArgs(t)
	tc.eventuallyKubeAuthProxyDeploymentHasTLSArgs(minArg, cipherArg, curveArg)
	tc.EnsureDeploymentReady(types.NamespacedName{Name: kubeAuthProxyName, Namespace: tc.gatewayNamespace()}, 2)
}

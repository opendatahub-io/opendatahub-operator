package e2e_test

// RHOAIENG-101897: pre-upgrade capture of the platform default-ingress auth
// stack, ported from the Python reference suite (opendatahub-tests,
// tests/workbenches/notebooks_server/controller/upgrade/). The five PreUpgrade
// tests assert the auth stack is healthy before the cluster upgrade and
// capture its auth-relevant state into the baseline ConfigMap consumed by the
// PostUpgrade tests: gatewayconfig.<field> entries for the default-gateway
// GatewayConfig, gateway.identity and gateway.uid for the data-science-gateway
// Gateway, oauthclient.* for the data-science OAuthClient (including its
// grantMethod), credssecret.<key>Digest entries for the kube-auth-proxy-creds
// Secret and kubeauthproxy.* entries for the kube-auth-proxy Deployment
// (including its UID, so a delete-and-recreate during the upgrade is
// detectable). Raw secret values never enter the baseline
// or the logs (CWE-532): the OAuthClient secret and every credentials-Secret
// value are stored as SHA-256 digests, and container args carrying
// secret-like flags are refused outright by argDigestEntries.
//
// Baseline assembly design: Go runs the tests of one binary sequentially in
// source order, so each test asserts its subject and records its entries into
// the package-level platformAuthBaseline map, and the final test
// (TestPreUpgradeKubeAuthProxyAvailable) persists the complete map exactly
// once via saveUpgradeBaseline. This mirrors the Python reference, whose
// session-scoped capture_platform_auth_baseline fixture collected the
// per-resource state into one ConfigMap; package-level state is acceptable
// here for the same reason. No test calls t.Parallel, so the map needs no
// synchronization. Before persisting, the final test enforces completeness
// (requireCompletePlatformAuthBaseline): every static schema key must be
// present — except the oauthclient.* keys on the OIDC skip path — so a
// failed earlier test or a -run filter cannot save a partial baseline that
// the PostUpgrade phase would silently accept. The PreUpgrade phase is
// meaningful only when run as a whole, one count:
//
//	E2E_TEST_TAG=PreUpgrade go test ./tests/e2e/ -run 'TestPreUpgrade' -v -count=1

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	oauthv1 "github.com/openshift/api/oauth/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"

	. "github.com/onsi/gomega"
)

// Platform-auth stack resource names, aliased from the operator's exported
// constants so the tests and the reconciler share a single source of truth.
// The kube-auth-proxy Deployment's container is named after the proxy itself
// (see the operator's kube-auth-proxy deployment templates), so
// gateway.KubeAuthProxyName pins both the Deployment and the container name.
const (
	// upgradeAuthNamespace hosts the platform default-auth stack.
	upgradeAuthNamespace = gateway.GatewayNamespace
	// upgradeGatewayConfigName is the GatewayConfig singleton's name.
	upgradeGatewayConfigName = serviceApi.GatewayConfigName
	// upgradeDefaultGatewayName is the default Gateway's name.
	upgradeDefaultGatewayName = gateway.DefaultGatewayName
	// upgradeOAuthClientName is the platform OAuthClient's name.
	upgradeOAuthClientName = gateway.AuthClientID
	// upgradeCredentialsSecretName is the kube-auth-proxy credentials Secret's name.
	upgradeCredentialsSecretName = gateway.KubeAuthProxySecretsName
	// upgradeAuthProxyName is the kube-auth-proxy Deployment (and container) name.
	upgradeAuthProxyName = gateway.KubeAuthProxyName
)

// upgradeCredentialsSecretKeys are the credentials Secret's expected keys.
var upgradeCredentialsSecretKeys = []string{
	gateway.EnvClientID,
	gateway.EnvClientSecret,
	gateway.EnvCookieSecret,
}

// Baseline entry keys (the schema ported from the Python reference suite's
// platform_auth_baseline fixture; Task 5's PostUpgrade tests compare against
// these exact keys, so they must not drift).
const (
	// baselineKeyPrefixGatewayConfig prefixes per-field GatewayConfig entries.
	baselineKeyPrefixGatewayConfig = "gatewayconfig."
	// baselineKeyGatewayIdentity holds the default Gateway's routing identity.
	baselineKeyGatewayIdentity = "gateway.identity"
	// baselineKeyGatewayUID holds the default Gateway's metadata UID, so the
	// PostUpgrade phase can detect a delete-and-recreate during the upgrade.
	baselineKeyGatewayUID = "gateway.uid"
	// baselineKeyOAuthRedirectURIs holds the sorted, comma-joined redirect URIs.
	baselineKeyOAuthRedirectURIs = "oauthclient.redirectURIs"
	// baselineKeyOAuthSecretDigest holds the SHA-256 digest of the OAuthClient secret.
	baselineKeyOAuthSecretDigest = "oauthclient.secretDigest"
	// baselineKeyOAuthWWWChallenge holds the respondWithChallenges flag.
	baselineKeyOAuthWWWChallenge = "oauthclient.wwwChallenge"
	// baselineKeyOAuthTokenMaxAge holds the accessTokenMaxAgeSeconds override.
	baselineKeyOAuthTokenMaxAge = "oauthclient.tokenMaxAge"
	// baselineKeyOAuthGrantMethod holds the OAuthClient's grantMethod, so the
	// PostUpgrade phase can detect a registration change during the upgrade.
	baselineKeyOAuthGrantMethod = "oauthclient.grantMethod"
	// baselineKeyPrefixCredsSecret prefixes per-key credentials-Secret digests.
	baselineKeyPrefixCredsSecret = "credssecret."
	// baselineKeyPrefixAuthProxyArgs prefixes per-flag kube-auth-proxy arg digests.
	baselineKeyPrefixAuthProxyArgs = "kubeauthproxy.args."
	// baselineKeyAuthProxyImage holds the kube-auth-proxy container image.
	baselineKeyAuthProxyImage = "kubeauthproxy.image"
	// baselineKeyAuthProxyUID holds the kube-auth-proxy Deployment's metadata
	// UID, so the PostUpgrade phase can detect a delete-and-recreate during
	// the upgrade.
	baselineKeyAuthProxyUID = "kubeauthproxy.uid"

	// baselineUnset marks an absent optional field, so an explicitly-set zero
	// value stays distinguishable from a field an upgrade dropped entirely.
	baselineUnset = "unset"
)

// platformAuthBaseline accumulates the entries recorded by the PreUpgrade
// tests; see the file comment for the assembly design.
var platformAuthBaseline = map[string]string{}

// platformAuthOIDCSkipped records that the OAuthClient test took the OIDC
// skip path: the oauthclient.* entries are then legitimately absent, so the
// completeness guard must not demand them.
var platformAuthOIDCSkipped bool

// recordPlatformAuthBaseline merges entries into the accumulated baseline,
// failing on a duplicate key: a collision would mean two subjects claim the
// same baseline entry, which would silently drop one of them from the map.
func recordPlatformAuthBaseline(t *testing.T, entries map[string]string) {
	t.Helper()
	for key, value := range entries {
		_, exists := platformAuthBaseline[key]
		require.False(t, exists, "duplicate platform-auth baseline entry key %q", key)
		platformAuthBaseline[key] = value
	}
}

// expectedPlatformAuthBaselineKeys returns the static schema keys a complete
// baseline must carry. The per-subject key sets are derived from the entry
// builders themselves — invoked on zero objects, purely for their key sets,
// no cluster access — so a key added to a builder is enforced here
// automatically; only the composite identity, UID and image keys are listed
// explicitly. The dynamic kubeauthproxy.args.* entries are checked separately
// by count in requireCompletePlatformAuthBaseline.
func expectedPlatformAuthBaselineKeys() ([]string, error) {
	gatewayConfigEntries, err := gatewayConfigBaselineEntries(&serviceApi.GatewayConfig{})
	if err != nil {
		return nil, fmt.Errorf("derive GatewayConfig baseline keys: %w", err)
	}
	credentialsEntries := credentialsSecretBaselineEntries(&corev1.Secret{})
	oauthClientEntries := oauthClientBaselineEntries(&oauthv1.OAuthClient{})

	expected := make([]string, 0,
		len(gatewayConfigEntries)+len(credentialsEntries)+len(oauthClientEntries)+4)
	for key := range gatewayConfigEntries {
		expected = append(expected, key)
	}
	for key := range credentialsEntries {
		expected = append(expected, key)
	}
	if !platformAuthOIDCSkipped {
		for key := range oauthClientEntries {
			expected = append(expected, key)
		}
	}
	return append(expected,
		baselineKeyGatewayIdentity,
		baselineKeyGatewayUID,
		baselineKeyAuthProxyImage,
		baselineKeyAuthProxyUID,
	), nil
}

// requireCompletePlatformAuthBaseline refuses to persist a partial baseline:
// every static schema key must be present (except the oauthclient.* keys when
// the OAuthClient test took the OIDC skip) and at least one
// kubeauthproxy.args.* entry must exist. Without this guard, a failed earlier
// test — Go keeps running the remaining tests after a require failure — or a
// -run filter selecting only the final test would save a partial map whose
// missing entries Task 5's PostUpgrade iteration would silently skip,
// producing a misleading PASS on a broken baseline. Mirrors the Python
// authority's "refusing to capture a broken baseline". The failure names
// missing keys only, never values.
func requireCompletePlatformAuthBaseline(t *testing.T, entries map[string]string) {
	t.Helper()

	expected, err := expectedPlatformAuthBaselineKeys()
	require.NoError(t, err, "should derive the expected platform-auth baseline key set")

	missing := make([]string, 0)
	for _, key := range expected {
		if _, exists := entries[key]; !exists {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		require.Empty(t, missing,
			"refusing to capture an incomplete platform-auth baseline: missing keys %v "+
				"(an earlier PreUpgrade test failed or was filtered out; re-run the full "+
				"PreUpgrade phase before the upgrade)", missing)
	}

	argEntries := 0
	for key := range entries {
		if strings.HasPrefix(key, baselineKeyPrefixAuthProxyArgs) {
			argEntries++
		}
	}
	require.NotZero(t, argEntries,
		"refusing to capture an incomplete platform-auth baseline: no %s* entries were captured",
		baselineKeyPrefixAuthProxyArgs)
}

// formatOptionalBool renders an optional bool ("unset" when nil).
func formatOptionalBool(value *bool) string {
	if value == nil {
		return baselineUnset
	}
	return strconv.FormatBool(*value)
}

// formatOptionalInt32 renders an optional int32 ("unset" when nil).
func formatOptionalInt32(value *int32) string {
	if value == nil {
		return baselineUnset
	}
	return strconv.Itoa(int(*value))
}

// marshalOptionalJSON renders an optional struct as deterministic JSON
// ("unset" when nil). encoding/json emits struct fields in declaration order,
// so the same API type always renders to the same string. The GatewayConfig's
// oidc and tokenReview stanzas carry only configuration and secret
// references (names and keys), never secret values.
func marshalOptionalJSON[T any](value *T) (string, error) {
	if value == nil {
		return baselineUnset, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal baseline entry: %w", err)
	}
	return string(encoded), nil
}

// gatewayConfigBaselineEntries renders the auth-relevant GatewayConfig spec
// fields as baseline entries: the OIDC provider configuration, session cookie
// lifetimes, auth-proxy timeout and capacity, provider certificate
// verification, service-account token validation and its TokenReview tuning.
// Non-auth fields (domain, certificate, ingress exposure, additional
// ingresses) are deliberately not captured. The deprecated authTimeout field
// is not captured either; it is an alias of authProxyTimeout.
func gatewayConfigBaselineEntries(config *serviceApi.GatewayConfig) (map[string]string, error) {
	oidc, err := marshalOptionalJSON(config.Spec.OIDC)
	if err != nil {
		return nil, fmt.Errorf("serialize GatewayConfig oidc: %w", err)
	}
	tokenReview, err := marshalOptionalJSON(config.Spec.TokenReview)
	if err != nil {
		return nil, fmt.Errorf("serialize GatewayConfig tokenReview: %w", err)
	}

	return map[string]string{
		baselineKeyPrefixGatewayConfig + "oidc":                      oidc,
		baselineKeyPrefixGatewayConfig + "cookieExpire":              config.Spec.Cookie.Expire.Duration.String(),
		baselineKeyPrefixGatewayConfig + "cookieRefresh":             config.Spec.Cookie.Refresh.Duration.String(),
		baselineKeyPrefixGatewayConfig + "authProxyTimeout":          config.Spec.AuthProxyTimeout.Duration.String(),
		baselineKeyPrefixGatewayConfig + "authProxyMaxReplicas":      formatOptionalInt32(config.Spec.AuthProxyMaxReplicas),
		baselineKeyPrefixGatewayConfig + "providerCASecretName":      config.Spec.ProviderCASecretName,
		baselineKeyPrefixGatewayConfig + "verifyProviderCertificate": formatOptionalBool(config.Spec.VerifyProviderCertificate),
		baselineKeyPrefixGatewayConfig + "enableK8sTokenValidation":  formatOptionalBool(config.Spec.EnableK8sTokenValidation),
		baselineKeyPrefixGatewayConfig + "tokenReview":               tokenReview,
	}, nil
}

// gatewayIdentity renders the default Gateway's stable routing identity:
// gateway class plus listener names and ports, sorted by listener name so a
// benign listener reordering still compares equal. The PostUpgrade test
// re-renders the live Gateway and compares the strings.
func gatewayIdentity(defaultGateway *gwapiv1.Gateway) string {
	listeners := make([]string, 0, len(defaultGateway.Spec.Listeners))
	for _, listener := range defaultGateway.Spec.Listeners {
		listeners = append(listeners, fmt.Sprintf("%s:%d", listener.Name, listener.Port))
	}
	slices.Sort(listeners)
	return fmt.Sprintf("gatewayClass=%s;listeners=%s",
		defaultGateway.Spec.GatewayClassName, strings.Join(listeners, ","))
}

// oauthClientBaselineEntries renders the OAuthClient registration as baseline
// entries. Redirect URIs are public URLs, sorted and comma-joined for an
// order-insensitive comparison. The client secret is the credential
// kube-auth-proxy presents to the cluster OAuth server; only its SHA-256
// digest enters the baseline, never the value (CWE-532). wwwChallenge maps to
// the API's respondWithChallenges bool and tokenMaxAge to its optional
// accessTokenMaxAgeSeconds override. grantMethod is captured verbatim so the
// PostUpgrade phase can detect a registration change during the upgrade.
func oauthClientBaselineEntries(oauthClient *oauthv1.OAuthClient) map[string]string {
	redirectURIs := slices.Clone(oauthClient.RedirectURIs)
	slices.Sort(redirectURIs)

	return map[string]string{
		baselineKeyOAuthRedirectURIs: strings.Join(redirectURIs, ","),
		baselineKeyOAuthSecretDigest: sha256Hex(oauthClient.Secret),
		baselineKeyOAuthWWWChallenge: strconv.FormatBool(oauthClient.RespondWithChallenges),
		baselineKeyOAuthTokenMaxAge:  formatOptionalInt32(oauthClient.AccessTokenMaxAgeSeconds),
		baselineKeyOAuthGrantMethod:  string(oauthClient.GrantMethod),
	}
}

// credentialsSecretBaselineEntries renders the credentials Secret's values as
// per-key SHA-256 digests. The caller must ensure the secret was fetched and
// carries the expected keys; only digests are returned, never values.
func credentialsSecretBaselineEntries(credentialsSecret *corev1.Secret) map[string]string {
	entries := make(map[string]string, len(upgradeCredentialsSecretKeys))
	for _, key := range upgradeCredentialsSecretKeys {
		entries[baselineKeyPrefixCredsSecret+key+"Digest"] = sha256Hex(string(credentialsSecret.Data[key]))
	}
	return entries
}

// kubeAuthProxyBaselineEntries renders the kube-auth-proxy Deployment's
// identity, container args and image as baseline entries: the Deployment's
// metadata UID (so a delete-and-recreate during the upgrade is detectable),
// the container's args as flag-keyed value digests via argDigestEntries —
// which refuses secret-like flags outright — and the image reference as-is,
// so digest-pinned references keep their digest and tag references keep
// their tag.
func kubeAuthProxyBaselineEntries(deployment *appsv1.Deployment) (map[string]string, error) {
	container := authProxyContainer(deployment)
	if container == nil {
		return nil, fmt.Errorf("deployment %s/%s has no %s container", deployment.Namespace, deployment.Name, upgradeAuthProxyName)
	}

	argEntries, err := argDigestEntries(container.Args)
	if err != nil {
		return nil, fmt.Errorf("kube-auth-proxy container %s: %w", container.Name, err)
	}

	entries := make(map[string]string, len(argEntries)+2)
	for flag, digest := range argEntries {
		entries[baselineKeyPrefixAuthProxyArgs+flag] = digest
	}
	entries[baselineKeyAuthProxyImage] = container.Image
	entries[baselineKeyAuthProxyUID] = string(deployment.UID)
	return entries, nil
}

// gatewayConfigReady reports whether the GatewayConfig's aggregate Ready
// condition is True ("Ready" is the top-level condition type; the
// service-specific GatewayConfigReady condition is folded into it).
func gatewayConfigReady(config *serviceApi.GatewayConfig) bool {
	for _, condition := range config.GetConditions() {
		if condition.Type == "Ready" {
			return condition.Status == metav1.ConditionTrue
		}
	}
	return false
}

// gatewayAcceptedAndProgrammed reports whether the Gateway reports both
// Gateway API v1 readiness conditions True. Gateway API has no literal Ready
// condition; Accepted plus Programmed is the ready state the suite's other
// Gateway specs assert.
func gatewayAcceptedAndProgrammed(defaultGateway *gwapiv1.Gateway) bool {
	accepted, programmed := false, false
	for _, condition := range defaultGateway.Status.Conditions {
		switch condition.Type {
		case string(gwapiv1.GatewayConditionAccepted):
			accepted = condition.Status == metav1.ConditionTrue
		case string(gwapiv1.GatewayConditionProgrammed):
			programmed = condition.Status == metav1.ConditionTrue
		}
	}
	return accepted && programmed
}

// deploymentAvailable reports whether the Deployment's Available condition is True.
func deploymentAvailable(deployment *appsv1.Deployment) bool {
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// authProxyContainer returns the kube-auth-proxy container of the Deployment,
// or nil when absent. The container is named after the proxy itself; see the
// upgradeAuthProxyName comment.
func authProxyContainer(deployment *appsv1.Deployment) *corev1.Container {
	for index := range deployment.Spec.Template.Spec.Containers {
		if deployment.Spec.Template.Spec.Containers[index].Name == upgradeAuthProxyName {
			return &deployment.Spec.Template.Spec.Containers[index]
		}
	}
	return nil
}

// clusterAuthUsesOIDC reports whether the platform auth stack runs in
// external-OIDC mode: the kube-auth-proxy container carries the
// --provider=oidc arg. It backs the OAuthClient test's OIDC skip; on
// integrated-OAuth clusters the proxy runs --provider=openshift and the
// OAuthClient must exist. Probe failures are logged and treated as
// not-OIDC — the safe direction: the caller's OAuthClient Get then fails
// loudly instead of silently passing.
func clusterAuthUsesOIDC(t *testing.T, ctx context.Context, c client.Client) bool {
	t.Helper()

	deployment := &appsv1.Deployment{}
	if err := c.Get(ctx, types.NamespacedName{Name: upgradeAuthProxyName, Namespace: upgradeAuthNamespace}, deployment); err != nil {
		t.Logf("OIDC probe: get Deployment %s/%s failed: %v", upgradeAuthNamespace, upgradeAuthProxyName, err)
		return false
	}
	container := authProxyContainer(deployment)
	if container == nil {
		t.Logf("OIDC probe: Deployment %s/%s has no %s container", upgradeAuthNamespace, upgradeAuthProxyName, upgradeAuthProxyName)
		return false
	}
	return slices.Contains(container.Args, "--provider=oidc")
}

// TestPreUpgradeGatewayConfigExists verifies that the GatewayConfig driving
// the platform default-ingress auth stack exists and is Ready before the
// upgrade, and captures its auth-relevant spec fields into the baseline.
func TestPreUpgradeGatewayConfigExists(t *testing.T) {
	skipUnlessUpgradePhase(t, PreUpgrade)

	tc, err := NewTestContext(t)
	require.NoError(t, err)
	ctx := tc.Context()

	g := NewWithT(t)
	g.Eventually(func(g Gomega) {
		gatewayConfig := &serviceApi.GatewayConfig{}
		g.Expect(tc.Client().Get(ctx, types.NamespacedName{Name: upgradeGatewayConfigName}, gatewayConfig)).To(Succeed(),
			"GatewayConfig %s should exist before the upgrade", upgradeGatewayConfigName)
		g.Expect(gatewayConfigReady(gatewayConfig)).To(BeTrue(),
			"GatewayConfig %s should be Ready before the upgrade", upgradeGatewayConfigName)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())

	// Capture from a fresh read after readiness settled.
	gatewayConfig := &serviceApi.GatewayConfig{}
	require.NoError(t, tc.Client().Get(ctx, types.NamespacedName{Name: upgradeGatewayConfigName}, gatewayConfig),
		"GatewayConfig %s should still exist after the readiness check", upgradeGatewayConfigName)

	entries, err := gatewayConfigBaselineEntries(gatewayConfig)
	require.NoError(t, err)
	recordPlatformAuthBaseline(t, entries)
}

// TestPreUpgradeDefaultGatewayExists verifies that the default Gateway exists
// with a stable identity and is ready to route traffic before the upgrade,
// and captures its routing identity (gateway class, listener names and ports)
// and its UID into the baseline, so the PostUpgrade phase can detect a
// delete-and-recreate during the upgrade.
func TestPreUpgradeDefaultGatewayExists(t *testing.T) {
	skipUnlessUpgradePhase(t, PreUpgrade)

	tc, err := NewTestContext(t)
	require.NoError(t, err)
	ctx := tc.Context()

	defaultGateway := &gwapiv1.Gateway{}
	gatewayKey := types.NamespacedName{Name: upgradeDefaultGatewayName, Namespace: upgradeAuthNamespace}
	require.NoError(t, tc.Client().Get(ctx, gatewayKey, defaultGateway),
		"Gateway %s/%s should exist before the upgrade", upgradeAuthNamespace, upgradeDefaultGatewayName)
	require.NotEmpty(t, defaultGateway.UID,
		"Gateway %s/%s should have a UID before the upgrade", upgradeAuthNamespace, upgradeDefaultGatewayName)
	require.True(t, gatewayAcceptedAndProgrammed(defaultGateway),
		"Gateway %s/%s should be Accepted and Programmed before the upgrade", upgradeAuthNamespace, upgradeDefaultGatewayName)

	recordPlatformAuthBaseline(t, map[string]string{
		baselineKeyGatewayIdentity: gatewayIdentity(defaultGateway),
		baselineKeyGatewayUID:      string(defaultGateway.UID),
	})
}

// TestPreUpgradeOAuthClientExists verifies that the platform OAuthClient
// registration exists with a non-empty secret, a non-empty grant method and
// the oauth2 callback redirect before the upgrade, and captures its
// registration entries (including grantMethod) into the baseline. On BYOIDC
// clusters (no OAuthClient and kube-auth-proxy running --provider=oidc) the
// test skips with an explanatory message: there is no platform OAuthClient to
// capture, and a silent pass would hide the skip from pipeline readers.
func TestPreUpgradeOAuthClientExists(t *testing.T) {
	skipUnlessUpgradePhase(t, PreUpgrade)

	tc, err := NewTestContext(t)
	require.NoError(t, err)
	ctx := tc.Context()

	oauthClient := &oauthv1.OAuthClient{}
	err = tc.Client().Get(ctx, types.NamespacedName{Name: upgradeOAuthClientName}, oauthClient)
	if k8serr.IsNotFound(err) && clusterAuthUsesOIDC(t, ctx, tc.Client()) {
		// Record the legitimate absence so the final test's completeness
		// guard does not demand oauthclient.* entries from a cluster that
		// has no OAuthClient to read.
		platformAuthOIDCSkipped = true
		t.Skipf(
			"OAuthClient %s not found and kube-auth-proxy runs --provider=oidc: "+
				"this cluster authenticates via an external OIDC provider instead of the "+
				"integrated OpenShift OAuth server, so there is no platform OAuthClient to "+
				"capture before the upgrade (expected skip on BYOIDC clusters; the "+
				"PostUpgrade OAuthClient test skips the same way)",
			upgradeOAuthClientName,
		)
	}
	require.NoError(t, err, "OAuthClient %s should exist before the upgrade", upgradeOAuthClientName)

	// The secret is asserted non-empty but never rendered: only its digest
	// enters the baseline (CWE-532).
	require.NotEmpty(t, oauthClient.Secret,
		"OAuthClient %s should have a non-empty secret before the upgrade", upgradeOAuthClientName)
	require.NotEmpty(t, oauthClient.RedirectURIs,
		"OAuthClient %s should have redirect URIs before the upgrade", upgradeOAuthClientName)
	for _, uri := range oauthClient.RedirectURIs {
		require.True(t, strings.HasSuffix(uri, gateway.OAuthCallbackPath),
			"OAuthClient %s redirect URIs should end with the oauth2 callback path %s (got %s)",
			upgradeOAuthClientName, gateway.OAuthCallbackPath, uri)
	}
	require.NotEmpty(t, oauthClient.GrantMethod,
		"OAuthClient %s should have a non-empty grantMethod before the upgrade", upgradeOAuthClientName)

	recordPlatformAuthBaseline(t, oauthClientBaselineEntries(oauthClient))
}

// TestPreUpgradeCredentialsSecretExists verifies that the kube-auth-proxy
// credentials Secret exists with all three keys (client ID, client secret,
// cookie secret) before the upgrade, and captures the per-key value digests
// into the baseline.
func TestPreUpgradeCredentialsSecretExists(t *testing.T) {
	skipUnlessUpgradePhase(t, PreUpgrade)

	tc, err := NewTestContext(t)
	require.NoError(t, err)
	ctx := tc.Context()

	credentialsSecret := &corev1.Secret{}
	secretKey := types.NamespacedName{Name: upgradeCredentialsSecretName, Namespace: upgradeAuthNamespace}
	require.NoError(t, tc.Client().Get(ctx, secretKey, credentialsSecret),
		"credentials Secret %s/%s should exist before the upgrade", upgradeAuthNamespace, upgradeCredentialsSecretName)

	// secretDataDigests panics on a nil secret, so the digest step relies on
	// this invariant. The check is defense in depth: the pointer cannot
	// actually be nil here (a Go zero value is non-nil and a typed Get cannot
	// nil it), but the explicit assertion documents the invariant and fails
	// loudly should a refactor ever break it.
	require.NotNil(t, credentialsSecret)

	// Key names and emptiness only: values are compared as digests and must
	// never reach a failure message (CWE-532).
	for _, key := range upgradeCredentialsSecretKeys {
		require.Contains(t, credentialsSecret.Data, key,
			"credentials Secret %s/%s should carry key %s", upgradeAuthNamespace, upgradeCredentialsSecretName, key)
		require.NotEmpty(t, credentialsSecret.Data[key],
			"credentials Secret %s/%s key %s should be non-empty", upgradeAuthNamespace, upgradeCredentialsSecretName, key)
	}

	recordPlatformAuthBaseline(t, credentialsSecretBaselineEntries(credentialsSecret))
}

// TestPreUpgradeKubeAuthProxyAvailable verifies that the kube-auth-proxy
// Deployment is Available with all desired replicas ready before the upgrade,
// captures its identity (UID), container arg digests and image into the
// baseline, and — as the final PreUpgrade test — persists the complete
// accumulated baseline via saveUpgradeBaseline (see the file comment for the
// assembly design).
func TestPreUpgradeKubeAuthProxyAvailable(t *testing.T) {
	skipUnlessUpgradePhase(t, PreUpgrade)

	tc, err := NewTestContext(t)
	require.NoError(t, err)
	ctx := tc.Context()

	deploymentKey := types.NamespacedName{Name: upgradeAuthProxyName, Namespace: upgradeAuthNamespace}
	g := NewWithT(t)
	g.Eventually(func(g Gomega) {
		deployment := &appsv1.Deployment{}
		g.Expect(tc.Client().Get(ctx, deploymentKey, deployment)).To(Succeed(),
			"Deployment %s/%s should exist before the upgrade", upgradeAuthNamespace, upgradeAuthProxyName)
		g.Expect(deploymentAvailable(deployment)).To(BeTrue(),
			"Deployment %s/%s should be Available before the upgrade", upgradeAuthNamespace, upgradeAuthProxyName)
		desiredReplicas := int32(1)
		if deployment.Spec.Replicas != nil {
			desiredReplicas = *deployment.Spec.Replicas
		}
		g.Expect(deployment.Status.ReadyReplicas).To(Equal(desiredReplicas),
			"Deployment %s/%s should have all %d replicas ready before the upgrade",
			upgradeAuthNamespace, upgradeAuthProxyName, desiredReplicas)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())

	// Capture from a fresh read after readiness settled.
	deployment := &appsv1.Deployment{}
	require.NoError(t, tc.Client().Get(ctx, deploymentKey, deployment),
		"Deployment %s/%s should still exist after the readiness check", upgradeAuthNamespace, upgradeAuthProxyName)
	require.NotEmpty(t, deployment.UID,
		"Deployment %s/%s should have a UID before the upgrade", upgradeAuthNamespace, upgradeAuthProxyName)

	// argDigestEntries refuses secret-like args; a live cluster legitimately
	// carrying one must fail here rather than leak into the baseline.
	entries, err := kubeAuthProxyBaselineEntries(deployment)
	require.NoError(t, err,
		"should capture kube-auth-proxy baseline entries; secret-like args are refused outright")
	recordPlatformAuthBaseline(t, entries)

	// Completeness guard: refuse to persist a partial baseline. An earlier
	// failed test or a -run filter would otherwise leave holes that Task 5's
	// PostUpgrade key iteration would silently skip.
	requireCompletePlatformAuthBaseline(t, platformAuthBaseline)

	// Final assembly: persist the complete baseline exactly once.
	require.NoError(t, saveUpgradeBaseline(ctx, tc.Client(), platformAuthBaseline),
		"should save the platform-auth baseline ConfigMap %s/%s", upgradeBaselineNamespace, upgradeBaselineCMName)
	t.Logf("captured %d platform-auth baseline entries into ConfigMap %s/%s",
		len(platformAuthBaseline), upgradeBaselineNamespace, upgradeBaselineCMName)
}

// PostUpgrade phase: the seven preservation tests, ported from the Python
// reference suite's TestPostUpgradePlatformAuth. Each loads the baseline
// ConfigMap captured by the PreUpgrade phase and compares the live auth
// stack against it. Comparison semantics deliberately differ from the
// Python "image equal" wording in one place: a real upgrade legitimately
// advances the image digest or tag, so the image is compared
// repository-to-repository (imageRepository below), exactly as the Python
// authority's _image_repository helper does. The Gateway, OAuthClient
// grantMethod and kube-auth-proxy Deployment UIDs are compared too, so a
// delete-and-recreate during the upgrade fails loudly, and the
// kube-auth-proxy args are compared as an exact map (both directions): a
// flag added post-upgrade fails just like a removed one. Failure messages
// name resources, flags and baseline keys; digests may appear in diffs, raw
// secret values never.

// requirePostUpgradeBaseline loads the baseline ConfigMap captured by the
// PreUpgrade phase, surfacing loadUpgradeBaseline's distinct "no baseline
// found — was the PreUpgrade phase run" error as a hard failure: a
// PostUpgrade run without its PreUpgrade counterpart must fail loudly,
// never pass or skip.
func requirePostUpgradeBaseline(t *testing.T, ctx context.Context, c client.Client) map[string]string {
	t.Helper()
	baseline, err := loadUpgradeBaseline(ctx, c)
	require.NoError(t, err,
		"should load the platform-auth baseline ConfigMap %s/%s captured by the PreUpgrade phase",
		upgradeBaselineNamespace, upgradeBaselineCMName)
	return baseline
}

// requireBaselineEntry returns baseline[key], hard-failing when the entry is
// absent: a PostUpgrade comparison must never silently skip a subject whose
// baseline entry is missing — an incomplete PreUpgrade capture is a failure,
// not a pass.
func requireBaselineEntry(t *testing.T, baseline map[string]string, key string) string {
	t.Helper()
	value, exists := baseline[key]
	require.True(t, exists,
		"baseline entry %q is missing from ConfigMap %s/%s: the PreUpgrade capture is incomplete; "+
			"re-run the full PreUpgrade phase before the PostUpgrade phase",
		key, upgradeBaselineNamespace, upgradeBaselineCMName)
	return value
}

// imageRepository returns the repository component of an image reference,
// stripping any digest (everything after "@") and any tag (a colon after
// the last slash, so registry ports such as registry:5000/repo keep their
// port). Ported from the Python reference's _image_repository: a real
// 3.5.3→3.6.0 upgrade legitimately advances the image digest, so the
// PostUpgrade image comparison is repository-to-repository; the baseline's
// full kubeauthproxy.image reference (digest or tag included) is parsed on
// both sides.
func imageRepository(image string) string {
	repository := strings.SplitN(image, "@", 2)[0]
	if colonIndex := strings.LastIndex(repository, ":"); colonIndex > strings.LastIndex(repository, "/") {
		repository = repository[:colonIndex]
	}
	return repository
}

// assertSettingsSubset recursively asserts that baseline settings survive in
// current, ported from the Python reference's _assert_settings_subset:
// mappings are compared as subsets (fields newly introduced by an upgrade
// are allowed inside existing settings), scalars and lists must match
// exactly. Failure messages name the setting path and the missing keys,
// never values.
func assertSettingsSubset(t *testing.T, resource, settingPath string, baseline, current any) {
	t.Helper()
	baselineMap, baselineIsMap := baseline.(map[string]any)
	if !baselineIsMap {
		require.Equal(t, baseline, current, "%s setting %q changed after the upgrade", resource, settingPath)
		return
	}
	currentMap, currentIsMap := current.(map[string]any)
	require.True(t, currentIsMap,
		"%s setting %q changed shape after the upgrade: expected a mapping", resource, settingPath)

	missing := make([]string, 0)
	for key := range baselineMap {
		if _, exists := currentMap[key]; !exists {
			missing = append(missing, key)
		}
	}
	slices.Sort(missing)
	require.Empty(t, missing, "%s lost settings after the upgrade at %q: %v", resource, settingPath, missing)

	for key, baselineValue := range baselineMap {
		assertSettingsSubset(t, resource, settingPath+"."+key, baselineValue, currentMap[key])
	}
}

// requireJSONSettingPreserved compares a JSON-serialized baseline entry with
// the current rendering under subset semantics: the "unset" markers compare
// directly, and both JSON sides must unmarshal before
// assertSettingsSubset applies the recursive subset comparison (new nested
// fields introduced by the upgrade are allowed, dropped or changed ones are
// not).
func requireJSONSettingPreserved(t *testing.T, resource, key, baselineValue, currentValue string) {
	t.Helper()
	if baselineValue == baselineUnset || currentValue == baselineUnset {
		require.Equal(t, baselineValue, currentValue,
			"%s setting %q changed after the upgrade", resource, key)
		return
	}
	var baselineJSON, currentJSON any
	require.NoError(t, json.Unmarshal([]byte(baselineValue), &baselineJSON),
		"baseline entry %q should be valid JSON or %q", key, baselineUnset)
	require.NoError(t, json.Unmarshal([]byte(currentValue), &currentJSON),
		"%s setting %q should render as valid JSON", resource, key)
	assertSettingsSubset(t, resource, key, baselineJSON, currentJSON)
}

// TestPostUpgradeGatewayConfigSettingsPreserved verifies that every
// auth-relevant GatewayConfig setting captured before the upgrade is
// unchanged after it. Fields newly introduced by the upgraded version are
// allowed, including inside existing nested settings (oidc, tokenReview),
// mirroring the Python reference's subset comparison.
func TestPostUpgradeGatewayConfigSettingsPreserved(t *testing.T) {
	skipUnlessUpgradePhase(t, PostUpgrade)

	tc, err := NewTestContext(t)
	require.NoError(t, err)
	ctx := tc.Context()
	baseline := requirePostUpgradeBaseline(t, ctx, tc.Client())

	gatewayConfig := &serviceApi.GatewayConfig{}
	require.NoError(t, tc.Client().Get(ctx, types.NamespacedName{Name: upgradeGatewayConfigName}, gatewayConfig),
		"GatewayConfig %s should still exist after the upgrade", upgradeGatewayConfigName)

	currentEntries, err := gatewayConfigBaselineEntries(gatewayConfig)
	require.NoError(t, err, "should render the current GatewayConfig baseline entries")

	resource := "GatewayConfig " + upgradeGatewayConfigName
	for key, currentValue := range currentEntries {
		baselineValue := requireBaselineEntry(t, baseline, key)
		switch key {
		case baselineKeyPrefixGatewayConfig + "oidc", baselineKeyPrefixGatewayConfig + "tokenReview":
			requireJSONSettingPreserved(t, resource, key, baselineValue, currentValue)
		default:
			require.Equal(t, baselineValue, currentValue,
				"%s setting %q changed after the upgrade", resource, key)
		}
	}
}

// TestPostUpgradeDefaultGatewayIdentityPreserved verifies that the default
// Gateway kept its routing identity (gateway class, listener names and
// ports) and its UID across the upgrade: a changed UID means the Gateway
// was deleted and recreated, which would drop its status and addresses.
func TestPostUpgradeDefaultGatewayIdentityPreserved(t *testing.T) {
	skipUnlessUpgradePhase(t, PostUpgrade)

	tc, err := NewTestContext(t)
	require.NoError(t, err)
	ctx := tc.Context()
	baseline := requirePostUpgradeBaseline(t, ctx, tc.Client())

	defaultGateway := &gwapiv1.Gateway{}
	gatewayKey := types.NamespacedName{Name: upgradeDefaultGatewayName, Namespace: upgradeAuthNamespace}
	require.NoError(t, tc.Client().Get(ctx, gatewayKey, defaultGateway),
		"Gateway %s/%s should still exist after the upgrade", upgradeAuthNamespace, upgradeDefaultGatewayName)

	require.Equal(t, requireBaselineEntry(t, baseline, baselineKeyGatewayIdentity), gatewayIdentity(defaultGateway),
		"Gateway %s/%s routing identity (gateway class, listeners) changed after the upgrade",
		upgradeAuthNamespace, upgradeDefaultGatewayName)
	require.Equal(t, requireBaselineEntry(t, baseline, baselineKeyGatewayUID), string(defaultGateway.UID),
		"Gateway %s/%s was recreated during the upgrade: its UID changed",
		upgradeAuthNamespace, upgradeDefaultGatewayName)
}

// TestPostUpgradeOAuthClientRegistrationPreserved verifies that the platform
// OAuthClient registration survived the upgrade: grant method, redirect
// URIs, the secret digest, wwwChallenge and tokenMaxAge all equal their
// baseline entries. On BYOIDC clusters (no OAuthClient and kube-auth-proxy
// running --provider=oidc) the test skips the same way its PreUpgrade
// counterpart does: there is no platform OAuthClient to compare, and a
// silent pass would hide the skip from pipeline readers.
func TestPostUpgradeOAuthClientRegistrationPreserved(t *testing.T) {
	skipUnlessUpgradePhase(t, PostUpgrade)

	tc, err := NewTestContext(t)
	require.NoError(t, err)
	ctx := tc.Context()
	baseline := requirePostUpgradeBaseline(t, ctx, tc.Client())

	oauthClient := &oauthv1.OAuthClient{}
	err = tc.Client().Get(ctx, types.NamespacedName{Name: upgradeOAuthClientName}, oauthClient)
	if k8serr.IsNotFound(err) && clusterAuthUsesOIDC(t, ctx, tc.Client()) {
		t.Skipf(
			"OAuthClient %s not found and kube-auth-proxy runs --provider=oidc: "+
				"this cluster authenticates via an external OIDC provider instead of the "+
				"integrated OpenShift OAuth server, so there is no platform OAuthClient to "+
				"compare after the upgrade (expected skip on BYOIDC clusters, matching the "+
				"PreUpgrade phase's skip)",
			upgradeOAuthClientName,
		)
	}
	require.NoError(t, err, "OAuthClient %s should still exist after the upgrade", upgradeOAuthClientName)

	currentEntries := oauthClientBaselineEntries(oauthClient)
	comparisons := []struct {
		key string
		msg string
	}{
		{baselineKeyOAuthGrantMethod, "grantMethod changed after the upgrade (registration changed)"},
		{baselineKeyOAuthRedirectURIs, "redirectURIs changed after the upgrade"},
		{baselineKeyOAuthSecretDigest, "secret was rotated during the upgrade (sha256 digest mismatch)"},
		{baselineKeyOAuthWWWChallenge, "respondWithChallenges changed after the upgrade"},
		{baselineKeyOAuthTokenMaxAge, "accessTokenMaxAgeSeconds changed after the upgrade"},
	}
	for _, comparison := range comparisons {
		require.Equal(t, requireBaselineEntry(t, baseline, comparison.key), currentEntries[comparison.key],
			"OAuthClient %s %s", upgradeOAuthClientName, comparison.msg)
	}
}

// TestPostUpgradeCredentialsSecretKeysPreserved verifies that the
// kube-auth-proxy credentials Secret still exists with the same key set it
// carried before the upgrade (client ID, client secret, cookie secret).
func TestPostUpgradeCredentialsSecretKeysPreserved(t *testing.T) {
	skipUnlessUpgradePhase(t, PostUpgrade)

	tc, err := NewTestContext(t)
	require.NoError(t, err)
	ctx := tc.Context()
	baseline := requirePostUpgradeBaseline(t, ctx, tc.Client())

	credentialsSecret := &corev1.Secret{}
	secretKey := types.NamespacedName{Name: upgradeCredentialsSecretName, Namespace: upgradeAuthNamespace}
	require.NoError(t, tc.Client().Get(ctx, secretKey, credentialsSecret),
		"credentials Secret %s/%s should still exist after the upgrade", upgradeAuthNamespace, upgradeCredentialsSecretName)

	// Key names only: values are compared as digests elsewhere and must
	// never reach a failure message (CWE-532).
	currentKeys := make([]string, 0, len(credentialsSecret.Data))
	for key := range credentialsSecret.Data {
		currentKeys = append(currentKeys, key)
	}
	slices.Sort(currentKeys)

	baselineKeys := make([]string, 0, len(upgradeCredentialsSecretKeys))
	for _, key := range upgradeCredentialsSecretKeys {
		requireBaselineEntry(t, baseline, baselineKeyPrefixCredsSecret+key+"Digest")
		baselineKeys = append(baselineKeys, key)
	}

	require.Equal(t, baselineKeys, currentKeys,
		"credentials Secret %s/%s key set changed after the upgrade: current keys %v, baseline keys %v",
		upgradeAuthNamespace, upgradeCredentialsSecretName, currentKeys, baselineKeys)
}

// TestPostUpgradeCookieSecretNotRotated verifies that the credentials
// Secret's values — including OAUTH2_PROXY_COOKIE_SECRET, whose rotation
// would force every user to re-login — were not rotated during the upgrade.
// Every per-key digest is compared against its baseline entry; the failure
// names the affected keys, never the values or their digests.
func TestPostUpgradeCookieSecretNotRotated(t *testing.T) {
	skipUnlessUpgradePhase(t, PostUpgrade)

	tc, err := NewTestContext(t)
	require.NoError(t, err)
	ctx := tc.Context()
	baseline := requirePostUpgradeBaseline(t, ctx, tc.Client())

	credentialsSecret := &corev1.Secret{}
	secretKey := types.NamespacedName{Name: upgradeCredentialsSecretName, Namespace: upgradeAuthNamespace}
	require.NoError(t, tc.Client().Get(ctx, secretKey, credentialsSecret),
		"credentials Secret %s/%s should still exist after the upgrade", upgradeAuthNamespace, upgradeCredentialsSecretName)

	currentDigests := credentialsSecretBaselineEntries(credentialsSecret)
	rotatedKeys := make([]string, 0)
	for entryKey, currentDigest := range currentDigests {
		if requireBaselineEntry(t, baseline, entryKey) != currentDigest {
			rotatedKeys = append(rotatedKeys,
				strings.TrimSuffix(strings.TrimPrefix(entryKey, baselineKeyPrefixCredsSecret), "Digest"))
		}
	}
	slices.Sort(rotatedKeys)
	require.Empty(t, rotatedKeys,
		"credentials Secret %s/%s values were rotated during the upgrade "+
			"(sha256 digest mismatch; keys affected: %v)",
		upgradeAuthNamespace, upgradeCredentialsSecretName, rotatedKeys)
}

// TestPostUpgradeKubeAuthProxyIdentityPreserved verifies that the
// kube-auth-proxy Deployment kept its identity across the upgrade: same UID
// (a change means delete-and-recreate), same image repository (the digest
// or tag may legitimately advance with the upgrade, the repository may not
// change) and the exact same flag-digested arg map — a flag added
// post-upgrade fails just like a removed one, in both directions.
func TestPostUpgradeKubeAuthProxyIdentityPreserved(t *testing.T) {
	skipUnlessUpgradePhase(t, PostUpgrade)

	tc, err := NewTestContext(t)
	require.NoError(t, err)
	ctx := tc.Context()
	baseline := requirePostUpgradeBaseline(t, ctx, tc.Client())

	deployment := &appsv1.Deployment{}
	deploymentKey := types.NamespacedName{Name: upgradeAuthProxyName, Namespace: upgradeAuthNamespace}
	require.NoError(t, tc.Client().Get(ctx, deploymentKey, deployment),
		"Deployment %s/%s should still exist after the upgrade", upgradeAuthNamespace, upgradeAuthProxyName)

	require.Equal(t, requireBaselineEntry(t, baseline, baselineKeyAuthProxyUID), string(deployment.UID),
		"Deployment %s/%s was recreated during the upgrade: its UID changed",
		upgradeAuthNamespace, upgradeAuthProxyName)

	container := authProxyContainer(deployment)
	require.NotNil(t, container,
		"Deployment %s/%s should still have a %s container after the upgrade",
		upgradeAuthNamespace, upgradeAuthProxyName, upgradeAuthProxyName)

	// Repository-to-repository: a real upgrade advances the image digest or
	// tag; the repository must not change. The failure names both
	// repositories so the operator's image swap is immediately visible.
	baselineImage := requireBaselineEntry(t, baseline, baselineKeyAuthProxyImage)
	baselineRepository := imageRepository(baselineImage)
	currentRepository := imageRepository(container.Image)
	require.Equal(t, baselineRepository, currentRepository,
		"Deployment %s/%s container %s image repository changed after the upgrade: "+
			"current %q != baseline %q (the digest or tag may advance with the upgrade, the repository may not)",
		upgradeAuthNamespace, upgradeAuthProxyName, upgradeAuthProxyName, currentRepository, baselineRepository)

	// argDigestEntries refuses secret-like args; a live cluster legitimately
	// carrying one must fail here rather than be compared.
	currentArgEntries, err := argDigestEntries(container.Args)
	require.NoError(t, err,
		"should digest the current kube-auth-proxy args; secret-like args are refused outright")

	// Assemble the baseline arg map from the kubeauthproxy.args.* entries;
	// a baseline carrying none is an incomplete capture, a failure.
	baselineArgEntries := make(map[string]string)
	for key, value := range baseline {
		if strings.HasPrefix(key, baselineKeyPrefixAuthProxyArgs) {
			baselineArgEntries[strings.TrimPrefix(key, baselineKeyPrefixAuthProxyArgs)] = value
		}
	}
	require.NotEmpty(t, baselineArgEntries,
		"baseline ConfigMap %s/%s carries no %s* entries: the PreUpgrade capture is incomplete; "+
			"re-run the full PreUpgrade phase before the PostUpgrade phase",
		upgradeBaselineNamespace, upgradeBaselineCMName, baselineKeyPrefixAuthProxyArgs)

	// Exact map equality compares both directions at once: a flag removed
	// by the upgrade and a flag added by it fail equally. The diff carries
	// flag names and value digests only, never raw values.
	require.Equal(t, baselineArgEntries, currentArgEntries,
		"Deployment %s/%s container %s args changed after the upgrade "+
			"(exact comparison of flag-to-digest entries, both directions)",
		upgradeAuthNamespace, upgradeAuthProxyName, upgradeAuthProxyName)
}

// TestPostUpgradeKubeAuthProxyAvailableAfterUpgrade verifies that the
// upgraded kube-auth-proxy rollout completed: the Deployment reports
// Available, its status observes the current generation, and its updated
// and available replicas sit at the desired count (Available=True alone can
// be satisfied by old pods during a failed rollout).
func TestPostUpgradeKubeAuthProxyAvailableAfterUpgrade(t *testing.T) {
	skipUnlessUpgradePhase(t, PostUpgrade)

	tc, err := NewTestContext(t)
	require.NoError(t, err)
	ctx := tc.Context()

	deploymentKey := types.NamespacedName{Name: upgradeAuthProxyName, Namespace: upgradeAuthNamespace}
	g := NewWithT(t)
	g.Eventually(func(g Gomega) {
		deployment := &appsv1.Deployment{}
		g.Expect(tc.Client().Get(ctx, deploymentKey, deployment)).To(Succeed(),
			"Deployment %s/%s should still exist after the upgrade", upgradeAuthNamespace, upgradeAuthProxyName)
		g.Expect(deploymentAvailable(deployment)).To(BeTrue(),
			"Deployment %s/%s should be Available after the upgrade; the upgraded auth proxy is not serving",
			upgradeAuthNamespace, upgradeAuthProxyName)
		desiredReplicas := int32(1)
		if deployment.Spec.Replicas != nil {
			desiredReplicas = *deployment.Spec.Replicas
		}
		g.Expect(deployment.Status.ObservedGeneration).To(Equal(deployment.Generation),
			"Deployment %s/%s status should observe the current generation after the upgrade; the rollout has not been observed",
			upgradeAuthNamespace, upgradeAuthProxyName)
		g.Expect(deployment.Status.UpdatedReplicas).To(Equal(desiredReplicas),
			"Deployment %s/%s should have all %d replicas updated after the upgrade; the upgraded auth proxy has not rolled out",
			upgradeAuthNamespace, upgradeAuthProxyName, desiredReplicas)
		g.Expect(deployment.Status.AvailableReplicas).To(Equal(desiredReplicas),
			"Deployment %s/%s should have all %d replicas available after the upgrade; the upgraded auth proxy is not fully serving",
			upgradeAuthNamespace, upgradeAuthProxyName, desiredReplicas)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
}

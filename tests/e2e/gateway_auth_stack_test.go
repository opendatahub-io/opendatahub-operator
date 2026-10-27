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

package e2e_test

// RHOAIENG-94376: additional-ingress authentication stack — identity, session
// preservation and blast radius on a live OpenShift cluster, on both
// authentication modes (IntegratedOAuth and BYOIDC/OIDC).
//
// Every additional ingress owns a complete auth stack: in IntegratedOAuth
// mode an OAuthClient named data-science-<ingress>, always a credentials
// Secret <ingress>-auth-creds carrying its own client/cookie secrets (the
// client ID coming from the shard's OAuthClient or, in OIDC mode, from the
// fixture's auth.oidc identity), a kube-auth-proxy-<ingress>
// Deployment/Service/HPA/NetworkPolicy, and a oauth-callback-route-<ingress>
// HTTPRoute parented on that ingress's Gateway. These specs pin, per ingress:
//
//   - auth identity: the shard's OAuthClient, client ID, redirect URL and cookie
//     domain, and pairwise-distinct cookie secrets (Tier1 inventory)
//   - session preservation: cookie values of the default and sibling stacks
//     survive another ingress's transient failures (credential rotation,
//     foreign ownership, capacity destruction) and add/remove churn; a deleted
//     credentials Secret rotates only its own ingress's cookie (Tier2 failure
//     isolation)
//   - persistence: the auth stacks and their cookie values survive an operator
//     restart at the HPA two-replica floor (Tier2, resilience suite)
//
// Scope boundary vs RHOAIENG-94365: the per-ingress condition machinery, the
// deletion of the auth stack on ingress removal and the default proxy's
// no-roll behavior are covered there; those assertions are deliberately not
// repeated here. The restart spec lives in the resilience suite (not the
// gateway suite) because restarting the operator inside a services spec would
// disturb every later spec of that suite.
//
// The two-ingress substrate comes from newNotebookIngressFixture
// (workbenches_ingress_test.go), shared with RHOAIENG-94382/96913.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	oauthv1 "github.com/openshift/api/oauth/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

// perIngressProxyName returns the auth proxy resource name for an additional
// ingress (Deployment, Service, HPA, ServiceAccount and NetworkPolicy share it).
func perIngressProxyName(ingressName string) string {
	return gateway.KubeAuthProxyName + "-" + ingressName
}

// perIngressCredsName returns the credentials Secret name for an additional ingress.
func perIngressCredsName(ingressName string) string {
	return ingressName + "-auth-creds"
}

// perIngressOAuthClientName returns the OAuthClient name for an additional ingress.
func perIngressOAuthClientName(ingressName string) string {
	return gateway.AuthClientID + "-" + ingressName
}

// perIngressCallbackRouteName returns the OAuth callback HTTPRoute name for an
// additional ingress.
func perIngressCallbackRouteName(ingressName string) string {
	return gateway.OAuthCallbackRouteName + "-" + ingressName
}

// perIngressDestinationRuleName returns the TLS DestinationRule name for an
// additional ingress: the shard Gateway's TLS policy for that shard's own
// auth proxy Service.
func perIngressDestinationRuleName(ingressName string) string {
	return ingressName + "-auth-destination"
}

// perIngressEnvoyFilterName returns the Gateway-scoped EnvoyFilter name for an
// additional ingress: ext_authz plus Lua identity/token forwarding, selected
// on that shard's Gateway workloads only.
func perIngressEnvoyFilterName(ingressName string) string {
	return ingressName + "-authn"
}

// cookieSecretDigest returns the SHA-256 fingerprint of a cookie secret
// value. The raw OAUTH2_PROXY_COOKIE_SECRET is a live session-encryption key;
// callers assert on fingerprints so a failing equality, emptiness or
// distinctness check cannot leak the key into test logs (CWE-532).
func cookieSecretDigest(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// fetchCookieSecretValue returns a SHA-256 fingerprint of the
// OAUTH2_PROXY_COOKIE_SECRET of the named credentials Secret in the gateway
// namespace. Fingerprints preserve equality semantics (distinct secrets yield
// distinct fingerprints) without ever exposing the raw key.
func (tc *GatewayTestCtx) fetchCookieSecretValue(ctx context.Context, credsName string) (string, error) {
	secret := &corev1.Secret{}
	if err := tc.Client().Get(ctx, types.NamespacedName{Name: credsName, Namespace: tc.gatewayNamespace()}, secret); err != nil {
		return "", err
	}
	value, ok := secret.Data["OAUTH2_PROXY_COOKIE_SECRET"]
	if !ok || len(value) == 0 {
		return "", fmt.Errorf("credentials secret %s has no OAUTH2_PROXY_COOKIE_SECRET", credsName)
	}
	return cookieSecretDigest(value), nil
}

// authProxyDeploymentAvailable reports whether the named kube-auth-proxy
// Deployment in the gateway namespace reports Available.
func (tc *GatewayTestCtx) authProxyDeploymentAvailable(ctx context.Context, proxyName string) bool {
	deployment := &appsv1.Deployment{}
	if err := tc.Client().Get(ctx, types.NamespacedName{Name: proxyName, Namespace: tc.gatewayNamespace()}, deployment); err != nil {
		return false
	}
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// authStackServingError verifies that the default and the named additional
// ingresses' auth proxies are Available and keep exactly the given cookie
// values (credentials Secret name -> expected value). It returns the first
// violation so it can be used both in require and in Eventually, which the
// add/remove phase needs: a reconcile that started before the re-add can run
// its GC after the new resources exist and briefly remove them again.
func (tc *GatewayTestCtx) authStackServingError(ctx context.Context, ingresses []serviceApi.AdditionalIngress, expectedCookies map[string]string) error {
	proxyNames := make([]string, 0, 1+len(ingresses))
	proxyNames = append(proxyNames, kubeAuthProxyName)
	for _, ingress := range ingresses {
		proxyNames = append(proxyNames, perIngressProxyName(ingress.Name))
	}
	for _, proxyName := range proxyNames {
		if !tc.authProxyDeploymentAvailable(ctx, proxyName) {
			return fmt.Errorf("auth proxy %s is not serving", proxyName)
		}
	}
	for credsName, expected := range expectedCookies {
		current, err := tc.fetchCookieSecretValue(ctx, credsName)
		if err != nil {
			return fmt.Errorf("credentials secret %s: %w", credsName, err)
		}
		if current != expected {
			return fmt.Errorf("cookie secret of %s must be preserved (sessions survive), got a different value", credsName)
		}
	}
	return nil
}

// requireAuthStackServing asserts that the default and the named additional
// ingresses' auth stacks keep serving with their sessions intact.
func (tc *GatewayTestCtx) requireAuthStackServing(t *testing.T, ctx context.Context, ingresses []serviceApi.AdditionalIngress, expectedCookies map[string]string) {
	t.Helper()
	require.NoError(t, tc.authStackServingError(ctx, ingresses, expectedCookies))
}

// ValidateAdditionalIngressAuthStack inventories the per-ingress auth stack of
// two additional ingresses on both authentication modes: in IntegratedOAuth
// mode an OAuthClient with the shard's callback redirect URI, in OIDC mode the
// fixture's auth.oidc client identity; always a credentials Secret with its
// own client/cookie secrets, HPA at the two-replica floor, NetworkPolicy
// scoped to the shard's proxy, the kube-auth-proxy Deployment/Service wired to
// the shard's credentials and hostname, and the callback HTTPRoute parented on
// the shard's Gateway.
func (tc *GatewayTestCtx) ValidateAdditionalIngressAuthStack(t *testing.T) {
	t.Helper()

	tc.SkipIfXKSCluster(t)
	skipUnless(t, Tier1)
	if !tc.isOcpRouteMode(t) {
		t.Skip("additional ingress auth stack specs require OcpRoute mode")
	}
	t.Log("Validating additional ingress auth stack inventory")

	fixture := newNotebookIngressFixture(t, tc)
	namespace := tc.gatewayNamespace()
	ctx := tc.Context()
	// The OAuthClient inventory only applies to IntegratedOAuth clusters; on
	// BYOIDC clusters (GatewayConfig carries spec.oidc) the shards instead own
	// the fixture's auth.oidc identities.
	oauthMode := fixture.original.Spec.OIDC == nil

	for _, ingress := range fixture.ingresses {
		proxyName := perIngressProxyName(ingress.Name)
		credsName := perIngressCredsName(ingress.Name)
		oauthClient := perIngressOAuthClientName(ingress.Name)
		callbackURL := "https://" + ingress.Hostname + gateway.OAuthCallbackPath
		redirectArg := makeRedirectURL(ingress.Hostname)
		cookieDomainArg := makeCookieDomain(ingress.Hostname)

		// OAuthClient (IntegratedOAuth only): the shard's own client identity.
		// Additional ingresses deliberately use grantMethod "prompt" where the
		// default uses "auto" (gateway_support.go branches non-default targets
		// to GrantHandlerPrompt).
		if oauthMode {
			tc.requireIngressOAuthClient(t, ctx, ingress.Name, oauthClient, callbackURL)
		}

		// Credentials Secret: the shard's own client and cookie secrets.
		tc.requireIngressCredentialsSecret(t, ctx, credsName, namespace)

		// HPA: two-replica floor, bounded maximum.
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.HorizontalPodAutoscaler, types.NamespacedName{Name: proxyName, Namespace: namespace}),
			WithCondition(And(
				jq.Match(`.spec.scaleTargetRef.kind == "Deployment"`),
				jq.Match(`.spec.scaleTargetRef.name == "%s"`, proxyName),
				jq.Match(`.spec.minReplicas >= 2`),
				jq.Match(`.spec.maxReplicas >= 2 and .spec.maxReplicas <= 10`),
			)),
			WithCustomErrorMsg("HPA %s should target the shard proxy with minReplicas >= 2 and maxReplicas within 2-10", proxyName),
		)

		// NetworkPolicy: scoped to the shard's own proxy (content semantics are
		// pinned by the envtest isolation specs, RHOAIENG-94376).
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.NetworkPolicy, types.NamespacedName{Name: proxyName, Namespace: namespace}),
			WithCondition(And(
				jq.Match(`.spec.podSelector.matchLabels.app == "%s"`, proxyName),
			)),
			WithCustomErrorMsg("NetworkPolicy %s should exist and select the shard's own proxy pods", proxyName),
		)

		// Deployment: wired to the shard's credentials and hostname identity.
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.Deployment, types.NamespacedName{Name: proxyName, Namespace: namespace}),
			WithCondition(And(
				jq.Match(`.spec.selector.matchLabels.app == "%s"`, proxyName),
				jq.Match(`.spec.template.spec.containers[0].env | any(.[]; .name == "%s" and .valueFrom.secretKeyRef.name == "%s")`, gateway.EnvClientID, credsName),
				jq.Match(`.spec.template.spec.containers[0].env | any(.[]; .name == "%s" and .valueFrom.secretKeyRef.name == "%s")`, gateway.EnvClientSecret, credsName),
				jq.Match(`.spec.template.spec.containers[0].env | any(.[]; .name == "%s" and .valueFrom.secretKeyRef.name == "%s")`, gateway.EnvCookieSecret, credsName),
				jq.Match(`.spec.template.spec.containers[0].args | any(. == "%s")`, redirectArg),
				jq.Match(`.spec.template.spec.containers[0].args | any(. == "%s")`, cookieDomainArg),
				jq.Match(`.spec.template.spec.containers[0].volumeMounts[] | select(.name == "tls-certs") | .readOnly == true`),
				jq.Match(`.spec.template.metadata.annotations["%s"] != null`, SecretHashAnnotation),
				jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`),
			)),
			WithCustomErrorMsg("auth proxy deployment %s should consume the shard credentials (%s), advertise the shard redirect URL (%s) and cookie domain, and be Available",
				proxyName, credsName, redirectArg),
		)

		// Service: the shard's proxy front-end (https + metrics ports; the plain
		// http container port is intentionally not exposed on the Service).
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.Service, types.NamespacedName{Name: proxyName, Namespace: namespace}),
			WithCondition(And(
				jq.Match(`.spec.selector.app == "%s"`, proxyName),
				jq.Match(`.spec.ports[] | select(.name == "https") | .port == %d`, kubeAuthProxyHTTPSPort),
				jq.Match(`.spec.ports[] | select(.name == "metrics") | .port == %d`, kubeAuthProxyMetricsPort),
			)),
			WithCustomErrorMsg("auth proxy service %s should select the shard's proxy pods and expose the https and metrics ports", proxyName),
		)

		// Callback HTTPRoute: parented on the shard's own Gateway.
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.HTTPRoute, types.NamespacedName{Name: perIngressCallbackRouteName(ingress.Name), Namespace: namespace}),
			WithCondition(And(
				jq.Match(`.spec.parentRefs | length == 1`),
				jq.Match(`.spec.parentRefs[0].group == "%s"`, gwapiv1.GroupVersion.Group),
				jq.Match(`.spec.parentRefs[0].kind == "Gateway"`),
				jq.Match(`.spec.parentRefs[0].name == "%s"`, ingress.Name),
				jq.Match(`.spec.parentRefs[0].namespace == "%s"`, namespace),
				jq.Match(`.spec.rules | length == 1`),
				jq.Match(`.spec.rules[0].matches[0].path.type == "PathPrefix"`),
				jq.Match(`.spec.rules[0].matches[0].path.value == "%s"`, authProxyOAuth2Path),
				jq.Match(`.spec.rules[0].backendRefs | length == 1`),
				jq.Match(`.spec.rules[0].backendRefs[0].kind == "Service"`),
				jq.Match(`.spec.rules[0].backendRefs[0].name == "%s"`, proxyName),
				jq.Match(`.spec.rules[0].backendRefs[0].port == %d`, kubeAuthProxyHTTPSPort),
			)),
			WithCustomErrorMsg("callback HTTPRoute %s should be parented on Gateway %s and back-end the shard proxy %s",
				perIngressCallbackRouteName(ingress.Name), ingress.Name, proxyName),
		)

		// DestinationRule: the shard Gateway's TLS policy for its own auth
		// proxy Service - SIMPLE TLS with verification disabled, on the
		// standard (443) and gateway (8443) HTTPS ports.
		proxyFQDN := getServiceFQDN(proxyName, namespace)
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.DestinationRule, types.NamespacedName{Name: perIngressDestinationRuleName(ingress.Name), Namespace: namespace}),
			WithCondition(And(
				jq.Match(`.spec.host == "%s"`, proxyFQDN),
				jq.Match(`.spec.trafficPolicy.portLevelSettings | length == 2`),
				jq.Match(`.spec.trafficPolicy.portLevelSettings | all(.[]; (.port.number == %d or .port.number == %d) and .tls.mode == "SIMPLE" and .tls.insecureSkipVerify == true)`,
					gateway.StandardHTTPSPort, gateway.GatewayHTTPSPort),
			)),
			WithCustomErrorMsg("DestinationRule %s should target the shard proxy %s with SIMPLE skip-verify TLS on ports %d/%d",
				perIngressDestinationRuleName(ingress.Name), proxyFQDN, gateway.StandardHTTPSPort, gateway.GatewayHTTPSPort),
		)

		// EnvoyFilter: the shard's Gateway-scoped auth filter chain - ext_authz
		// consulting that shard's own proxy (Istio EDS cluster for its
		// Service), plus the Lua identity/token-forwarding filter.
		shardEDSCluster := fmt.Sprintf("outbound|%d||%s", gateway.GatewayHTTPSPort, proxyFQDN)
		shardAuthURI := "https://" + net.JoinHostPort(proxyFQDN, strconv.Itoa(gateway.GatewayHTTPSPort)) + gateway.AuthProxyOAuth2Path + "/auth"
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.EnvoyFilter, types.NamespacedName{Name: perIngressEnvoyFilterName(ingress.Name), Namespace: namespace}),
			WithCondition(And(
				jq.Match(`.metadata.labels["%s"] == "%s"`, labels.K8SCommon.PartOf, gateway.PartOfLabelValue),
				jq.Match(`.spec.workloadSelector.labels."%s" == "%s"`, labels.GatewayAPI.GatewayName, ingress.Name),
				jq.Match(`.spec.configPatches | length == 2`),
				jq.Match(`.spec.configPatches[0].applyTo == "HTTP_FILTER"`),
				jq.Match(`.spec.configPatches[0].patch.value.name == "envoy.filters.http.ext_authz"`),
				jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.server_uri.cluster == "%s"`, shardEDSCluster),
				jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.server_uri.uri == "%s"`, shardAuthURI),
				jq.Match(`.spec.configPatches[1].applyTo == "HTTP_FILTER"`),
				jq.Match(`.spec.configPatches[1].patch.value.name == "envoy.filters.http.lua"`),
				jq.Match(`.spec.configPatches[1].patch.value.typed_config.inline_code | contains("x-auth-request-access-token")`),
				jq.Match(`.spec.configPatches[1].patch.value.typed_config.inline_code | contains("Bearer")`),
			)),
			WithCustomErrorMsg("EnvoyFilter %s should scope ext_authz (cluster %s) and Lua identity forwarding to shard %s's own Gateway and proxy",
				perIngressEnvoyFilterName(ingress.Name), shardEDSCluster, ingress.Name),
		)
	}

	// Auth identity: in IntegratedOAuth mode client IDs match their OAuthClients;
	// in OIDC mode the shards carry the fixture's auth.oidc client IDs and the
	// default carries the configured spec.oidc client. Cookie secrets are
	// pairwise distinct across the default and both shards in both modes, so
	// sessions cannot cross ingress boundaries.
	credsNames := make([]string, 0, 1+len(fixture.ingresses))
	credsNames = append(credsNames, kubeAuthProxyCredsName)
	for _, ingress := range fixture.ingresses {
		credsNames = append(credsNames, perIngressCredsName(ingress.Name))
	}
	cookieValues := make(map[string]string, len(credsNames))
	clientIDs := make(map[string]string, len(credsNames))
	for _, credsName := range credsNames {
		secret := &corev1.Secret{}
		require.NoError(t, tc.Client().Get(ctx, types.NamespacedName{Name: credsName, Namespace: namespace}, secret),
			"credentials secret %s should exist", credsName)
		// The cookie secret is fetched through fetchCookieSecretValue, which
		// fails on a missing or empty value and returns a SHA-256 fingerprint:
		// a failing distinctness check must not leak the live
		// session-encryption key into test logs (CWE-532). The OAuth client
		// ID is public (it appears in redirect URLs) and is compared as-is.
		cookie, err := tc.fetchCookieSecretValue(ctx, credsName)
		require.NoError(t, err, "cookie secret of %s should be non-empty", credsName)
		clientID := string(secret.Data["OAUTH2_PROXY_CLIENT_ID"])
		require.NotEmpty(t, clientID, "client ID of %s should be non-empty", credsName)
		for otherName, otherValue := range cookieValues {
			require.NotEqual(t, otherValue, cookie, "cookie secrets of %s and %s must be distinct", otherName, credsName)
		}
		cookieValues[credsName] = cookie
		clientIDs[credsName] = clientID
	}
	if oauthMode {
		require.Equal(t, oauthClientName, clientIDs[kubeAuthProxyCredsName],
			"default client ID must match the default OAuthClient")
		for _, ingress := range fixture.ingresses {
			require.Equal(t, perIngressOAuthClientName(ingress.Name), clientIDs[perIngressCredsName(ingress.Name)],
				"client ID of ingress %s must match its own OAuthClient", ingress.Name)
		}
	} else {
		require.Equal(t, fixture.original.Spec.OIDC.ClientID, clientIDs[kubeAuthProxyCredsName],
			"default client ID must match the configured spec.oidc client")
		for _, ingress := range fixture.ingresses {
			require.Equal(t, fixtureOIDCClientID(ingress.Name), clientIDs[perIngressCredsName(ingress.Name)],
				"client ID of ingress %s must match its fixture auth.oidc identity", ingress.Name)
		}
	}

	t.Log("Additional ingress auth stack inventory validated")
}

// requireIngressOAuthClient asserts the IntegratedOAuth identity of one
// additional ingress: an OAuthClient named for the shard with grant method
// prompt and the shard's own callback redirect URI.
//
// Asserted via a typed Get rather than EnsureResourceExists+jq.Match: the
// shared matcher's failure output dumps the whole resource, which would
// publish the OAuthClient secret into publicly readable CI build logs. The
// assertions below own their failure messages, so only names and URIs ever
// appear in test output (CWE-532).
func (tc *GatewayTestCtx) requireIngressOAuthClient(t *testing.T, ctx context.Context, ingressName, oauthClient, callbackURL string) {
	t.Helper()
	g := NewWithT(t)
	g.Eventually(func(g Gomega) {
		client := &oauthv1.OAuthClient{}
		g.Expect(tc.Client().Get(ctx, types.NamespacedName{Name: oauthClient}, client)).To(Succeed(),
			"additional ingress %s should own OAuthClient %s", ingressName, oauthClient)
		g.Expect(client.GrantMethod).To(Equal(oauthv1.GrantHandlerPrompt),
			"OAuthClient %s should use grant method prompt", oauthClient)
		g.Expect(client.RedirectURIs).To(ContainElement(callbackURL),
			"OAuthClient %s should carry the shard callback redirect URI %s", oauthClient, callbackURL)
		g.Expect(client.Secret).NotTo(BeEmpty(),
			"OAuthClient %s should have a non-empty secret", oauthClient)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
}

// requireIngressCredentialsSecret asserts the credentials Secret of one
// additional ingress: Opaque, labeled as gateway-owned, and carrying
// non-empty client and cookie secrets.
//
// Asserted via a typed Get for the same CWE-532 reason as
// requireIngressOAuthClient: a jq.Match failure would dump .data — the
// client and cookie secrets — into publicly readable CI build logs. Only
// key names and value lengths are asserted; values are compared elsewhere
// strictly as SHA-256 fingerprints.
func (tc *GatewayTestCtx) requireIngressCredentialsSecret(t *testing.T, ctx context.Context, credsName, namespace string) {
	t.Helper()
	g := NewWithT(t)
	g.Eventually(func(g Gomega) {
		secret := &corev1.Secret{}
		g.Expect(tc.Client().Get(ctx, types.NamespacedName{Name: credsName, Namespace: namespace}, secret)).To(Succeed(),
			"credentials secret %s should exist", credsName)
		g.Expect(secret.Type).To(Equal(corev1.SecretTypeOpaque),
			"credentials secret %s should be Opaque", credsName)
		g.Expect(secret.Labels).To(HaveKeyWithValue(labels.PlatformPartOf, gateway.PartOfGatewayConfig),
			"credentials secret %s should carry the %s=%s label", credsName, labels.PlatformPartOf, gateway.PartOfGatewayConfig)
		for _, key := range []string{
			"OAUTH2_PROXY_CLIENT_ID",
			"OAUTH2_PROXY_CLIENT_SECRET",
			"OAUTH2_PROXY_COOKIE_SECRET",
		} {
			value, ok := secret.Data[key]
			g.Expect(ok).To(BeTrue(),
				"credentials secret %s should carry %s", credsName, key)
			g.Expect(value).NotTo(BeEmpty(),
				"credentials secret %s should carry a non-empty %s", credsName, key)
		}
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
}

// ValidateAdditionalIngressAuthFailureIsolation injects auth failures into one
// additional ingress and asserts the blast radius never leaves it:
//
//   - transient: deleting the shard's credentials Secret rotates only that
//     shard's cookie secret; the default and sibling values are preserved
//   - foreign ownership: pointing the shard's credentials Secret's controller
//     ownerReference at a foreign GatewayConfig must not disturb the default or
//     sibling stacks; the shipped build silently reclaims the Secret and
//     regenerates the shard's credentials (its cookie rotates; repo main
//     instead degrades the shard's AuthenticationReady condition — a gap
//     between main and the shipped build, reported as a qualification finding
//     on RHOAIENG-94376)
//   - capacity: scaling the shard's auth proxy to zero degrades only that
//     shard while the default and sibling stacks keep serving; requests
//     through the down shard fail closed (no auth bypass, backend never
//     reached) and authorized traffic through the sibling keeps serving; the
//     HPA restores the two-replica floor
//   - callback failure: deleting the shard's callback HTTPRoute is healed by
//     the operator recreating it, without touching the sibling or default
//     callback routes
//   - HPA change: changing the shard's HPA bounds through its auth.maxReplicas
//     changes only that shard's HPA, with no unrelated churn
//   - add/remove: removing and re-adds a sibling shard preserves the default
//     and the remaining shard's auth stacks byte-for-byte
//   - quota: a Deployment-count ResourceQuota blocking the re-added shard's
//     proxy affects only that ingress; removing the quota recovers it
//
// The per-ingress condition machinery and the auth-stack deletion semantics are
// RHOAIENG-94365's and are deliberately not asserted here.
func (tc *GatewayTestCtx) ValidateAdditionalIngressAuthFailureIsolation(t *testing.T) {
	t.Helper()

	tc.SkipIfXKSCluster(t)
	skipUnless(t, Tier2)
	if !tc.isOcpRouteMode(t) {
		t.Skip("additional ingress auth stack specs require OcpRoute mode")
	}
	t.Log("Validating additional ingress auth failure isolation")

	fixture := newNotebookIngressFixture(t, tc)
	// The capacity phase drives real traffic through the shards, so each needs
	// its echo backend; the ServiceAccount bearer token is the authorized
	// request path that works on both authentication modes.
	backends := tc.deployShardEchoBackends(t, fixture)
	tokenSecret := tc.createShardTrafficTokenSecret(t)
	expectedCookies := tc.snapshotAuthCookies(t, fixture)

	tc.validateAdditionalIngressCookieRotation(t, fixture, expectedCookies)
	tc.validateAdditionalIngressOwnershipReclaim(t, fixture, expectedCookies)
	tc.validateAdditionalIngressCapacityIsolation(t, fixture, expectedCookies, backends, tokenSecret)
	tc.validateAdditionalIngressCallbackRecovery(t, fixture, expectedCookies)
	tc.validateAdditionalIngressHPAChange(t, fixture, expectedCookies)
	tc.validateAdditionalIngressAuthAddRemove(t, fixture, expectedCookies)
	tc.validateAdditionalIngressQuotaIsolation(t, fixture, expectedCookies)

	t.Log("Additional ingress auth failure isolation validated")
}

// snapshotAuthCookies captures the current cookie secret values of the default
// and both shard credentials Secrets.
func (tc *GatewayTestCtx) snapshotAuthCookies(t *testing.T, fixture *notebookIngressFixture) map[string]string {
	t.Helper()
	ctx := tc.Context()
	expectedCookies := map[string]string{
		kubeAuthProxyCredsName:                         "",
		perIngressCredsName(fixture.ingresses[0].Name): "",
		perIngressCredsName(fixture.ingresses[1].Name): "",
	}
	for credsName := range expectedCookies {
		value, err := tc.fetchCookieSecretValue(ctx, credsName)
		require.NoError(t, err, "credentials secret %s should exist", credsName)
		expectedCookies[credsName] = value
	}
	return expectedCookies
}

// setAdditionalIngresses replaces the GatewayConfig's additional ingresses.
func (tc *GatewayTestCtx) setAdditionalIngresses(t *testing.T, additional serviceApi.AdditionalIngresses) {
	t.Helper()
	g := NewWithT(t)
	ctx := tc.Context()
	g.Eventually(func() error {
		return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			current := &serviceApi.GatewayConfig{}
			if err := tc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, current); err != nil {
				return err
			}
			current.Spec.AdditionalIngresses = additional
			return tc.Client().Update(ctx, current)
		})
	}).Should(Succeed())
}

// validateAdditionalIngressCookieRotation deletes the first shard's
// credentials Secret and asserts that only that shard's cookie secret rotates.
func (tc *GatewayTestCtx) validateAdditionalIngressCookieRotation(t *testing.T, fixture *notebookIngressFixture, expectedCookies map[string]string) {
	t.Helper()
	g := NewWithT(t)
	ctx := tc.Context()
	alpha := fixture.ingresses[0]
	alphaCreds := perIngressCredsName(alpha.Name)

	require.NoError(t, tc.Client().Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: alphaCreds, Namespace: tc.gatewayNamespace(),
	}}), "delete alpha's credentials Secret")
	g.Eventually(func() (string, error) {
		return tc.fetchCookieSecretValue(ctx, alphaCreds)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).ShouldNot(Equal(expectedCookies[alphaCreds]),
		"alpha's cookie secret must rotate after its credentials Secret is deleted")
	for credsName := range expectedCookies {
		value, err := tc.fetchCookieSecretValue(ctx, credsName)
		require.NoError(t, err, "credentials secret %s should exist", credsName)
		if credsName != alphaCreds {
			require.Equal(t, expectedCookies[credsName], value,
				"cookie secret of %s must be preserved while alpha rotates", credsName)
		} else {
			expectedCookies[credsName] = value
		}
	}
}

// validateAdditionalIngressOwnershipReclaim points the first shard's
// credentials Secret controller ownerReference at a foreign GatewayConfig name
// (a fake UID alone would not do: controller-runtime's ownership check compares
// APIVersion/Kind/Name, so the operator would silently heal it either way).
//
// Shipped-build semantics (RHOAI 3.6.0, observed live): the shard recovers
// from the foreign ownership without degrading its AuthenticationReady
// condition and without disturbing any other stack. The recovery mechanism is
// delete-and-recreate, not an in-place ownership update: Kubernetes GC
// collects the Secret once its ownerReference targets an absent owner (the
// fake owner), the operator's next-but-one reconcile recreates it with the
// real GatewayConfig ownership and freshly generated credentials (the shard's
// cookie secret rotates), and one transient gatewayconfig Reconciler error
// ("uid mismatch ... no existing object was found") is logged in between.
// In-place reclaim is not observable here by design — GC racing any such
// update is standard Kubernetes behavior for orphaned dependents — so this
// phase pins the recovery end-state (present, correctly owned, rotated,
// serving), not the object's instance identity. The Secret holds only
// operator-generated credentials, so recreation loses nothing, and rotating
// them after an ownership compromise is the conservative response.
// Repo main (#4221, pinned by #4247's envtest) instead records
// an ownership conflict as AuthenticationReady=False/Unknown while preserving
// the stack; that degradation signal is absent from the shipped build
// (qualification finding on RHOAIENG-94376). This phase therefore pins the
// blast radius of the corruption, not the condition machinery
// (RHOAIENG-94365's).
func (tc *GatewayTestCtx) validateAdditionalIngressOwnershipReclaim(t *testing.T, fixture *notebookIngressFixture, expectedCookies map[string]string) {
	t.Helper()
	g := NewWithT(t)
	ctx := tc.Context()
	alpha, beta := fixture.ingresses[0], fixture.ingresses[1]
	alphaCreds := perIngressCredsName(alpha.Name)
	betaCreds := perIngressCredsName(beta.Name)

	secret := &corev1.Secret{}
	require.NoError(t, tc.Client().Get(ctx, types.NamespacedName{Name: alphaCreds, Namespace: tc.gatewayNamespace()}, secret))
	originalOwners := secret.OwnerReferences
	// Diagnostic snapshot only (see the phase comment): the UID is expected to
	// change across the recovery — K8s GC collects the orphaned Secret and the
	// operator recreates it — so it is logged, not asserted.
	secretUID := secret.UID
	fakeOwnerUID := types.UID("another-gateway-config")
	secret.OwnerReferences = []metav1.OwnerReference{{
		APIVersion:         serviceApi.GroupVersion.String(),
		Kind:               "GatewayConfig",
		Name:               string(fakeOwnerUID),
		UID:                fakeOwnerUID,
		Controller:         new(true),
		BlockOwnerDeletion: new(true),
	}}
	require.NoError(t, tc.Client().Update(ctx, secret), "corrupt alpha's credentials Secret ownership")
	restoreOwners := func() {
		current := &corev1.Secret{}
		if err := tc.Client().Get(ctx, types.NamespacedName{Name: alphaCreds, Namespace: tc.gatewayNamespace()}, current); err != nil {
			return
		}
		for _, owner := range current.OwnerReferences {
			if owner.UID == fakeOwnerUID {
				current.OwnerReferences = originalOwners
				if err := tc.Client().Update(ctx, current); err != nil {
					t.Logf("restore alpha's credentials Secret ownership: %v", err)
				}
				return
			}
		}
	}
	t.Cleanup(restoreOwners)

	// Blast radius while the corrupted Secret is in place: the default and
	// sibling stacks must keep serving with their sessions intact across the
	// reconcile cycles that observe the corruption.
	g.Consistently(func(g Gomega) {
		g.Expect(tc.authProxyDeploymentAvailable(ctx, kubeAuthProxyName)).To(BeTrue(),
			"default auth proxy must keep serving while alpha's credentials Secret is foreign-owned")
		g.Expect(tc.authProxyDeploymentAvailable(ctx, perIngressProxyName(beta.Name))).To(BeTrue(),
			"sibling auth proxy must keep serving while alpha's credentials Secret is foreign-owned")
		value, err := tc.fetchCookieSecretValue(ctx, kubeAuthProxyCredsName)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(value).To(Equal(expectedCookies[kubeAuthProxyCredsName]))
		value, err = tc.fetchCookieSecretValue(ctx, betaCreds)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(value).To(Equal(expectedCookies[betaCreds]))
	}, 30*time.Second, 5*time.Second).Should(Succeed(),
		"the foreign-owned credentials Secret must not affect the default or sibling auth stacks")

	// The shard recovers: a Secret of the same name exists again, owned by the
	// real GatewayConfig (only the operator's reconcile can restore that
	// ownership), with regenerated credentials, while alpha's proxy keeps
	// serving throughout. The instance identity is deliberately not pinned —
	// see the phase comment for the delete-and-recreate mechanism.
	g.Eventually(func(g Gomega) {
		current := &corev1.Secret{}
		g.Expect(tc.Client().Get(ctx, types.NamespacedName{Name: alphaCreds, Namespace: tc.gatewayNamespace()}, current)).To(Succeed())
		reclaimed := false
		for _, owner := range current.OwnerReferences {
			if owner.Controller != nil && *owner.Controller && owner.UID == fixture.original.UID {
				reclaimed = true
			}
		}
		g.Expect(reclaimed).To(BeTrue(), "the operator should reclaim the foreign-owned credentials Secret")
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
	require.True(t, tc.authProxyDeploymentAvailable(ctx, perIngressProxyName(alpha.Name)),
		"alpha's auth proxy should keep serving through the ownership reclaim")
	reclaimedSecret := &corev1.Secret{}
	require.NoError(t, tc.Client().Get(ctx, types.NamespacedName{Name: alphaCreds, Namespace: tc.gatewayNamespace()}, reclaimedSecret))
	if reclaimedSecret.UID != secretUID {
		t.Logf("alpha's credentials Secret was recovered by delete-and-recreate (uid %s -> %s): K8s GC collected the orphaned Secret and the operator recreated it",
			secretUID, reclaimedSecret.UID)
	}
	reclaimed, err := tc.fetchCookieSecretValue(ctx, alphaCreds)
	require.NoError(t, err)
	require.NotEqual(t, expectedCookies[alphaCreds], reclaimed,
		"alpha's cookie secret should rotate when the operator reclaims the foreign-owned credentials Secret")
	expectedCookies[alphaCreds] = reclaimed
}

// validateAdditionalIngressCapacityIsolation scales the first shard's auth
// proxy to zero and asserts that only that shard degrades, both at the control
// plane (sibling/default stacks keep serving, cookie secrets preserved) and on
// the dataplane: requests through the down shard fail closed - never a 200,
// never the backend's marker, i.e. no auth bypass through the unavailable
// ext_authz path - while authorized traffic through the sibling shard keeps
// serving. The HPA cannot recover a manually zero-scaled Deployment (with zero
// pods there is no metrics source to scale from — recovery from a deliberate
// zero-scale is manual by design), so the test restores the two-replica floor
// itself after asserting the isolation.
func (tc *GatewayTestCtx) validateAdditionalIngressCapacityIsolation(
	t *testing.T,
	fixture *notebookIngressFixture,
	expectedCookies map[string]string,
	backends []shardEchoBackend,
	tokenSecret string,
) {
	t.Helper()
	g := NewWithT(t)
	ctx := tc.Context()
	alpha, beta := fixture.ingresses[0], fixture.ingresses[1]
	alphaProxy := types.NamespacedName{Name: perIngressProxyName(alpha.Name), Namespace: tc.gatewayNamespace()}

	scaled := &appsv1.Deployment{}
	require.NoError(t, tc.Client().Get(ctx, alphaProxy, scaled))
	scaled.Spec.Replicas = new(int32(0))
	require.NoError(t, tc.Client().Update(ctx, scaled), "scale alpha's auth proxy to zero")
	// The fail-closed probe below is only meaningful once the proxy is really
	// down: while it is still serving, an unauthenticated request merely gets
	// the healthy 302 login redirect, which satisfies "not 200, no backend
	// marker" without ever exercising the unavailable ext_authz path.
	g.Eventually(func(g Gomega) {
		current := &appsv1.Deployment{}
		g.Expect(tc.Client().Get(ctx, alphaProxy, current)).To(Succeed())
		g.Expect(current.Status.ReadyReplicas).To(BeZero(),
			"alpha's auth proxy should finish terminating before the fail-closed probe")
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
	require.True(t, tc.authProxyDeploymentAvailable(ctx, kubeAuthProxyName),
		"default auth proxy must keep serving while alpha's auth proxy is scaled to zero")
	require.True(t, tc.authProxyDeploymentAvailable(ctx, perIngressProxyName(beta.Name)),
		"sibling auth proxy must keep serving while alpha's auth proxy is scaled to zero")
	for credsName, expected := range expectedCookies {
		value, err := tc.fetchCookieSecretValue(ctx, credsName)
		require.NoError(t, err, "credentials secret %s should exist", credsName)
		require.Equal(t, expected, value,
			"cookie secret of %s must be preserved while alpha's auth proxy is scaled to zero", credsName)
	}

	// Fail-closed dataplane: with alpha's auth path unavailable, requests
	// through alpha's shard must not succeed and must never carry its
	// backend's marker (no auth bypass through the down ext_authz path).
	g.Eventually(func(g Gomega) {
		out := tc.requestViaShard(t, alpha.IngressControllerName, alpha.Hostname)
		code, _ := out.statusAndURL("STATUS")
		g.Expect(code).NotTo(Equal("200"),
			"requests via %s must fail closed while its auth proxy is down, raw:\n%s", alpha.Hostname, out.raw)
		g.Expect(out.lines["BODY"]).NotTo(ContainSubstring(backends[0].marker),
			"the down shard's backend must never serve traffic, got %q", out.lines["BODY"])
	}, tc.TestTimeouts.authGatewayTimeout, 5*time.Second).Should(Succeed())
	rejected := tc.requestViaShard(t, alpha.IngressControllerName, alpha.Hostname)
	rejectedCode, _ := rejected.statusAndURL("STATUS")
	require.NotEqual(t, "200", rejectedCode,
		"requests via %s must stay failed closed, raw:\n%s", alpha.Hostname, rejected.raw)
	require.NotContains(t, rejected.lines["BODY"], backends[0].marker,
		"the down shard's backend must never serve traffic")

	// Authorized traffic through the healthy sibling keeps serving the whole
	// time (the default path's usability is pinned above by its proxy staying
	// Available with its session preserved).
	g.Eventually(func(g Gomega) {
		out := tc.requestViaShardWithToken(t, beta.IngressControllerName, beta.Hostname, tokenSecret)
		code, _ := out.statusAndURL("STATUS")
		g.Expect(code).To(Equal("200"),
			"authorized requests via the sibling %s must keep serving while alpha's auth proxy is down, raw:\n%s", beta.Hostname, out.raw)
		g.Expect(out.lines["BODY"]).To(ContainSubstring(backends[1].marker),
			"authorized requests should reach the sibling's own backend, got %q", out.lines["BODY"])
	}, tc.TestTimeouts.authGatewayTimeout, 5*time.Second).Should(Succeed())

	restored := &appsv1.Deployment{}
	require.NoError(t, tc.Client().Get(ctx, alphaProxy, restored))
	restored.Spec.Replicas = new(int32(2))
	require.NoError(t, tc.Client().Update(ctx, restored), "restore alpha's auth proxy to the two-replica floor")
	g.Eventually(func(g Gomega) {
		current := &appsv1.Deployment{}
		g.Expect(tc.Client().Get(ctx, alphaProxy, current)).To(Succeed())
		g.Expect(current.Status.ReadyReplicas).To(BeNumerically(">=", 2))
		g.Expect(tc.authProxyDeploymentAvailable(ctx, alphaProxy.Name)).To(BeTrue())
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed(),
		"alpha's auth proxy should recover once its two-replica floor is restored")
}

// validateAdditionalIngressAuthAddRemove removes and re-adds the second shard
// and asserts the default and first shard's auth stacks are preserved
// byte-for-byte. The re-added shard's own credentials are regenerated (fresh
// cookie secret), so only the surviving stacks are compared after the re-add.
func (tc *GatewayTestCtx) validateAdditionalIngressAuthAddRemove(t *testing.T, fixture *notebookIngressFixture, expectedCookies map[string]string) {
	t.Helper()
	g := NewWithT(t)
	ctx := tc.Context()
	alpha, beta := fixture.ingresses[0], fixture.ingresses[1]
	betaCreds := perIngressCredsName(beta.Name)
	survivingCookies := map[string]string{
		kubeAuthProxyCredsName:          expectedCookies[kubeAuthProxyCredsName],
		perIngressCredsName(alpha.Name): expectedCookies[perIngressCredsName(alpha.Name)],
	}

	tc.setAdditionalIngresses(t, serviceApi.AdditionalIngresses{alpha})
	g.Eventually(func() bool {
		err := tc.Client().Get(ctx, types.NamespacedName{
			Name: perIngressProxyName(beta.Name), Namespace: tc.gatewayNamespace(),
		}, &appsv1.Deployment{})
		return k8serr.IsNotFound(err)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(BeTrue(),
		"beta's auth proxy should be gone once beta is removed (sequencing wait; removal semantics are RHOAIENG-94365's)")
	tc.requireAuthStackServing(t, ctx, []serviceApi.AdditionalIngress{alpha}, survivingCookies)

	tc.setAdditionalIngresses(t, fixture.ingresses)
	tc.validateAdditionalGatewayAndRoute(t, beta)
	validateAdditionalIngressRouteAdmissionStatus(t, tc,
		types.NamespacedName{Name: beta.Name, Namespace: tc.gatewayNamespace()},
		beta.Name, beta.Hostname, beta.IngressControllerName)
	// A reconcile that started before the re-add can run its GC after the new
	// resources exist and briefly remove them again, so both checks retry.
	g.Eventually(func() error {
		return tc.authStackServingError(ctx, fixture.ingresses, survivingCookies)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed(),
		"the surviving auth stacks should keep their sessions intact after beta is re-added")
	g.Eventually(func() (string, error) {
		return tc.fetchCookieSecretValue(ctx, betaCreds)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).ShouldNot(BeEmpty(),
		"beta's credentials Secret should be recreated once beta is re-added")
}

// validateAdditionalIngressCallbackRecovery deletes the first shard's callback
// HTTPRoute out of band and asserts the AC behavior: the operator recreates it
// (ownership reclaim) and that shard's authentication recovers, while the
// sibling's and the default callback routes are untouched throughout.
func (tc *GatewayTestCtx) validateAdditionalIngressCallbackRecovery(t *testing.T, fixture *notebookIngressFixture, expectedCookies map[string]string) {
	t.Helper()
	g := NewWithT(t)
	ctx := tc.Context()
	alpha, beta := fixture.ingresses[0], fixture.ingresses[1]
	namespace := tc.gatewayNamespace()

	alphaCallback := types.NamespacedName{Name: perIngressCallbackRouteName(alpha.Name), Namespace: namespace}
	betaCallback := types.NamespacedName{Name: perIngressCallbackRouteName(beta.Name), Namespace: namespace}
	defaultCallback := types.NamespacedName{Name: gateway.OAuthCallbackRouteName, Namespace: namespace}

	betaRoute := &gwapiv1.HTTPRoute{}
	require.NoError(t, tc.Client().Get(ctx, betaCallback, betaRoute), "beta's callback route should exist")
	defaultRoute := &gwapiv1.HTTPRoute{}
	require.NoError(t, tc.Client().Get(ctx, defaultCallback, defaultRoute), "the default callback route should exist")

	require.NoError(t, tc.Client().Delete(ctx, &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: alphaCallback.Name, Namespace: alphaCallback.Namespace},
	}), "delete alpha's callback HTTPRoute")

	// The operator recreates the shard's callback route and it is admitted on
	// the shard's Gateway for the recreated route's current generation.
	g.Eventually(func(g Gomega) {
		route := &gwapiv1.HTTPRoute{}
		err := tc.Client().Get(ctx, alphaCallback, route)
		g.Expect(err).To(Succeed())
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed(),
		"the operator should recreate alpha's deleted callback HTTPRoute")
	tc.requireRouteAdmittedAtCurrentGeneration(t, alphaCallback, alpha.Name)
	tc.requireIngressAuthenticationReady(t, alpha.Name)

	// The sibling's and the default callback routes were untouched: same
	// objects, same specs (no unrelated churn).
	currentBeta := &gwapiv1.HTTPRoute{}
	require.NoError(t, tc.Client().Get(ctx, betaCallback, currentBeta))
	require.Equal(t, betaRoute.UID, currentBeta.UID,
		"beta's callback route must not be recreated by alpha's callback failure")
	require.Equal(t, betaRoute.Spec, currentBeta.Spec,
		"beta's callback route must keep its spec through alpha's callback failure")
	currentDefault := &gwapiv1.HTTPRoute{}
	require.NoError(t, tc.Client().Get(ctx, defaultCallback, currentDefault))
	require.Equal(t, defaultRoute.UID, currentDefault.UID,
		"the default callback route must not be recreated by alpha's callback failure")
	tc.requireAuthStackServing(t, ctx, fixture.ingresses, expectedCookies)
}

// validateAdditionalIngressHPAChange changes the first shard's HPA bounds
// through the product surface (its auth.maxReplicas) and asserts the AC blast
// radius: only that shard's HPA changes, the default and sibling HPAs and
// their proxies are untouched (no unrelated resource churn), sessions
// survive, and the affected ingress recovers its default bounds when the
// change is reverted.
func (tc *GatewayTestCtx) validateAdditionalIngressHPAChange(t *testing.T, fixture *notebookIngressFixture, expectedCookies map[string]string) {
	t.Helper()
	g := NewWithT(t)
	ctx := tc.Context()
	alpha, beta := fixture.ingresses[0], fixture.ingresses[1]
	namespace := tc.gatewayNamespace()

	snapshotHPA := func(name string) *autoscalingv2.HorizontalPodAutoscaler {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{}
		require.NoError(t, tc.Client().Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, hpa),
			"HPA %s should exist", name)
		return hpa
	}
	snapshotProxyUID := func(name string) types.UID {
		deployment := &appsv1.Deployment{}
		require.NoError(t, tc.Client().Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, deployment),
			"auth proxy %s should exist", name)
		return deployment.UID
	}
	defaultHPA := snapshotHPA(kubeAuthProxyName)
	betaHPA := snapshotHPA(perIngressProxyName(beta.Name))
	// Snapshot alpha's pre-change bound too: the revert must land back on it,
	// not on a hardcoded default (a cluster-level spec.authProxyMaxReplicas
	// override would make the default something other than 10).
	alphaHPA := snapshotHPA(perIngressProxyName(alpha.Name))
	defaultProxyUID := snapshotProxyUID(kubeAuthProxyName)
	betaProxyUID := snapshotProxyUID(perIngressProxyName(beta.Name))

	// Change only alpha's HPA bounds via its per-ingress auth configuration.
	changed := *alpha.DeepCopy()
	changed.Auth.MaxReplicas = new(int32(3))
	tc.setAdditionalIngresses(t, serviceApi.AdditionalIngresses{changed, beta})

	g.Eventually(func(g Gomega) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{}
		g.Expect(tc.Client().Get(ctx, types.NamespacedName{Name: perIngressProxyName(alpha.Name), Namespace: namespace}, hpa)).To(Succeed())
		g.Expect(hpa.Spec.MaxReplicas).To(Equal(int32(3)),
			"alpha's HPA maxReplicas should follow its auth.maxReplicas")
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())

	// Blast radius: the default and sibling HPAs keep their specs, their
	// proxies are not recreated, and every stack keeps serving with its
	// session intact.
	require.Equal(t, defaultHPA.Spec, snapshotHPA(kubeAuthProxyName).Spec,
		"the default HPA must be unchanged by a shard HPA change")
	require.Equal(t, betaHPA.Spec, snapshotHPA(perIngressProxyName(beta.Name)).Spec,
		"the sibling HPA must be unchanged by a shard HPA change")
	require.Equal(t, defaultProxyUID, snapshotProxyUID(kubeAuthProxyName),
		"the default auth proxy must not be recreated by a shard HPA change")
	require.Equal(t, betaProxyUID, snapshotProxyUID(perIngressProxyName(beta.Name)),
		"the sibling auth proxy must not be recreated by a shard HPA change")
	tc.requireAuthStackServing(t, ctx, fixture.ingresses, expectedCookies)

	// Revert: the affected ingress recovers its default HPA bounds.
	tc.setAdditionalIngresses(t, fixture.ingresses)
	g.Eventually(func(g Gomega) {
		hpa := &autoscalingv2.HorizontalPodAutoscaler{}
		g.Expect(tc.Client().Get(ctx, types.NamespacedName{Name: perIngressProxyName(alpha.Name), Namespace: namespace}, hpa)).To(Succeed())
		g.Expect(hpa.Spec.MaxReplicas).To(Equal(alphaHPA.Spec.MaxReplicas),
			"alpha's HPA maxReplicas should return to its pre-change bound after the change is reverted")
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
	tc.requireAuthStackServing(t, ctx, fixture.ingresses, expectedCookies)
}

// validateAdditionalIngressQuotaIsolation blocks net-new Deployment creation
// in the gateway namespace with an object-count ResourceQuota while a shard is
// re-added, and asserts the AC blast radius: only the quota-blocked ingress
// degrades, the healthy sibling and the default auth path keep serving, and
// the affected ingress converges once the quota is removed.
//
// The quota is sized to the Deployment count observed while the shard is
// removed, so the re-added shard's proxy Deployment is the only net-new
// creation it can block. Object-count quotas only reject creation of new
// objects; existing Deployments in the namespace (including the cluster
// routers that share it) keep scaling and rolling out unaffected.
func (tc *GatewayTestCtx) validateAdditionalIngressQuotaIsolation(t *testing.T, fixture *notebookIngressFixture, expectedCookies map[string]string) {
	t.Helper()
	g := NewWithT(t)
	ctx := tc.Context()
	alpha, beta := fixture.ingresses[0], fixture.ingresses[1]
	namespace := tc.gatewayNamespace()
	betaProxy := types.NamespacedName{Name: perIngressProxyName(beta.Name), Namespace: namespace}

	survivingCookies := map[string]string{
		kubeAuthProxyCredsName:          expectedCookies[kubeAuthProxyCredsName],
		perIngressCredsName(alpha.Name): expectedCookies[perIngressCredsName(alpha.Name)],
	}

	// Remove the shard and wait for its proxy Deployment to be gone.
	tc.setAdditionalIngresses(t, serviceApi.AdditionalIngresses{alpha})
	g.Eventually(func() bool {
		err := tc.Client().Get(ctx, betaProxy, &appsv1.Deployment{})
		return k8serr.IsNotFound(err)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(BeTrue(),
		"beta's auth proxy should be gone once beta is removed")

	// Block net-new Deployments in the gateway namespace. The namespace is
	// shared (the cluster routers live in it) and Deployment garbage
	// collection is asynchronous - e.g. routers of IngressControllers deleted
	// by an earlier run's cleanup - so a count taken against a churning set
	// can silently free a slot after sizing. The set is therefore allowed to
	// settle first (nothing terminating, stable count across two samples),
	// and the quota's binding is proven with a canary before the re-add
	// relies on it.
	listDeployments := func() *appsv1.DeploymentList {
		list := &appsv1.DeploymentList{}
		require.NoError(t, tc.Client().List(ctx, list, client.InNamespace(namespace)))
		return list
	}
	g.Eventually(func(g Gomega) bool {
		first := listDeployments()
		for i := range first.Items {
			if first.Items[i].DeletionTimestamp != nil {
				return false
			}
		}
		time.Sleep(10 * time.Second)
		return len(listDeployments().Items) == len(first.Items)
	}, 2*time.Minute, 10*time.Second).Should(BeTrue(),
		"the gateway namespace's Deployment set should settle before the quota is sized")

	quotaHard := func() corev1.ResourceList {
		return corev1.ResourceList{
			"count/deployments.apps": *resource.NewQuantity(int64(len(listDeployments().Items)), resource.DecimalSI),
		}
	}
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-shard-quota", Namespace: namespace},
		Spec:       corev1.ResourceQuotaSpec{Hard: quotaHard()},
	}
	require.NoError(t, tc.Client().Create(ctx, quota), "create the object-count ResourceQuota")
	t.Cleanup(func() {
		if err := tc.Client().Delete(tc.Context(), &corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: quota.Name, Namespace: quota.Namespace},
		}); err != nil && !k8serr.IsNotFound(err) {
			t.Errorf("delete test ResourceQuota: %v", err)
		}
	})

	// Prove the quota binds net-new Deployment creation with a canary before
	// relying on it. If the canary is admitted, the namespace shrank after
	// sizing (late asynchronous garbage collection): remove the canary,
	// re-size the quota to the current count, and retry.
	// A fresh canary object per attempt: once a Create succeeds, the returned
	// object carries server-populated metadata (resourceVersion, UID) that
	// the API server rejects on the next Create, so reusing one pointer across
	// Eventually retries would break the retry loop with a non-quota error.
	newCanary := func() *appsv1.Deployment {
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "e2e-quota-canary", Namespace: namespace},
			Spec: appsv1.DeploymentSpec{
				Replicas: new(int32(0)),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "e2e-quota-canary"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "e2e-quota-canary"}},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Name:  "canary",
						Image: echoBackendImage,
					}}},
				},
			},
		}
	}
	t.Cleanup(func() {
		if err := tc.Client().Delete(tc.Context(), newCanary()); err != nil && !k8serr.IsNotFound(err) {
			t.Errorf("delete quota canary deployment: %v", err)
		}
	})
	g.Eventually(func(g Gomega) error {
		canary := newCanary()
		err := tc.Client().Create(ctx, canary)
		if err == nil {
			g.Expect(tc.Client().Delete(ctx, canary)).To(Succeed())
			g.Eventually(func() bool {
				return k8serr.IsNotFound(tc.Client().Get(ctx, client.ObjectKeyFromObject(canary), &appsv1.Deployment{}))
			}, 30*time.Second, 2*time.Second).Should(BeTrue(),
				"the admitted canary deployment should be removed before re-sizing the quota")
			// Refresh the quota before resizing: the ResourceQuota controller
			// advances its resourceVersion through status.used updates, so an
			// update from the stale Create-returned object conflicts.
			g.Expect(retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				current := &corev1.ResourceQuota{}
				if err := tc.Client().Get(ctx, client.ObjectKeyFromObject(quota), current); err != nil {
					return err
				}
				current.Spec.Hard = quotaHard()
				return tc.Client().Update(ctx, current)
			})).To(Succeed())
			// Fail this attempt so the outer Eventually retries against the
			// re-sized quota.
			return errors.New("the quota did not bind net-new Deployments; re-sized to the current count, retrying")
		}
		g.Expect(err.Error()).To(ContainSubstring("exceeded quota"),
			"the canary creation should be denied by the ResourceQuota, got %v", err)
		return nil
	}, 2*time.Minute, 5*time.Second).Should(Succeed(),
		"the ResourceQuota should reject net-new Deployment creation in the gateway namespace")

	// Re-add the shard: its proxy Deployment must be rejected by the quota and
	// stay blocked while the quota is in place.
	tc.setAdditionalIngresses(t, fixture.ingresses)
	g.Consistently(func() bool {
		err := tc.Client().Get(ctx, betaProxy, &appsv1.Deployment{})
		return k8serr.IsNotFound(err)
	}, 30*time.Second, 5*time.Second).Should(BeTrue(),
		"beta's proxy Deployment must stay blocked while the quota is in place")

	// The quota failure affects only the blocked ingress: the default and the
	// healthy sibling keep serving with their sessions intact.
	require.NoError(t, tc.authStackServingError(ctx, []serviceApi.AdditionalIngress{alpha}, survivingCookies),
		"the default and sibling auth stacks must keep serving while beta is quota-blocked")

	// Removing the quota lets the affected ingress converge and recover.
	require.NoError(t, tc.Client().Delete(ctx, quota), "remove the ResourceQuota")
	g.Eventually(func(g Gomega) {
		deployment := &appsv1.Deployment{}
		g.Expect(tc.Client().Get(ctx, betaProxy, deployment)).To(Succeed())
		g.Expect(deployment.Status.ReadyReplicas).To(BeNumerically(">=", 2))
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed(),
		"beta's auth proxy should converge once the quota is removed")
	tc.requireIngressAuthenticationReady(t, beta.Name)
}

// operatorLeaderElectionID is the operator's compiled-in LeaderElectionID;
// keep in sync with cmd/main.go.
const operatorLeaderElectionID = "07ed84f7.opendatahub.io"

// ValidateAdditionalIngressAuthPersistenceAcrossOperatorRestart verifies that
// an operator restart does not churn the additional-ingress auth stacks or
// invalidate their sessions: every credentials Secret keeps its cookie secret
// value, every kube-auth-proxy Deployment keeps its UID and its two-replica
// (HPA floor) scale, and both shard Gateways stay admitted.
//
// It lives in the resilience suite rather than the gateway suite because
// restarting the operator inside a services spec would disturb every later
// spec of that suite.
func (tc *OperatorResilienceTestCtx) ValidateAdditionalIngressAuthPersistenceAcrossOperatorRestart(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier2)

	gtc := &GatewayTestCtx{TestContext: tc.TestContext}
	if !gtc.isOcpRouteMode(t) {
		t.Skip("additional ingress auth stack specs require OcpRoute mode")
	}
	t.Log("Validating additional ingress auth persistence across operator restart")

	fixture := newNotebookIngressFixture(t, gtc)
	g := NewWithT(t)
	ctx := gtc.Context()

	credsNames := make([]string, 0, 1+len(fixture.ingresses))
	credsNames = append(credsNames, kubeAuthProxyCredsName)
	proxyNames := make([]string, 0, 1+len(fixture.ingresses))
	proxyNames = append(proxyNames, kubeAuthProxyName)
	for _, ingress := range fixture.ingresses {
		credsNames = append(credsNames, perIngressCredsName(ingress.Name))
		proxyNames = append(proxyNames, perIngressProxyName(ingress.Name))
	}

	// Session identity to survive the restart: the exact cookie secret values.
	cookiesBefore := make(map[string]string, len(credsNames))
	for _, credsName := range credsNames {
		value, err := gtc.fetchCookieSecretValue(ctx, credsName)
		require.NoError(t, err, "credentials secret %s should exist", credsName)
		cookiesBefore[credsName] = value
	}

	// Auth-stack identity and scale to survive the restart: the proxy Deployment
	// UIDs and their settled replica counts. The HPA floors each stack at two
	// replicas, so wait for that floor before snapshotting.
	replicasBefore := make(map[string]int32, len(proxyNames))
	uidsBefore := make(map[string]types.UID, len(proxyNames))
	for _, proxyName := range proxyNames {
		key := types.NamespacedName{Name: proxyName, Namespace: gtc.gatewayNamespace()}
		deployment := &appsv1.Deployment{}
		g.Eventually(func(g Gomega) {
			g.Expect(gtc.Client().Get(ctx, key, deployment)).To(Succeed())
			g.Expect(deployment.Status.ReadyReplicas).To(BeNumerically(">=", 2),
				"auth proxy %s should settle at the HPA two-replica floor", proxyName)
		}, gtc.TestTimeouts.authGatewayTimeout, 5*time.Second).Should(Succeed())
		replicasBefore[proxyName] = deployment.Status.ReadyReplicas
		uidsBefore[proxyName] = deployment.UID
	}

	// Restart the operator (same procedure as ValidateAdditionalIngressRestartResilience):
	// wait until every pre-restart pod is gone before asserting anything, so the
	// assertions run against the restarted operator and not against a gracefully
	// terminating one.
	deploymentName := tc.getControllerDeploymentName()
	deployment := tc.getDeployment(t, types.NamespacedName{Namespace: tc.OperatorNamespace, Name: deploymentName})
	require.NotNil(t, deployment, "operator deployment not found")
	matchLabels, found, err := unstructured.NestedStringMap(deployment.Object, "spec", "selector", "matchLabels")
	require.NoError(t, err)
	require.True(t, found && len(matchLabels) > 0, "operator deployment has no pod selector")
	oldPods := tc.FetchResources(
		WithMinimalObject(gvk.Pod, types.NamespacedName{Namespace: tc.OperatorNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace:     tc.OperatorNamespace,
			LabelSelector: k8slabels.SelectorFromSet(matchLabels),
		}),
	)
	oldPodKeys := make([]types.NamespacedName, 0, len(oldPods))
	for _, pod := range oldPods {
		oldPodKeys = append(oldPodKeys, types.NamespacedName{Namespace: pod.GetNamespace(), Name: pod.GetName()})
	}
	// Restart time snapshot: the leader-election Lease must be renewed after
	// this point before the persistence assertions run.
	restartTime := time.Now()
	tc.deletePodsForDeployment(t, types.NamespacedName{Namespace: tc.OperatorNamespace, Name: deploymentName})
	g.Eventually(func() bool {
		for _, key := range oldPodKeys {
			if err := gtc.Client().Get(ctx, key, &corev1.Pod{}); !k8serr.IsNotFound(err) {
				return false
			}
		}
		return true
	}, gtc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(BeTrue(),
		"every pre-restart operator pod should be replaced")
	tc.validateDeploymentHealth(t)

	// Deployment readiness does not prove the replacement reconciled: the
	// readiness probe is a health ping, not a leader-election check, and the
	// one-shot persistence assertions below would otherwise pass against
	// unchanged pre-restart state. Wait for the leader-election Lease to be
	// held and renewed by a replacement pod: the Lease object outlives the old
	// leader, so renewTime alone could still reflect a renewal that raced the
	// pod deletion — the holder must not belong to any pre-restart pod.
	leaseGVK := schema.GroupVersionKind{Group: "coordination.k8s.io", Version: "v1", Kind: "Lease"}
	leaseKey := types.NamespacedName{Name: operatorLeaderElectionID, Namespace: tc.OperatorNamespace}
	leaseRenewed := func() bool {
		lease := &unstructured.Unstructured{}
		lease.SetGroupVersionKind(leaseGVK)
		if err := gtc.Client().Get(ctx, leaseKey, lease); err != nil {
			return false
		}
		// controller-runtime sets holderIdentity to "<hostname>_<uuid>" and a
		// pod's hostname is its pod name.
		holder, found, err := unstructured.NestedString(lease.Object, "spec", "holderIdentity")
		if err != nil || !found || holder == "" {
			return false
		}
		for _, key := range oldPodKeys {
			if strings.HasPrefix(holder, key.Name+"_") {
				return false
			}
		}
		renewTime, found, err := unstructured.NestedString(lease.Object, "spec", "renewTime")
		if err != nil || !found {
			return false
		}
		renewed, err := time.Parse(time.RFC3339Nano, renewTime)
		if err != nil {
			return false
		}
		return !renewed.Before(restartTime.Truncate(time.Second))
	}
	g.Eventually(leaseRenewed, gtc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(BeTrue(),
		"the replacement operator pod should acquire the leader-election Lease and renew it")

	// Persistence: sessions survive (same cookie secret values), the stacks are
	// not recreated (same UIDs) and keep their replica counts. Asserted over a
	// window so a late post-restart reconcile that churned anything is caught.
	g.Consistently(func(g Gomega) {
		for _, credsName := range credsNames {
			value, err := gtc.fetchCookieSecretValue(ctx, credsName)
			g.Expect(err).NotTo(HaveOccurred(), "credentials secret %s should exist after the restart", credsName)
			g.Expect(value).To(Equal(cookiesBefore[credsName]),
				"cookie secret of %s must survive the operator restart (sessions must not be invalidated)", credsName)
		}
		for _, proxyName := range proxyNames {
			deployment := &appsv1.Deployment{}
			g.Expect(gtc.Client().Get(ctx,
				types.NamespacedName{Name: proxyName, Namespace: gtc.gatewayNamespace()}, deployment)).To(Succeed(),
				"auth proxy %s should exist after the restart", proxyName)
			g.Expect(deployment.UID).To(Equal(uidsBefore[proxyName]),
				"auth proxy %s must not be recreated by the restart", proxyName)
			g.Expect(deployment.Status.ReadyReplicas).To(Equal(replicasBefore[proxyName]),
				"auth proxy %s must keep its replica count across the restart", proxyName)
			g.Expect(gtc.authProxyDeploymentAvailable(ctx, proxyName)).To(BeTrue(),
				"auth proxy %s should be Available after the restart", proxyName)
		}
	}, 30*time.Second, 5*time.Second).Should(Succeed(),
		"sessions and auth stacks must survive the operator restart")
	for _, ingress := range fixture.ingresses {
		gtc.validateAdditionalGatewayAndRoute(t, ingress)
	}

	t.Log("Additional ingress auth persistence across operator restart validated")
}

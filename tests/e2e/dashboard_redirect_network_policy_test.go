package e2e_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	gomegaTypes "github.com/onsi/gomega/types"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	// dashboardRedirectHTTPPort is the only port the redirect nginx container
	// exposes and the only port the NetworkPolicy may allow.
	dashboardRedirectHTTPPort = 8080

	// dashboardRedirectDriftPort is an undeclared port used to drift the policy
	// spec and verify that reconciliation restores the contract.
	dashboardRedirectDriftPort = 8081
)

// dashboardRedirectNetworkPolicyShape pins the redirect NetworkPolicy contract:
// GatewayConfig ownership, an exact non-empty pod selector, Ingress-only
// isolation (no egress rules), and a single ingress rule whose two namespace
// peers are alternatives (OR) allowing TCP 8080 only.
func dashboardRedirectNetworkPolicyShape() gomegaTypes.GomegaMatcher {
	return And(
		// Owned by the GatewayConfig so the gateway controller reconciles it.
		jq.Match(`.metadata.ownerReferences | any(.kind == "GatewayConfig" and .name == "%s" and .controller == true)`, gatewayConfigName),
		jq.Match(`.metadata.labels.app == "%s"`, gateway.DashboardRedirectName),
		jq.Match(`.metadata.labels["%s"] == "%s"`, labels.PlatformPartOf, gateway.PartOfGatewayConfig),

		// Selects exactly the redirect pods; the selector must stay non-empty.
		jq.Match(`.spec.podSelector.matchLabels.app == "%s"`, gateway.DashboardRedirectName),
		jq.Match(`.spec.podSelector.matchExpressions == null`),

		// Ingress-only isolation: no egress rules may be added.
		jq.Match(`.spec.policyTypes == ["Ingress"]`),
		jq.Match(`.spec.egress == null`),

		// Exactly one rule with the two alternative namespace peers on TCP 8080.
		jq.Match(`.spec.ingress | length == 1`),
		jq.Match(`.spec.ingress[0].from | length == 2`),
		jq.Match(`.spec.ingress[0].from[0].namespaceSelector.matchLabels."network.openshift.io/policy-group" == "ingress"`),
		jq.Match(`.spec.ingress[0].from[0].podSelector == null`),
		jq.Match(`.spec.ingress[0].from[1].namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "openshift-host-network"`),
		jq.Match(`.spec.ingress[0].from[1].podSelector == null`),
		jq.Match(`.spec.ingress[0].ports | length == 1`),
		jq.Match(`.spec.ingress[0].ports[0].port == %d`, dashboardRedirectHTTPPort),
		jq.Match(`.spec.ingress[0].ports[0].protocol == "%s"`, string(corev1.ProtocolTCP)),
	)
}

// ValidateDashboardRedirectNetworkPolicy validates the NetworkPolicy created
// for the dashboard redirect proxy.
func (tc *GatewayTestCtx) ValidateDashboardRedirectNetworkPolicy(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)
	t.Log("Validating dashboard redirect NetworkPolicy")

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.NetworkPolicy, types.NamespacedName{
			Name:      gateway.DashboardRedirectName,
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(dashboardRedirectNetworkPolicyShape()),
		WithCustomErrorMsg("dashboard-redirect NetworkPolicy should exist with the exact ingress-only contract on TCP %d", dashboardRedirectHTTPPort),
	)

	t.Log("Dashboard redirect NetworkPolicy validation completed")
}

// ValidateDSCIDefaultPolicyExcludesRedirectPods verifies that the DSCI baseline
// NetworkPolicy in the applications namespace does not select dashboard-redirect
// pods. NetworkPolicies are additive: a broad baseline selecting the redirect
// pods would grant ingress that the narrow gateway policy denies, masking its
// isolation on the cluster.
func (tc *GatewayTestCtx) ValidateDSCIDefaultPolicyExcludesRedirectPods(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)
	t.Log("Validating DSCI default NetworkPolicy excludes dashboard redirect pods")

	dsci := tc.FetchDSCInitialization()

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.NetworkPolicy, types.NamespacedName{
			Name:      dsci.Spec.ApplicationsNamespace,
			Namespace: dsci.Spec.ApplicationsNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.podSelector.matchExpressions | length == 1`),
			jq.Match(`.spec.podSelector.matchExpressions[0].key == "app"`),
			jq.Match(`.spec.podSelector.matchExpressions[0].operator == "NotIn"`),
			jq.Match(`.spec.podSelector.matchExpressions[0].values == ["%s"]`, gateway.DashboardRedirectName),
		)),
		WithCustomErrorMsg("DSCI default NetworkPolicy must exclude app=%s pods so the narrow gateway policy alone defines their ingress", gateway.DashboardRedirectName),
	)

	t.Log("DSCI default NetworkPolicy exclusion validation completed")
}

// ValidateDashboardRedirectNetworkPolicySelfHealing verifies that the policy is
// recreated after deletion and restored after spec drift without any
// GatewayConfig edit: the gateway controller watches the NetworkPolicies it
// owns.
func (tc *GatewayTestCtx) ValidateDashboardRedirectNetworkPolicySelfHealing(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)
	t.Log("Validating dashboard redirect NetworkPolicy self-healing")

	policyKey := types.NamespacedName{
		Name:      gateway.DashboardRedirectName,
		Namespace: tc.AppsNamespace,
	}

	// Deletion must trigger recreation with the full contract restored.
	tc.EnsureResourceDeletedThenRecreated(WithMinimalObject(gvk.NetworkPolicy, policyKey))
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.NetworkPolicy, policyKey),
		WithCondition(dashboardRedirectNetworkPolicyShape()),
		WithCustomErrorMsg("recreated dashboard-redirect NetworkPolicy should match the ingress contract"),
	)

	// Drift: widen the policy with an undeclared port. Reconciliation must
	// restore the single TCP 8080 rule without any GatewayConfig change.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.NetworkPolicy, policyKey),
		WithMutateFunc(testf.Transform(
			`.spec.ingress[0].ports += [{"port": %d, "protocol": "TCP"}]`,
			dashboardRedirectDriftPort,
		)),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.NetworkPolicy, policyKey),
		WithCondition(dashboardRedirectNetworkPolicyShape()),
		WithCustomErrorMsg("drifted dashboard-redirect NetworkPolicy should be restored to the ingress contract"),
	)

	t.Log("Dashboard redirect NetworkPolicy self-healing validation completed")
}

// ValidateDashboardRedirectNetworkPolicyTraffic verifies the effective isolation
// of the redirect pods on a cluster with an enforcing CNI:
//   - the only NetworkPolicy selecting them is the narrow gateway policy, so
//     neither the DSCI baseline nor any other broad policy can mask the
//     negative checks,
//   - an unapproved pod-network caller from another namespace cannot reach the
//     redirect pod on TCP 8080,
//   - the edge-terminated Route still reaches the redirect pods with the
//     policy in place.
//
// Undeclared ports are covered by the policy shape assertions above: the
// redirect container listens on TCP 8080 only, so a traffic probe of another
// port could not distinguish a policy deny from a missing listener.
func (tc *GatewayTestCtx) ValidateDashboardRedirectNetworkPolicyTraffic(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)
	if tc.IsXKS() {
		// Matches ValidateNetworkPolicyIngressTraffic: the repository's KinD
		// setup uses the default CNI, which does not enforce NetworkPolicy.
		t.Skip("KinD does not enforce NetworkPolicy ingress")
	}

	ctx := tc.Context()
	var redirectPod corev1.Pod
	tc.g.Eventually(func(g Gomega) {
		pods := &corev1.PodList{}
		g.Expect(tc.Client().List(ctx, pods,
			client.InNamespace(tc.AppsNamespace),
			client.MatchingLabels{"app": gateway.DashboardRedirectName},
		)).To(Succeed())
		g.Expect(pods.Items).NotTo(BeEmpty())
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" &&
				len(pod.Status.ContainerStatuses) > 0 && pod.Status.ContainerStatuses[0].Ready {
				redirectPod = pod
				return
			}
		}
		g.Expect(redirectPod.Status.PodIP).NotTo(BeEmpty(), "no ready dashboard-redirect pod has a Pod IP")
	}).Should(Succeed())

	// NetworkPolicies are additive. Report every policy selecting this pod so a
	// failure points to the effective policy union, not only our own manifest.
	policies := &networkingv1.NetworkPolicyList{}
	require.NoError(t, tc.Client().List(ctx, policies, client.InNamespace(tc.AppsNamespace)))
	selectedPolicies := make([]string, 0)
	for _, policy := range policies.Items {
		selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
		require.NoError(t, err)
		if selector.Matches(k8slabels.Set(redirectPod.Labels)) {
			selectedPolicies = append(selectedPolicies, policy.Name)
		}
	}
	require.ElementsMatch(t, []string{gateway.DashboardRedirectName}, selectedPolicies,
		"only the gateway policy may select dashboard-redirect pods; the DSCI baseline and any other broad policy would mask the isolation contract")
	t.Logf("NetworkPolicies selecting %s/%s: %v", redirectPod.Namespace, redirectPod.Name, selectedPolicies)

	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "dashboard-redirect-policy-probe-"}}
	require.NoError(t, tc.Client().Create(ctx, namespace))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := tc.Client().Delete(cleanupCtx, namespace); err != nil && !k8serr.IsNotFound(err) {
			t.Errorf("delete traffic probe namespace %s: %v", namespace.Name, err)
		}
	})
	tc.g.Eventually(func() error {
		return tc.Client().Get(ctx, types.NamespacedName{
			Name: "default", Namespace: namespace.Name,
		}, &corev1.ServiceAccount{})
	}).Should(Succeed())

	redirectURL := fmt.Sprintf("http://%s", net.JoinHostPort(redirectPod.Status.PodIP, strconv.Itoa(dashboardRedirectHTTPPort)))
	probe := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unapproved-dashboard-redirect-caller",
			Namespace: namespace.Name,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   new(true),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{{
				Name:    "probe",
				Image:   FakeGCSClientImage,
				Command: []string{"/bin/sh", "-c"},
				Args: []string{`
set -eu
if ! curl --noproxy '*' -ksS --connect-timeout 5 --max-time 10 -o /dev/null https://kubernetes.default.svc/version; then
  echo 'control request to Kubernetes API failed' > /dev/termination-log
  exit 1
fi
output="$(curl --noproxy '*' -kv --connect-timeout 5 --max-time 8 -o /dev/null "$REDIRECT_HTTP_URL" 2>&1)" || true
case "$output" in
  *'Connected to '*)
    echo "unexpected TCP connection to $REDIRECT_HTTP_URL" > /dev/termination-log
    exit 1
    ;;
esac
echo 'API control connected; redirect port denied' > /dev/termination-log
`},
				Env: []corev1.EnvVar{
					{Name: "REDIRECT_HTTP_URL", Value: redirectURL},
				},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: new(false),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
			}},
		},
	}
	require.NoError(t, tc.Client().Create(ctx, probe))

	key := types.NamespacedName{Name: probe.Name, Namespace: probe.Namespace}
	tc.g.Eventually(func() (string, error) {
		current := &corev1.Pod{}
		if err := tc.Client().Get(ctx, key, current); err != nil {
			return "", err
		}
		if len(current.Status.ContainerStatuses) > 0 {
			terminated := current.Status.ContainerStatuses[0].State.Terminated
			if terminated != nil {
				// The probe writes the termination log with echo, which appends a
				// trailing newline; trim it before comparing.
				return fmt.Sprintf("%s: %s", current.Status.Phase, strings.TrimSpace(terminated.Message)), nil
			}
		}
		return string(current.Status.Phase), nil
	}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Equal("Succeeded: API control connected; redirect port denied"))

	// The unapproved caller must not disturb the redirect pods.
	currentRedirect := &corev1.Pod{}
	require.NoError(t, tc.Client().Get(ctx, types.NamespacedName{
		Name: redirectPod.Name, Namespace: redirectPod.Namespace,
	}, currentRedirect), "the target redirect pod must still exist after the traffic probe")
	require.Equal(t, redirectPod.UID, currentRedirect.UID)
	require.Equal(t, redirectPod.Status.PodIP, currentRedirect.Status.PodIP)
	require.NotEmpty(t, currentRedirect.Status.ContainerStatuses)
	require.True(t, currentRedirect.Status.ContainerStatuses[0].Ready, "the target redirect pod must remain ready")

	// The approved ingress path: HTTPS through the edge-terminated Route must
	// still reach the redirect proxy with the policy in place. Plain HTTP is
	// not valid evidence: the router answers HTTP requests with its own
	// redirect before any traffic reaches the pod. On HostNetwork
	// IngressControllers (OVN-Kubernetes) the router traffic is node-sourced
	// and bypasses ingress NetworkPolicy enforcement, so Route success proves
	// the policy keeps the redirect path working, while the pod-network deny
	// probe above provides the enforcement evidence.
	routeName := getDashboardRouteNameByPlatform(tc.FetchPlatformRelease())
	routeObj := tc.EnsureResourceExists(
		WithMinimalObject(gvk.Route, types.NamespacedName{Name: routeName, Namespace: tc.AppsNamespace}),
		WithCustomErrorMsg("dashboard redirect route %s should exist", routeName),
	)
	routeHost := ExtractAndExpectValue[string](tc.g, routeObj, ".spec.host", Not(BeEmpty()))
	expectedGatewayHostname := tc.getExpectedGatewayHostname(t)
	httpClient := tc.createHTTPClient()

	requestURI := "/some/test/path?tab=models"
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, "https://"+routeHost+requestURI, nil)
	tc.g.Expect(err).NotTo(HaveOccurred())
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	tc.g.Expect(err).NotTo(HaveOccurred())
	defer func() { tc.g.Expect(resp.Body.Close()).To(Succeed()) }()
	tc.g.Expect(resp.StatusCode).To(Equal(http.StatusMovedPermanently))
	tc.g.Expect(resp.Header.Get("Location")).To(Equal("https://" + expectedGatewayHostname + requestURI))

	t.Log("Dashboard redirect NetworkPolicy traffic validation completed")
}

package e2e_test

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/gomega"
)

// The monitoring e2e suite already uses this pinned curl image. Reuse it for a
// disposable pod that checks the effective ingress policy from an unapproved namespace.
const authProxyTrafficProbeImage = FakeGCSClientImage

func (tc *GatewayTestCtx) ValidateNetworkPolicyIngressTraffic(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)
	if tc.IsXKS() {
		// The repository's KinD setup uses the default CNI, which does not enforce
		// NetworkPolicy. ValidateNetworkPolicy still checks the XKS policy shape.
		t.Skip("KinD does not enforce NetworkPolicy ingress")
	}

	ctx := tc.Context()
	var proxyPod corev1.Pod
	tc.g.Eventually(func(g Gomega) {
		pods := &corev1.PodList{}
		g.Expect(tc.Client().List(ctx, pods,
			client.InNamespace(tc.gatewayNamespace()),
			client.MatchingLabels{"app": kubeAuthProxyName},
		)).To(Succeed())
		g.Expect(pods.Items).NotTo(BeEmpty())
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" &&
				len(pod.Status.ContainerStatuses) > 0 && pod.Status.ContainerStatuses[0].Ready {
				proxyPod = pod
				return
			}
		}
		g.Expect(proxyPod.Status.PodIP).NotTo(BeEmpty(), "no ready kube-auth-proxy pod has a Pod IP")
	}).Should(Succeed())

	// NetworkPolicies are additive. Report every policy selecting this pod so a
	// failure points to the effective policy union, not only our own manifest.
	policies := &networkingv1.NetworkPolicyList{}
	require.NoError(t, tc.Client().List(ctx, policies, client.InNamespace(tc.gatewayNamespace())))
	selectedPolicies := make([]string, 0)
	for _, policy := range policies.Items {
		selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
		require.NoError(t, err)
		if selector.Matches(labels.Set(proxyPod.Labels)) {
			selectedPolicies = append(selectedPolicies, policy.Name)
		}
	}
	require.Contains(t, selectedPolicies, kubeAuthProxyName)
	t.Logf("NetworkPolicies selecting %s/%s: %v", proxyPod.Namespace, proxyPod.Name, selectedPolicies)

	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "gateway-network-policy-probe-"}}
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

	proxyURL := func(scheme string, port int) string {
		return fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(proxyPod.Status.PodIP, strconv.Itoa(port)))
	}
	probe := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unapproved-auth-proxy-caller",
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
				Image:   authProxyTrafficProbeImage,
				Command: []string{"/bin/sh", "-c"},
				Args: []string{`
set -eu
if ! curl --noproxy '*' -ksS --connect-timeout 5 --max-time 10 -o /dev/null https://kubernetes.default.svc/version; then
  echo 'control request to Kubernetes API failed' > /dev/termination-log
  exit 1
fi
for url in "$PROXY_HTTPS_URL" "$PROXY_HTTP_URL" "$PROXY_METRICS_URL"; do
  output="$(curl --noproxy '*' -kv --connect-timeout 5 --max-time 8 -o /dev/null "$url" 2>&1)" || true
  case "$output" in
    *'Connected to '*)
      echo "unexpected TCP connection to $url" > /dev/termination-log
      exit 1
      ;;
  esac
done
echo 'API control connected; all proxy ports denied' > /dev/termination-log
`},
				Env: []corev1.EnvVar{
					{Name: "PROXY_HTTPS_URL", Value: proxyURL("https", kubeAuthProxyHTTPSPort)},
					{Name: "PROXY_HTTP_URL", Value: proxyURL("http", kubeAuthProxyHTTPPort)},
					{Name: "PROXY_METRICS_URL", Value: proxyURL("http", kubeAuthProxyMetricsPort) + "/metrics"},
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
				return fmt.Sprintf("%s: %s", current.Status.Phase, strings.TrimSpace(terminated.Message)), nil
			}
		}
		return string(current.Status.Phase), nil
	}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Equal("Succeeded: API control connected; all proxy ports denied"))

	currentProxy := &corev1.Pod{}
	require.NoError(t, tc.Client().Get(ctx, types.NamespacedName{
		Name: proxyPod.Name, Namespace: proxyPod.Namespace,
	}, currentProxy), "the target proxy pod must still exist after the traffic probe")
	require.Equal(t, proxyPod.UID, currentProxy.UID)
	require.Equal(t, proxyPod.Status.PodIP, currentProxy.Status.PodIP)
	require.NotEmpty(t, currentProxy.Status.ContainerStatuses)
	require.True(t, currentProxy.Status.ContainerStatuses[0].Ready, "the target proxy must remain ready")
}

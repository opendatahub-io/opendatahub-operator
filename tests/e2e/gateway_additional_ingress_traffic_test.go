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

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"strings"
	"testing"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcfg "sigs.k8s.io/controller-runtime/pkg/client/config"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

// RHOAIENG-96913: authenticated traffic through additional ingresses, on both
// authentication modes.
//
// Enforcement is listener-level, not backend-level: every shard Gateway carries
// an EnvoyFilter (ext_authz) that consults that shard's kube-auth-proxy
// /oauth2/auth for EVERY request through the Gateway, and a Lua filter that
// strips the proxy cookie and injects the bearer token after a successful
// check. A plain backend therefore needs no auth sidecar of its own - it only
// needs to serve TLS on the gateway backend port, because the operator's
// DestinationRule makes the gateway talk TLS (skip-verify) to its backends.
//
// These specs deploy one distinct TLS echo backend per shard (nginx returning
// a shard-unique marker - no Notebook coupling), route it through the shard
// Gateway, and drive real traffic through the shard routers with an in-cluster
// curl pod:
//
//   - OIDC mode (BYOIDC clusters, guarded SkipUnlessBYOIDC): per-shard login
//     redirect identity, a complete mockCallback login through every shard,
//     per-shard current-generation traffic evidence, a forced cross-shard
//     session-cookie replay, and ServiceAccount bearer tokens.
//   - IntegratedOAuth mode (guarded SkipIfBYOIDC): per-shard redirect identity
//     (data-science-<ingress> OAuthClient) and ServiceAccount bearer tokens.
//
// Private IngressControllers are only reachable from inside the cluster, and
// the fixture hostnames (.e2e.invalid) have no DNS, so requests are sent from
// an in-cluster pod with curl --resolve <hostname>:443:<shard router IP>.

const (
	// Digest-pinned nginx unprivileged image serving the per-shard TLS echo.
	echoBackendImage = "nginxinc/nginx-unprivileged@sha256:65e3e85dbaed8ba248841d9d58a899b6197106c23cb0ff1a132b7bfe0547e4c0"
	// Shared request path served by every echo backend (distinct per-shard
	// hostnames keep the routes separate; the path is deliberately identical
	// so cross-shard requests are directly comparable).
	echoRoutePath = "/e2e-echo"
)

// shardEchoBackend is one deployed per-shard echo backend: a TLS nginx
// deployment answering every path with its marker, plus the HTTPRoute parenting
// it on the shard's Gateway. Everything lives in the applications namespace -
// the only namespace (besides the gateway namespace) the Gateways'
// allowedRoutes accept HTTPRoutes from.
type shardEchoBackend struct {
	ingress serviceApi.AdditionalIngress
	marker  string
}

// shardRouterClusterIP returns the ClusterIP of the private router service
// owned by the named IngressController.
func (tc *GatewayTestCtx) shardRouterClusterIP(t *testing.T, controllerName string) string {
	t.Helper()
	services := &corev1.ServiceList{}
	require.NoError(t, tc.Client().List(tc.Context(), services,
		client.InNamespace("openshift-ingress"),
		client.MatchingLabels{"ingresscontroller.operator.openshift.io/owning-ingresscontroller": controllerName},
	), "no router service found for IngressController %s", controllerName)
	require.NotEmpty(t, services.Items, "no router service found for IngressController %s", controllerName)
	ip := services.Items[0].Spec.ClusterIP
	require.NotEmpty(t, ip, "router service for IngressController %s has no ClusterIP", controllerName)
	return ip
}

// echoBackendPrefix returns the resource-name prefix for an ingress's echo backend.
func echoBackendPrefix(ingressName string) string {
	return "echo-" + ingressName
}

// deployEchoBackendTLS creates the self-signed TLS secret for the echo server.
// Any certificate works: the gateway's DestinationRule talks TLS to backends
// with verification disabled.
func (tc *GatewayTestCtx) deployEchoBackendTLS(t *testing.T, ingressName string) {
	t.Helper()
	certPEM, keyPEM := generateDexTLSAssets(t)
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      echoBackendPrefix(ingressName) + "-tls",
				Namespace: tc.AppsNamespace,
			},
			Type: corev1.SecretTypeTLS,
			Data: map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
		}),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)
}

// deployEchoBackendConfig creates the nginx server config returning the marker.
func (tc *GatewayTestCtx) deployEchoBackendConfig(t *testing.T, ingressName, marker string) {
	t.Helper()
	nginxConf := fmt.Sprintf(`server {
    listen 8443 ssl;
    server_name _;
    ssl_certificate /etc/nginx/tls/tls.crt;
    ssl_certificate_key /etc/nginx/tls/tls.key;
    location / { return 200 "%s\n"; }
}
`, marker)
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      echoBackendPrefix(ingressName) + "-conf",
				Namespace: tc.AppsNamespace,
			},
			Data: map[string]string{"nginx.conf": nginxConf},
		}),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)
}

// deployEchoBackendWorkload creates the echo Deployment and Service.
func (tc *GatewayTestCtx) deployEchoBackendWorkload(t *testing.T, ingressName string) {
	t.Helper()
	labels := map[string]string{"app": echoBackendPrefix(ingressName)}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      echoBackendPrefix(ingressName),
			Namespace: tc.AppsNamespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: new(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "echo",
						Image: echoBackendImage,
						Ports: []corev1.ContainerPort{{Name: "https", ContainerPort: gateway.GatewayHTTPSPort}},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "conf", MountPath: "/etc/nginx/conf.d", ReadOnly: true},
							{Name: "tls", MountPath: "/etc/nginx/tls", ReadOnly: true},
						},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: new(false),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
					}},
					Volumes: []corev1.Volume{
						{
							Name: "conf",
							VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: echoBackendPrefix(ingressName) + "-conf"},
							}},
						},
						{
							Name: "tls",
							VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
								SecretName: echoBackendPrefix(ingressName) + "-tls",
							}},
						},
					},
				},
			},
		},
	}
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(deployment),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      echoBackendPrefix(ingressName),
				Namespace: tc.AppsNamespace,
				Labels:    labels,
			},
			Spec: corev1.ServiceSpec{
				Selector: labels,
				Ports: []corev1.ServicePort{{
					Name:       "https",
					Port:       gateway.GatewayHTTPSPort,
					TargetPort: intstr.FromInt32(gateway.GatewayHTTPSPort),
				}},
			},
		}),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)
}

// deployEchoBackendRoute parents the echo Service on the shard's Gateway and
// waits for the route to be accepted and the backend to be available.
//
// The route is deliberately hostname-less, matching the convention of every
// product-created route on these Gateways (dashboard, Notebooks, the operator's
// own callback routes): on the OCP gateway implementation a hostname-bearing
// route creates a hostname-specific Envoy vhost that shadows the Gateway's
// wildcard vhost - which would leave the hostname-less oauth-callback route of
// the shard unrouted and break the login flow under test (verified live:
// /oauth2/callback returns the gateway's 404 while a hostname route exists).
// Host-based shard isolation is enforced by the router (the bridge Route only
// admits the shard hostname), not by the Gateway.
func (tc *GatewayTestCtx) deployEchoBackendRoute(t *testing.T, ingress serviceApi.AdditionalIngress) {
	t.Helper()
	route := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      echoBackendPrefix(ingress.Name),
			Namespace: tc.AppsNamespace,
			Labels:    map[string]string{"app": echoBackendPrefix(ingress.Name)},
		},
		Spec: gwapiv1.HTTPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{{
					Name:      gwapiv1.ObjectName(ingress.Name),
					Namespace: new(gwapiv1.Namespace(tc.gatewayNamespace())),
				}},
			},
			Rules: []gwapiv1.HTTPRouteRule{{
				Matches: []gwapiv1.HTTPRouteMatch{{
					Path: &gwapiv1.HTTPPathMatch{
						Type:  new(gwapiv1.PathMatchPathPrefix),
						Value: new(echoRoutePath),
					},
				}},
				BackendRefs: []gwapiv1.HTTPBackendRef{{
					BackendRef: gwapiv1.BackendRef{
						BackendObjectReference: gwapiv1.BackendObjectReference{
							Name: gwapiv1.ObjectName(echoBackendPrefix(ingress.Name)),
							Port: new(gwapiv1.PortNumber(gateway.GatewayHTTPSPort)),
						},
					},
				}},
			}},
		},
	}
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(route),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)

	// The route must be accepted by the shard Gateway and the backend reachable.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HTTPRoute, types.NamespacedName{
			Name:      echoBackendPrefix(ingress.Name),
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(And(
			jq.Match(`.status.parents[0].conditions[] | select(.type == "Accepted") | .status == "True"`),
			jq.Match(`.status.parents[0].conditions[] | select(.type == "ResolvedRefs") | .status == "True"`),
		)),
		WithEventuallyTimeout(tc.TestTimeouts.authGatewayTimeout),
		WithCustomErrorMsg("echo HTTPRoute for ingress %s should be accepted by its shard Gateway", ingress.Name),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      echoBackendPrefix(ingress.Name),
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`)),
		WithEventuallyTimeout(tc.TestTimeouts.operandReadinessTimeout),
		WithCustomErrorMsg("echo backend for ingress %s should be available", ingress.Name),
	)
}

// registerEchoBackendCleanup deletes the echo backend resources (LIFO: route,
// workload, config, TLS) once the spec finishes. Registered after the ingress
// fixture so it runs before the GatewayConfig restore.
func (tc *GatewayTestCtx) registerEchoBackendCleanup(t *testing.T, ingressName string) {
	t.Helper()
	names := []types.NamespacedName{
		{Name: echoBackendPrefix(ingressName), Namespace: tc.AppsNamespace},           // HTTPRoute, Deployment, Service
		{Name: echoBackendPrefix(ingressName) + "-conf", Namespace: tc.AppsNamespace}, // ConfigMap
		{Name: echoBackendPrefix(ingressName) + "-tls", Namespace: tc.AppsNamespace},  // Secret
	}
	t.Cleanup(func() {
		cleanupCtx := tc.Context()
		for _, name := range names {
			meta := metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace}
			if err := tc.Client().Delete(cleanupCtx, &gwapiv1.HTTPRoute{ObjectMeta: meta}); err != nil && !k8serr.IsNotFound(err) {
				t.Errorf("delete echo HTTPRoute %s: %v", name.Name, err)
			}
			if err := tc.Client().Delete(cleanupCtx, &appsv1.Deployment{ObjectMeta: meta}); err != nil && !k8serr.IsNotFound(err) {
				t.Errorf("delete echo deployment %s: %v", name.Name, err)
			}
			if err := tc.Client().Delete(cleanupCtx, &corev1.Service{ObjectMeta: meta}); err != nil && !k8serr.IsNotFound(err) {
				t.Errorf("delete echo service %s: %v", name.Name, err)
			}
			if err := tc.Client().Delete(cleanupCtx, &corev1.ConfigMap{ObjectMeta: meta}); err != nil && !k8serr.IsNotFound(err) {
				t.Errorf("delete echo configmap %s: %v", name.Name, err)
			}
			if err := tc.Client().Delete(cleanupCtx, &corev1.Secret{ObjectMeta: meta}); err != nil && !k8serr.IsNotFound(err) {
				t.Errorf("delete echo secret %s: %v", name.Name, err)
			}
		}
	})
}

// deployShardEchoBackends deploys one distinct TLS echo backend per shard of
// the fixture, each answering with its own marker, and returns the backends in
// fixture.ingresses order.
func (tc *GatewayTestCtx) deployShardEchoBackends(t *testing.T, fixture *notebookIngressFixture) []shardEchoBackend {
	t.Helper()

	backends := make([]shardEchoBackend, 0, len(fixture.ingresses))
	for _, ingress := range fixture.ingresses {
		marker := "echo-" + ingress.Name
		tc.deployEchoBackendTLS(t, ingress.Name)
		tc.deployEchoBackendConfig(t, ingress.Name, marker)
		tc.deployEchoBackendWorkload(t, ingress.Name)
		tc.registerEchoBackendCleanup(t, ingress.Name)
		tc.deployEchoBackendRoute(t, ingress)
		backends = append(backends, shardEchoBackend{ingress: ingress, marker: marker})
	}
	return backends
}

// probeOutput holds the labeled lines a probe pod wrote to its termination log.
type probeOutput struct {
	lines map[string]string
	raw   string
}

// redacted returns the raw probe log with session cookie values masked, so
// failure messages never leak live credentials into test logs (CWE-532).
func (p probeOutput) redacted() string {
	return redactProbeLog(p.raw)
}

// redactProbeLog masks the values of the COOKIES and LOGIN_COOKIE lines while
// keeping their labels and cookie names (still useful for diagnostics).
func redactProbeLog(log string) string {
	maskPairs := func(line string) string {
		label, rest, _ := strings.Cut(line, " ")
		var b strings.Builder
		for pair := range strings.SplitSeq(rest, ";") {
			name, _, _ := strings.Cut(strings.TrimSpace(pair), "=")
			if name == "" {
				continue
			}
			b.WriteString(name + "=<redacted>; ")
		}
		return label + " " + b.String()
	}
	var out strings.Builder
	for line := range strings.SplitSeq(log, "\n") {
		if strings.HasPrefix(line, "COOKIES ") || strings.HasPrefix(line, "LOGIN_COOKIE ") {
			line = maskPairs(line)
		}
		out.WriteString(line + "\n")
	}
	return out.String()
}

// probeTerminatedLog concatenates a probe pod's container termination messages.
func probeTerminatedLog(pod *corev1.Pod) string {
	var b strings.Builder
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Terminated != nil {
			b.WriteString(status.State.Terminated.Message)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// statusAndURL splits a "<http code> <url>" label value.
func (p probeOutput) statusAndURL(label string) (string, string) {
	value, ok := p.lines[label]
	if !ok {
		return "", ""
	}
	code, url, _ := strings.Cut(value, " ")
	return code, url
}

// parseProbeOutput splits each "LABEL value" line of a probe termination log.
func parseProbeOutput(message string) probeOutput {
	lines := make(map[string]string)
	for line := range strings.SplitSeq(message, "\n") {
		label, value, found := strings.Cut(strings.TrimSpace(line), " ")
		if !found || label == "" {
			continue
		}
		if _, seen := lines[label]; !seen {
			lines[label] = value
		}
	}
	return probeOutput{lines: lines, raw: message}
}

// createCurlProbePod creates (but does not wait for) an in-cluster curl pod
// running the shell script, which must write its results as labeled lines to
// /dev/termination-log. The pod is deleted at test cleanup.
func (tc *GatewayTestCtx) createCurlProbePod(t *testing.T, script string) *corev1.Pod {
	t.Helper()
	return tc.createCurlProbePodWithEnv(t, script, nil)
}

// createCurlProbePodWithEnv is createCurlProbePod plus container environment
// variables. Credentials for the probe (e.g. the ServiceAccount bearer token)
// must be injected this way, sourced from a Secret — never embedded in the
// script, which is the pod's args and visible in its spec (CWE-532).
func (tc *GatewayTestCtx) createCurlProbePodWithEnv(t *testing.T, script string, env []corev1.EnvVar) *corev1.Pod {
	t.Helper()

	probe := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "shard-traffic-probe-",
			Namespace:    tc.AppsNamespace,
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
				Args:    []string{script},
				Env:     env,
			}},
		},
	}
	require.NoError(t, tc.Client().Create(tc.Context(), probe))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := tc.Client().Delete(cleanupCtx, probe); err != nil && !k8serr.IsNotFound(err) {
			t.Errorf("delete traffic probe pod: %v", err)
		}
	})
	return probe
}

// collectCurlProbePod waits for the probe pod to succeed and returns the
// parsed output. requiredLabel must be the first label the script emits, so
// an empty or crashed probe fails loudly instead of parsing silence.
func (tc *GatewayTestCtx) collectCurlProbePod(t *testing.T, probe *corev1.Pod, requiredLabel string, timeout time.Duration) probeOutput {
	t.Helper()
	ctx := tc.Context()

	g := NewWithT(t)
	g.Eventually(func(g Gomega) {
		current := &corev1.Pod{}
		g.Expect(tc.Client().Get(ctx, types.NamespacedName{Name: probe.Name, Namespace: probe.Namespace}, current)).To(Succeed())
		// PodFailed is terminal: the probe script exits non-zero on purpose
		// (e.g. LOGIN1_OK no), so report immediately instead of polling a
		// dead pod until the collect timeout.
		if current.Status.Phase == corev1.PodFailed {
			StopTrying(fmt.Sprintf("traffic probe pod failed, phase %s, log:\n%s",
				current.Status.Phase, redactProbeLog(probeTerminatedLog(current)))).Now()
		}
		// A succeeded probe whose termination log already exists but lacks
		// the required label is terminal too — the labels cannot appear
		// anymore (the kubelet keeps only the last 4096 bytes of the
		// termination message, so an overwritten label is gone for good) —
		// so report immediately instead of polling a finished pod until the
		// collect timeout.
		if current.Status.Phase == corev1.PodSucceeded {
			for _, status := range current.Status.ContainerStatuses {
				if status.State.Terminated != nil && status.State.Terminated.Message != "" &&
					!strings.Contains(status.State.Terminated.Message, requiredLabel) {
					StopTrying(fmt.Sprintf("traffic probe succeeded without %s, log:\n%s",
						requiredLabel, redactProbeLog(status.State.Terminated.Message))).Now()
				}
			}
		}
		g.Expect(current.Status.Phase).To(Equal(corev1.PodSucceeded),
			"traffic probe pod should succeed, phase %s, log:\n%s", current.Status.Phase, redactProbeLog(probeTerminatedLog(current)))
		for _, status := range current.Status.ContainerStatuses {
			if status.State.Terminated != nil {
				g.Expect(status.State.Terminated.ExitCode).To(BeZero())
				g.Expect(redactProbeLog(status.State.Terminated.Message)).To(ContainSubstring(requiredLabel),
					"probe should report results (log redacted)")
			}
		}
	}, timeout, 2*time.Second).Should(Succeed())

	current := &corev1.Pod{}
	require.NoError(t, tc.Client().Get(ctx, types.NamespacedName{Name: probe.Name, Namespace: probe.Namespace}, current))
	for _, status := range current.Status.ContainerStatuses {
		if status.State.Terminated != nil {
			return parseProbeOutput(status.State.Terminated.Message)
		}
	}
	require.FailNow(t, "traffic probe pod has no terminated container")
	return probeOutput{}
}

// runCurlProbePod runs the shell script (which must write its results as
// labeled lines to /dev/termination-log) in an in-cluster curl pod and returns
// the parsed output. requiredLabel must be the first label the script emits,
// so an empty or crashed probe fails loudly instead of parsing silence.
func (tc *GatewayTestCtx) runCurlProbePod(t *testing.T, script, requiredLabel string) probeOutput {
	t.Helper()
	return tc.collectCurlProbePod(t, tc.createCurlProbePod(t, script), requiredLabel, tc.TestTimeouts.authGatewayTimeout)
}

// requestViaShard sends one in-cluster HTTPS request to the shard hostname
// (resolved against the shard's router service) and returns the observed
// status, redirect target, body and cookie jar.
func (tc *GatewayTestCtx) requestViaShard(t *testing.T, controllerName, hostname string) probeOutput {
	t.Helper()
	return tc.requestViaShardOpts(t, controllerName, hostname, "")
}

// requestViaShardWithToken is requestViaShard authenticating with the
// ServiceAccount bearer token held in the named Secret (see
// createShardTrafficTokenSecret). The token reaches the probe pod as the
// SHARD_TRAFFIC_TOKEN environment variable sourced from that Secret and is
// referenced only as a shell variable inside the script — it never appears in
// the pod spec or args (CWE-532).
func (tc *GatewayTestCtx) requestViaShardWithToken(t *testing.T, controllerName, hostname, tokenSecret string) probeOutput {
	t.Helper()
	return tc.requestViaShardOpts(t, controllerName, hostname, tokenSecret)
}

func (tc *GatewayTestCtx) requestViaShardOpts(t *testing.T, controllerName, hostname, tokenSecret string) probeOutput {
	t.Helper()
	routerIP := tc.shardRouterClusterIP(t, controllerName)
	// The bearer header references only the shell variable: the token value
	// itself is injected as SHARD_TRAFFIC_TOKEN from the token Secret (see
	// createCurlProbePodWithEnv), never written into the script.
	authHeader := ""
	if tokenSecret != "" {
		authHeader = `-H "Authorization: Bearer $SHARD_TRAFFIC_TOKEN" `
	}
	script := fmt.Sprintf(`set -u
LG=/dev/termination-log
curl -ksS --noproxy '*' --connect-timeout 5 --max-time 60 \
  --resolve %[1]s:443:%[2]s %[3]s-o /tmp/body \
  -w 'STATUS %%{http_code} %%{redirect_url}\n' https://%[1]s%[4]s > $LG 2>&1 \
  || echo 'STATUS 000 curl-failed' >> $LG
echo "BODY $(head -c 120 /tmp/body 2>/dev/null | tr -d '\r\n')" >> $LG
if [ -f /tmp/jar ]; then
  echo "COOKIES $(awk '/^#HttpOnly_/ || !/^#/ {if (NF>=7) printf "%%s=%%s; ", $6, $7}' /tmp/jar | head -c 300)" >> $LG
fi
`, hostname, routerIP, authHeader, echoRoutePath)
	if tokenSecret != "" {
		env := []corev1.EnvVar{{
			Name: "SHARD_TRAFFIC_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: tokenSecret},
					Key:                  "token",
				},
			},
		}}
		probe := tc.createCurlProbePodWithEnv(t, script, env)
		return tc.collectCurlProbePod(t, probe, "STATUS", tc.TestTimeouts.authGatewayTimeout)
	}
	return tc.runCurlProbePod(t, script, "STATUS")
}

// loginViaShard completes a full OIDC login through one shard with the test
// Dex (mockCallback) and returns the final response plus the established
// session cookies. kube-auth-proxy always sends approval_prompt=force, which
// makes Dex show its approval screen even with skipApprovalScreen set, so the
// login needs two phases: follow the redirect chain to the approval page,
// then submit the approval form - Dex redirects back with the code, the proxy
// validates it, sets the session cookie and returns to the original path.
// The probe reports the session cookie NAMES only: values are live session
// credentials and must not reach pod status or test logs (CWE-532). Specs
// that need the cookie value for replay keep it inside a single probe
// process instead (see the rotation probe).
func (tc *GatewayTestCtx) loginViaShard(t *testing.T, controllerName, hostname string) probeOutput {
	t.Helper()
	routerIP := tc.shardRouterClusterIP(t, controllerName)
	script := fmt.Sprintf(`set -u
LG=/dev/termination-log
HOST=%[1]s
IP=%[2]s
P1=$(curl -ksS --noproxy '*' -L --connect-timeout 5 --max-time 60 \
  --resolve $HOST:443:$IP -c /tmp/jar -o /tmp/body \
  -w '%%{http_code} %%{url_effective}' https://$HOST%[3]s 2>>$LG) \
  || P1='000 curl-failed'
echo "P1 $P1" >> $LG
case "$P1" in
  *"/dex/approval"*)
    REQ=$(printf '%%s' "$P1" | sed -n 's/.*[?&]req=\([^&]*\).*/\1/p')
    curl -ksS --noproxy '*' -L --connect-timeout 5 --max-time 60 \
      --resolve $HOST:443:$IP -b /tmp/jar -c /tmp/jar -o /tmp/body \
      -w 'STATUS %%{http_code} %%{url_effective}\n' \
      "$(printf '%%s' "$P1" | sed 's/^[^ ]* //')" -d "req=$REQ" -d 'approval=approve' >> $LG 2>&1 \
      || echo 'STATUS 000 curl-failed' >> $LG
    ;;
  *)
    echo "STATUS $P1" >> $LG
    ;;
esac
echo "BODY $(head -c 120 /tmp/body 2>/dev/null | tr -d '\r\n')" >> $LG
if [ -f /tmp/jar ]; then
  echo "COOKIES $(awk '/^#HttpOnly_/ || !/^#/ {if (NF>=7) printf "%%s; ", $6}' /tmp/jar | head -c 300)" >> $LG
fi
`, hostname, routerIP, echoRoutePath)
	return tc.runCurlProbePod(t, script, "P1")
}

// perIngressCookieName mirrors the controller's per-ingress session cookie
// name (AuthProxyCookieName + "_" + ingress name with dashes replaced).
func perIngressCookieName(ingressName string) string {
	return "_oauth2_proxy_" + strings.ReplaceAll(ingressName, "-", "_")
}

// createShardTrafficTokenSecret provisions a ServiceAccount in the applications
// namespace and returns the name of a test-owned Secret holding a short-lived
// (1h) TokenRequest token for it, for the documented kube-auth-proxy
// ServiceAccount token path. The token is delivered to probe pods as an
// environment variable sourced from the Secret: it never appears in a pod's
// spec, args or the test's own output (CWE-532), and unlike the legacy
// service-account token Secret flow it expires instead of living forever.
func (tc *GatewayTestCtx) createShardTrafficTokenSecret(t *testing.T) string {
	t.Helper()
	ctx := tc.Context()
	name := "e2e-shard-traffic-probe"

	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: tc.AppsNamespace}}
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(sa),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := tc.Client().Delete(cleanupCtx, sa); err != nil && !k8serr.IsNotFound(err) {
			t.Errorf("delete traffic probe service account: %v", err)
		}
	})

	// A bound, short-lived token via TokenRequest: no legacy long-lived
	// service-account token Secret is created at all.
	config, err := ctrlcfg.GetConfig()
	require.NoError(t, err, "get Kubernetes config for the TokenRequest")
	kubeClient, err := kubernetes.NewForConfig(config)
	require.NoError(t, err, "create clientset for the TokenRequest")
	expiration := int64(time.Hour / time.Second)
	tokenRequest, err := kubeClient.CoreV1().ServiceAccounts(tc.AppsNamespace).CreateToken(ctx, name,
		&authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &expiration}},
		metav1.CreateOptions{})
	require.NoError(t, err, "request a short-lived token for the probe ServiceAccount")
	require.NotEmpty(t, tokenRequest.Status.Token, "the TokenRequest should return a token")

	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-token", Namespace: tc.AppsNamespace},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{"token": tokenRequest.Status.Token},
	}
	// The mutate func is load-bearing: without it an existing Secret (left
	// behind by a hard-killed prior run whose cleanup never ran) keeps its
	// stale token and the probes authenticate with an expired credential
	// instead of the freshly requested one (verified by RED-check: a
	// pre-seeded garbage token made the bearer-authorization assertion fail
	// with auth redirects for the full 300s poll window).
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(tokenSecret),
		WithMutateFunc(testf.Mutate[*corev1.Secret](tc.Scheme(), func(s *corev1.Secret) error {
			s.Data = nil
			s.StringData = map[string]string{"token": tokenRequest.Status.Token}
			return nil
		})),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := tc.Client().Delete(cleanupCtx, tokenSecret); err != nil && !k8serr.IsNotFound(err) {
			t.Errorf("delete traffic probe token secret: %v", err)
		}
	})
	return tokenSecret.Name
}

// replayCookieAcrossShards logs in through shard a (mockCallback, including
// the Dex approval step), then force-replays a's genuine session cookie
// against shard b by attaching it as an explicit Cookie header - bypassing the
// (correct) browser behavior of domain-scoping the cookie, to prove shard b's
// proxy rejects a foreign session.
func (tc *GatewayTestCtx) replayCookieAcrossShards(t *testing.T, a, b shardEchoBackend) probeOutput {
	t.Helper()
	aIP := tc.shardRouterClusterIP(t, a.ingress.IngressControllerName)
	bIP := tc.shardRouterClusterIP(t, b.ingress.IngressControllerName)
	script := fmt.Sprintf(`set -u
LG=/dev/termination-log
HOSTA=%[1]s
IPA=%[2]s
HOSTB=%[3]s
IPB=%[4]s
P1=$(curl -ksS --noproxy '*' -L --connect-timeout 5 --max-time 60 \
  --resolve $HOSTA:443:$IPA -c /tmp/jar -o /tmp/bodyA \
  -w '%%{http_code} %%{url_effective}' https://$HOSTA%[5]s 2>>$LG) \
  || P1='000 curl-failed'
echo "P1 $P1" >> $LG
case "$P1" in
  *"/dex/approval"*)
    REQ=$(printf '%%s' "$P1" | sed -n 's/.*[?&]req=\([^&]*\).*/\1/p')
    curl -ksS --noproxy '*' -L --connect-timeout 5 --max-time 60 \
      --resolve $HOSTA:443:$IPA -b /tmp/jar -c /tmp/jar -o /tmp/bodyA \
      -w 'LOGIN %%{http_code} %%{url_effective}\n' \
      "$(printf '%%s' "$P1" | sed 's/^[^ ]* //')" -d "req=$REQ" -d 'approval=approve' >> $LG 2>&1 \
      || echo 'LOGIN 000 curl-failed' >> $LG
    ;;
  *)
    echo "LOGIN $P1" >> $LG
    ;;
esac
echo "LOGIN_BODY $(head -c 120 /tmp/bodyA 2>/dev/null | tr -d '\r\n')" >> $LG
echo "COOKIE_NAMES $(awk '/^#HttpOnly_/ || !/^#/ {if (NF>=7) printf "%%s ", $6}' /tmp/jar | head -c 200)" >> $LG
COOKIE=$(awk '/^#HttpOnly_/ || !/^#/ {if (NF>=7) printf "%%s=%%s; ", $6, $7}' /tmp/jar)
curl -ksS --noproxy '*' --connect-timeout 5 --max-time 30 \
  --resolve $HOSTB:443:$IPB -H "Cookie: $COOKIE" -o /tmp/bodyB \
  -w 'REPLAY %%{http_code} %%{redirect_url}\n' https://$HOSTB%[5]s >> $LG 2>&1 \
  || echo 'REPLAY 000 curl-failed' >> $LG
echo "REPLAY_BODY $(head -c 120 /tmp/bodyB 2>/dev/null | tr -d '\r\n')" >> $LG
`, a.ingress.Hostname, aIP, b.ingress.Hostname, bIP, echoRoutePath)
	return tc.runCurlProbePod(t, script, "P1")
}

// requireShardAuthReady waits until every shard reports AuthenticationReady.
func (tc *GatewayTestCtx) requireShardAuthReady(t *testing.T, fixture *notebookIngressFixture) {
	t.Helper()
	for _, ingress := range fixture.ingresses {
		tc.requireIngressAuthenticationReady(t, ingress.Name)
	}
}

// requireRouteAdmittedAtCurrentGeneration waits until the named HTTPRoute is
// parented on the given Gateway and reports Accepted and ResolvedRefs True for
// the route's current generation - the status must describe the route as it
// exists now, not a stale generation.
func (tc *GatewayTestCtx) requireRouteAdmittedAtCurrentGeneration(t *testing.T, key types.NamespacedName, parentGateway string) {
	t.Helper()
	g := NewWithT(t)
	ctx := tc.Context()
	g.Eventually(func(g Gomega) {
		route := &gwapiv1.HTTPRoute{}
		g.Expect(tc.Client().Get(ctx, key, route)).To(Succeed())
		g.Expect(route.Spec.ParentRefs).To(HaveLen(1))
		g.Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal(parentGateway),
			"HTTPRoute %s must parent on Gateway %s", key.Name, parentGateway)
		g.Expect(route.Status.Parents).To(HaveLen(1))
		accepted, resolved := false, false
		for _, condition := range route.Status.Parents[0].Conditions {
			switch condition.Type {
			case string(gwapiv1.RouteConditionAccepted):
				accepted = condition.Status == metav1.ConditionTrue
				g.Expect(condition.ObservedGeneration).To(Equal(route.Generation),
					"Accepted for %s must be reported for the current route generation", key.Name)
			case string(gwapiv1.RouteConditionResolvedRefs):
				resolved = condition.Status == metav1.ConditionTrue
				g.Expect(condition.ObservedGeneration).To(Equal(route.Generation),
					"ResolvedRefs for %s must be reported for the current route generation", key.Name)
			}
		}
		g.Expect(accepted).To(BeTrue(), "HTTPRoute %s must be Accepted by %s", key.Name, parentGateway)
		g.Expect(resolved).To(BeTrue(), "HTTPRoute %s must have ResolvedRefs on %s", key.Name, parentGateway)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
}

// requireShardTrafficEvidence verifies, per shard, the resources a request
// through that shard traverses, for the current observed generation: the shard
// Gateway (HTTPS listener on 443, Accepted and Programmed), the bridge Route
// admitted by the shard's own IngressController, the shard's kube-auth-proxy
// Service (the ext_authz provider), and the callback and echo HTTPRoutes
// parented on the shard Gateway with Accepted/ResolvedRefs True for the current
// route generation. Run before the traffic phases so that they only execute
// on a fully-admitted path.
func (tc *GatewayTestCtx) requireShardTrafficEvidence(t *testing.T, fixture *notebookIngressFixture) {
	t.Helper()

	for _, ingress := range fixture.ingresses {
		// Shard Gateway with its terminating HTTPS listener on 443, accepted
		// and programmed for the current generation.
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.KubernetesGateway, types.NamespacedName{
				Name:      ingress.Name,
				Namespace: tc.gatewayNamespace(),
			}),
			WithCondition(And(
				jq.Match(`.spec.listeners[] | select(.name == "%s") | .port == %d and .protocol == "%s"`,
					gateway.DefaultGatewayListenerName, gateway.StandardHTTPSPort, gwapiv1.HTTPSProtocolType),
				jq.Match(`.metadata.generation as $gen | .status.conditions[] | select(.type == "%s") | .status == "%s" and .observedGeneration == $gen`,
					string(gwapiv1.GatewayConditionAccepted), string(metav1.ConditionTrue)),
				jq.Match(`.metadata.generation as $gen | .status.conditions[] | select(.type == "%s") | .status == "%s" and .observedGeneration == $gen`,
					string(gwapiv1.GatewayConditionProgrammed), string(metav1.ConditionTrue)),
			)),
			WithEventuallyTimeout(tc.TestTimeouts.authGatewayTimeout),
			WithCustomErrorMsg("shard Gateway for ingress %s should have an HTTPS listener on 443 and be Accepted and Programmed for the current generation", ingress.Name),
		)

		// Bridge Route admitted by the shard's own IngressController (the
		// only router serving the shard hostname) and the shard's
		// kube-auth-proxy Service, the ext_authz provider every request
		// through the Gateway is checked against.
		g := NewWithT(t)
		ctx := tc.Context()
		g.Eventually(func(g Gomega) {
			bridge := &routev1.Route{}
			g.Expect(tc.Client().Get(ctx, types.NamespacedName{Name: ingress.Name, Namespace: tc.gatewayNamespace()}, bridge)).To(Succeed())
			g.Expect(routeAdmittedByRouter(bridge, ingress.Hostname, ingress.IngressControllerName)).To(BeTrue(),
				"bridge Route for ingress %s must be admitted by its own IngressController %s", ingress.Name, ingress.IngressControllerName)
			proxyService := &corev1.Service{}
			g.Expect(tc.Client().Get(ctx, types.NamespacedName{Name: perIngressProxyName(ingress.Name), Namespace: tc.gatewayNamespace()}, proxyService)).To(Succeed())
		}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())

		// The callback HTTPRoute that returns the login through the shard
		// hostname, and the echo route serving the traffic assertions, both
		// admitted for their current generation.
		tc.requireRouteAdmittedAtCurrentGeneration(t, types.NamespacedName{
			Name: perIngressCallbackRouteName(ingress.Name), Namespace: tc.gatewayNamespace(),
		}, ingress.Name)
		tc.requireRouteAdmittedAtCurrentGeneration(t, types.NamespacedName{
			Name: echoBackendPrefix(ingress.Name), Namespace: tc.AppsNamespace,
		}, ingress.Name)
	}
}

// ValidateAdditionalIngressOIDCTraffic drives authenticated traffic through
// the shards on BYOIDC clusters with the GatewayConfig pointed at the in-cluster
// Dex issuer: per-shard traffic evidence for the current observed generation
// (Gateway, bridge Route, auth-proxy Service, callback and echo routes),
// unauthenticated requests redirected to Dex with each shard's own client
// identity, a complete mockCallback login through EVERY shard returning via
// that shard's callback route to only its backend, and bearer ServiceAccount
// tokens reaching each shard's own backend (documented TP behavior).
func (tc *GatewayTestCtx) ValidateAdditionalIngressOIDCTraffic(t *testing.T) {
	t.Helper()

	tc.SkipIfXKSCluster(t)
	skipUnless(t, Tier1)
	tc.SkipUnlessBYOIDC(t)
	if !tc.isOcpRouteMode(t) {
		t.Skip("Skipping test: requires OcpRoute ingress mode")
	}
	t.Log("Validating additional ingress OIDC traffic")

	fixture := newNotebookIngressFixture(t, tc)
	backends := tc.deployShardEchoBackends(t, fixture)
	tc.ensureDexForOCP(t, tc.getExpectedGatewayHostname(t), fixture.ingresses)
	tc.configureGatewayConfigForOIDC(t, fixture)
	tc.requireShardAuthReady(t, fixture)

	// Step 0: per-shard traffic evidence for the current observed generation,
	// asserted before any traffic flows so the traffic phases only run on a
	// fully-admitted path: shard Gateway (HTTPS listener on 443, Accepted and
	// Programmed), bridge Route admitted by the shard's own router, the
	// shard's kube-auth-proxy Service, and the callback and echo HTTPRoutes
	// Accepted/ResolvedRefs for the current route generation.
	tc.requireShardTrafficEvidence(t, fixture)

	// Step 1: unauthenticated requests redirect to Dex with the shard's own
	// client identity and same-hostname callback, and never reach the backend.
	for i, ingress := range fixture.ingresses {
		g := NewWithT(t)
		g.Eventually(func(g Gomega) {
			out := tc.requestViaShard(t, ingress.IngressControllerName, ingress.Hostname)
			code, redirect := out.statusAndURL("STATUS")
			g.Expect(code).To(Equal("302"), "unauthenticated request via %s should redirect, raw:\n%s", ingress.Hostname, out.redacted())
			g.Expect(redirect).To(ContainSubstring(ocpDexIssuerURL+"/auth"),
				"redirect should target the Dex authorize endpoint, got %s", redirect)
			g.Expect(redirect).To(ContainSubstring("client_id="+fixtureOIDCClientID(ingress.Name)),
				"redirect should carry shard %s's own client id, got %s", ingress.Name, redirect)
			g.Expect(redirect).To(ContainSubstring(url.QueryEscape("https://"+ingress.Hostname+gateway.OAuthCallbackPath)),
				"redirect should return via the shard hostname callback, got %s", redirect)
			g.Expect(out.lines["BODY"]).NotTo(ContainSubstring(backends[i].marker),
				"unauthenticated request must not reach shard %s's backend", ingress.Name)
		}, tc.TestTimeouts.authGatewayTimeout, 5*time.Second).Should(Succeed())
	}

	// Step 2: complete login via Dex mockCallback through EVERY shard (through
	// Dex's approval screen, which approval_prompt=force requires); each
	// login returns via that shard's own callback route and hostname, reaches
	// only that shard's backend, and establishes that shard's session cookie.
	for _, backend := range backends {
		g := NewWithT(t)
		g.Eventually(func(g Gomega) {
			out := tc.loginViaShard(t, backend.ingress.IngressControllerName, backend.ingress.Hostname)
			code, _ := out.statusAndURL("STATUS")
			g.Expect(code).To(Equal("200"), "login through %s should complete and reach the backend, raw:\n%s", backend.ingress.Hostname, out.redacted())
			g.Expect(out.lines["BODY"]).To(ContainSubstring(backend.marker),
				"authenticated request should reach only shard %s's backend, got %q", backend.ingress.Name, out.lines["BODY"])
			g.Expect(out.lines["COOKIES"]).To(ContainSubstring(perIngressCookieName(backend.ingress.Name)),
				"login should establish a session cookie for shard %s, got %q", backend.ingress.Name, out.lines["COOKIES"])
		}, tc.TestTimeouts.authGatewayTimeout, 5*time.Second).Should(Succeed())
	}

	// Step 4: bearer ServiceAccount tokens reach each shard's own backend.
	tokenSecret := tc.createShardTrafficTokenSecret(t)
	for i, ingress := range fixture.ingresses {
		g := NewWithT(t)
		g.Eventually(func(g Gomega) {
			out := tc.requestViaShardWithToken(t, ingress.IngressControllerName, ingress.Hostname, tokenSecret)
			code, _ := out.statusAndURL("STATUS")
			g.Expect(code).To(Equal("200"), "bearer request via %s should be authorized, raw:\n%s", ingress.Hostname, out.redacted())
			g.Expect(out.lines["BODY"]).To(ContainSubstring(backends[i].marker),
				"bearer request should reach only shard %s's backend, got %q", ingress.Name, out.lines["BODY"])
		}, tc.TestTimeouts.authGatewayTimeout, 5*time.Second).Should(Succeed())
	}
}

// ValidateAdditionalIngressOIDCCookieIsolation proves cross-ingress session
// isolation (Tier2): a genuine session cookie obtained by logging in through
// shard alpha is force-replayed against shard beta and must be rejected - beta
// answers with a fresh login redirect and never serves its backend. Evidence
// (Gateway listeners, IngressControllers, bridge Routes, HTTPRoutes, auth
// statuses) is dumped by the fixture cleanup on failure.
func (tc *GatewayTestCtx) ValidateAdditionalIngressOIDCCookieIsolation(t *testing.T) {
	t.Helper()

	tc.SkipIfXKSCluster(t)
	skipUnless(t, Tier2)
	tc.SkipUnlessBYOIDC(t)
	if !tc.isOcpRouteMode(t) {
		t.Skip("Skipping test: requires OcpRoute ingress mode")
	}
	t.Log("Validating additional ingress OIDC cookie isolation")

	fixture := newNotebookIngressFixture(t, tc)
	backends := tc.deployShardEchoBackends(t, fixture)
	tc.ensureDexForOCP(t, tc.getExpectedGatewayHostname(t), fixture.ingresses)
	tc.configureGatewayConfigForOIDC(t, fixture)
	tc.requireShardAuthReady(t, fixture)

	alpha, beta := backends[0], backends[1]
	g := NewWithT(t)
	g.Eventually(func(g Gomega) {
		out := tc.replayCookieAcrossShards(t, alpha, beta)

		// Preconditions: the login through alpha genuinely succeeded and
		// established a real session cookie.
		loginCode, _ := out.statusAndURL("LOGIN")
		g.Expect(loginCode).To(Equal("200"), "login through %s should complete, raw:\n%s", alpha.ingress.Hostname, out.redacted())
		g.Expect(out.lines["LOGIN_BODY"]).To(ContainSubstring(alpha.marker))
		g.Expect(out.lines["COOKIE_NAMES"]).To(ContainSubstring(perIngressCookieName(alpha.ingress.Name)),
			"a genuine alpha session cookie should exist, got %q", out.lines["COOKIE_NAMES"])

		// The replay against beta must be rejected: beta's proxy does not
		// honor alpha's session (distinct cookie names, secrets and domains),
		// so it answers with its own login redirect and never reaches the
		// backend.
		replayCode, replayRedirect := out.statusAndURL("REPLAY")
		g.Expect(replayCode).To(Equal("302"),
			"beta %s must reject alpha's session cookie, raw:\n%s", beta.ingress.Hostname, out.redacted())
		g.Expect(replayRedirect).To(ContainSubstring("client_id="+fixtureOIDCClientID(beta.ingress.Name)),
			"beta's rejection should redirect to its own login, got %s", replayRedirect)
		g.Expect(out.lines["REPLAY_BODY"]).NotTo(ContainSubstring(beta.marker),
			"beta's backend must not serve a replayed alpha session")
	}, tc.TestTimeouts.authGatewayTimeout, 5*time.Second).Should(Succeed())
}

// rotationProbeCollectTimeout bounds the collect wait for the rotation probe:
// its internal deadlines sum to ~13 minutes (two retried logins at 4 minutes
// each, a 5-minute rejection poll, plus the handshake and replay requests).
const rotationProbeCollectTimeout = 18 * time.Minute

// rotationProbeScript builds the single-process credential-rotation probe.
// The pre-rotation session cookie value never leaves the probe process: no
// LOGIN_COOKIE transport through pod status and no Cookie header in a replay
// pod's args (CWE-532). The probe:
//
//  1. logs in through the shard (retrying until its deadline),
//  2. signals readiness with an authenticated request on a distinctive echo
//     path — the test watches the echo backend's logs for it,
//  3. polls with the pre-rotation cookie until the shard rejects it,
//  4. re-logs in post-rotation,
//  5. compares the two cookies in-shell, reporting only the boolean,
//  6. replays the pre-rotation cookie and reports the rejection.
//
// Every emitted line is value-free: status codes, cookie names and
// comparison booleans only. Any failure exits non-zero after reporting, so
// the collect step fails loudly instead of parsing silence.
func (tc *GatewayTestCtx) rotationProbeScript(t *testing.T, backend shardEchoBackend, cookieName, handshakePath string) string {
	t.Helper()
	routerIP := tc.shardRouterClusterIP(t, backend.ingress.IngressControllerName)
	return fmt.Sprintf(`set -u
LG=/dev/termination-log
HOST=%[1]s
IP=%[2]s
COOKIE=%[3]s
HS=%[4]s
EP=%[5]s

# Curl stderr goes to a scratch file, not the termination log: the kubelet
# keeps only the last 4096 bytes of the termination message, and retried
# transient errors during Dex startup or the proxy roll would otherwise
# evict the result labels. One bounded summary line is emitted on exit.
trap 'echo "CURL_ERR $(tail -c 300 /tmp/curl.err 2>/dev/null | tr "\n" "|")" >> $LG' EXIT

# login performs the full OIDC round-trip (redirect chain, Dex approval
# form) into the given cookie jar, retrying until the deadline. It emits no
# labeled output; callers report the outcome.
login() {
  JAR=$1
  rm -f "$JAR" /tmp/body
  END=$(( $(date +%%s) + %[6]d ))
  while [ $(date +%%s) -lt $END ]; do
    P1=$(curl -ksS --noproxy '*' -L --connect-timeout 5 --max-time 60 \
      --resolve $HOST:443:$IP -c "$JAR" -o /tmp/body \
      -w '%%{http_code} %%{url_effective}' https://$HOST$EP 2>>/tmp/curl.err) \
      || P1='000 curl-failed'
    R=$P1
    case "$P1" in
      *"/dex/approval"*)
        REQ=$(printf '%%s' "$P1" | sed -n 's/.*[?&]req=\([^&]*\).*/\1/p')
        R=$(curl -ksS --noproxy '*' -L --connect-timeout 5 --max-time 60 \
          --resolve $HOST:443:$IP -b "$JAR" -c "$JAR" -o /tmp/body \
          -w '%%{http_code} %%{url_effective}' \
          "$(printf '%%s' "$P1" | sed 's/^[^ ]* //')" -d "req=$REQ" -d 'approval=approve' 2>>/tmp/curl.err) \
          || R='000 curl-failed'
        ;;
    esac
    case "$R" in
      200*) return 0 ;;
      *) sleep 5 ;;
    esac
  done
  return 1
}

# Phase 1: the pre-rotation login. The session cookie is extracted into a
# shell variable and never written anywhere.
if login /tmp/jar1; then
  echo "LOGIN1_OK yes" >> $LG
  echo "LOGIN1_BODY $(head -c 120 /tmp/body 2>/dev/null | tr -d '\r\n')" >> $LG
else
  echo "LOGIN1_OK no" >> $LG
  exit 1
fi
OLD=$(awk -v n="$COOKIE" '/^#HttpOnly_/ || !/^#/ {if (NF>=7 && $6==n) printf "%%s=%%s", $6, $7}' /tmp/jar1)
if [ -z "$OLD" ]; then
  echo "OLD_COOKIE_PRESENT no" >> $LG
  exit 1
fi
echo "OLD_COOKIE_PRESENT yes" >> $LG

# Phase 2: readiness handshake. An authenticated request on a distinctive
# path; the test watches the echo backend's access log for it before it
# rotates the credentials.
H=$(curl -ksS --noproxy '*' --connect-timeout 5 --max-time 30 \
  --resolve $HOST:443:$IP -b /tmp/jar1 -o /dev/null \
  -w '%%{http_code}' https://$HOST$HS 2>>/tmp/curl.err) || H=000
echo "HANDSHAKE_STATUS $H" >> $LG

# Phase 3: poll with the pre-rotation cookie until the shard rejects it.
# 200 = the session is still valid (rotation not in effect yet); 302/401/403
# = rejected; anything else (5xx during the proxy roll, 000) = keep waiting.
OBS=no
END=$(( $(date +%%s) + %[7]d ))
while [ $(date +%%s) -lt $END ]; do
  C=$(curl -ksS --noproxy '*' --connect-timeout 5 --max-time 15 \
    --resolve $HOST:443:$IP -H "Cookie: $OLD" -o /dev/null \
    -w '%%{http_code}' https://$HOST$EP 2>>/tmp/curl.err) || C=000
  case "$C" in
    200) sleep 5 ;;
    302|401|403) OBS=yes; echo "REJECTION_STATUS $C" >> $LG; break ;;
    *) sleep 5 ;;
  esac
done
if [ "$OBS" != yes ]; then
  echo "ROTATION_OBSERVED no" >> $LG
  exit 1
fi
echo "ROTATION_OBSERVED yes" >> $LG

# Phase 4: the post-rotation re-login, into a fresh jar.
if login /tmp/jar2; then
  echo "LOGIN2_OK yes" >> $LG
  echo "LOGIN2_BODY $(head -c 120 /tmp/body 2>/dev/null | tr -d '\r\n')" >> $LG
else
  echo "LOGIN2_OK no" >> $LG
  exit 1
fi
NEW=$(awk -v n="$COOKIE" '/^#HttpOnly_/ || !/^#/ {if (NF>=7 && $6==n) printf "%%s=%%s", $6, $7}' /tmp/jar2)
if [ -z "$NEW" ]; then
  echo "NEW_COOKIE_PRESENT no" >> $LG
  exit 1
fi
echo "NEW_COOKIE_PRESENT yes" >> $LG

# Phase 5: the two session cookies must differ. Compared in-shell: only the
# boolean leaves the probe, never a credential-bearing string.
if [ "$OLD" != "$NEW" ]; then
  echo "COOKIE_ROTATED yes" >> $LG
else
  echo "COOKIE_ROTATED no" >> $LG
  exit 1
fi

# Phase 6: replay the pre-rotation cookie. The shard must demand a fresh
# login instead of serving the stale session.
RP=$(curl -ksS --noproxy '*' --connect-timeout 5 --max-time 30 \
  --resolve $HOST:443:$IP -H "Cookie: $OLD" -o /tmp/rbody \
  -w '%%{http_code} %%{redirect_url}' https://$HOST$EP 2>>/tmp/curl.err) || RP='000 curl-failed'
echo "REPLAY $RP" >> $LG
echo "REPLAY_BODY $(head -c 120 /tmp/rbody 2>/dev/null | tr -d '\r\n')" >> $LG
`, backend.ingress.Hostname, routerIP, cookieName, handshakePath, echoRoutePath, 240, 300)
}

// podLogTail returns the last tail lines of a container's log, bounded to
// 256KiB. Used for the echo-backend handshake, which the probe pods cannot
// report themselves.
func podLogTail(kubeClient kubernetes.Interface, namespace, podName, containerName string, tail int64) (string, error) {
	const maxLogBytes int64 = 256 * 1024
	limit := maxLogBytes
	req := kubeClient.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container:  containerName,
		TailLines:  &tail,
		LimitBytes: &limit,
	})
	logCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream, err := req.Stream(logCtx)
	if err != nil {
		return "", fmt.Errorf("open log stream: %w", err)
	}
	defer func() {
		if closeErr := stream.Close(); closeErr != nil {
			log.Printf("close log stream: %v", closeErr)
		}
	}()
	data, err := io.ReadAll(io.LimitReader(stream, maxLogBytes))
	if err != nil {
		return "", fmt.Errorf("read log stream: %w", err)
	}
	return string(data), nil
}

// waitForEchoBackendRequest polls the echo backend's pod logs until the given
// request path appears — the handshake proving the rotation probe's
// authenticated pre-rotation login completed before the credentials Secret is
// deleted. The probe pod is watched too: its script exits non-zero before the
// handshake when the login fails (LOGIN1_OK no / OLD_COOKIE_PRESENT no), and
// in that case waiting is pointless — the redacted probe log is reported
// immediately instead of timing out on a handshake that will never come (the
// same fail-fast pattern as collectCurlProbePod).
func (tc *GatewayTestCtx) waitForEchoBackendRequest(t *testing.T, probe *corev1.Pod, ingressName, path string) {
	t.Helper()
	config, err := ctrlcfg.GetConfig()
	require.NoError(t, err, "get Kubernetes config for pod log access")
	kubeClient, err := kubernetes.NewForConfig(config)
	require.NoError(t, err, "create clientset for pod log access")
	g := NewWithT(t)
	g.Eventually(func(g Gomega) bool {
		current := &corev1.Pod{}
		g.Expect(tc.Client().Get(tc.Context(), client.ObjectKeyFromObject(probe), current)).To(Succeed())
		if current.Status.Phase == corev1.PodFailed {
			StopTrying(fmt.Sprintf("rotation probe failed before its handshake reached the echo backend, log:\n%s",
				redactProbeLog(probeTerminatedLog(current)))).Now()
		}
		pods := &corev1.PodList{}
		g.Expect(tc.Client().List(tc.Context(), pods,
			client.InNamespace(tc.AppsNamespace),
			client.MatchingLabels{"app": echoBackendPrefix(ingressName)},
		)).To(Succeed())
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				continue
			}
			logs, logErr := podLogTail(kubeClient, pod.Namespace, pod.Name, "echo", 200)
			if logErr != nil {
				continue
			}
			if strings.Contains(logs, path) {
				return true
			}
		}
		return false
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(BeTrue(),
		"the rotation probe's authenticated handshake should reach the echo backend (path %s)", path)
}

// ValidateAdditionalIngressCredentialRotationRecovery proves the credential
// rotation semantics end to end on BYOIDC clusters (Tier2): after the shard's
// credentials Secret is deleted out of band and the operator rotates it, only
// that shard's cookie secret changes (the default and sibling values are
// preserved byte-for-byte), a fresh login through the shard succeeds with the
// NEW cookie (re-login after cookie rotation is allowed), and the OLD session
// cookie is rejected. The whole rotation flow runs inside a single probe
// process so the session cookie value is never at rest in the cluster
// (CWE-532); RHOAIENG-102012 (multi-Gateway TLS and credential rotation) can
// lift rotationProbeScript directly.
func (tc *GatewayTestCtx) ValidateAdditionalIngressCredentialRotationRecovery(t *testing.T) {
	t.Helper()

	tc.SkipIfXKSCluster(t)
	skipUnless(t, Tier2)
	tc.SkipUnlessBYOIDC(t)
	if !tc.isOcpRouteMode(t) {
		t.Skip("Skipping test: requires OcpRoute ingress mode")
	}
	t.Log("Validating additional ingress credential rotation recovery")

	fixture := newNotebookIngressFixture(t, tc)
	backends := tc.deployShardEchoBackends(t, fixture)
	tc.ensureDexForOCP(t, tc.getExpectedGatewayHostname(t), fixture.ingresses)
	tc.configureGatewayConfigForOIDC(t, fixture)
	tc.requireShardAuthReady(t, fixture)
	tc.requireShardTrafficEvidence(t, fixture)

	g := NewWithT(t)
	ctx := tc.Context()
	alpha := backends[0]
	beta := fixture.ingresses[1]
	alphaCreds := perIngressCredsName(alpha.ingress.Name)
	cookieName := perIngressCookieName(alpha.ingress.Name)

	// Single-process rotation probe (CWE-532): the pre-rotation session
	// cookie never leaves the probe — no LOGIN_COOKIE transport through pod
	// status and no Cookie header in a replay pod's args. The probe logs in,
	// signals readiness on a distinctive echo subpath, polls with its
	// pre-rotation cookie until the shard rejects it, re-logs in, compares
	// the two cookies in-shell (reporting only the boolean) and replays the
	// pre-rotation cookie.
	handshakePath := echoRoutePath + "/rotation-handshake-" + alpha.ingress.Name
	probe := tc.createCurlProbePod(t, tc.rotationProbeScript(t, alpha, cookieName, handshakePath))

	// Rotation blast radius to preserve: the default and sibling cookie secrets.
	siblingCookies := map[string]string{
		kubeAuthProxyCredsName:         "",
		perIngressCredsName(beta.Name): "",
	}
	for credsName := range siblingCookies {
		value, err := tc.fetchCookieSecretValue(ctx, credsName)
		require.NoError(t, err, "credentials secret %s should exist", credsName)
		siblingCookies[credsName] = value
	}
	originalSecret, err := tc.fetchCookieSecretValue(ctx, alphaCreds)
	require.NoError(t, err, "alpha's credentials secret should exist")

	// The credentials Secret may only be deleted once the probe's
	// pre-rotation login is complete: the handshake request reaching the echo
	// backend proves it. Deleting earlier would let the probe capture a
	// post-rotation cookie and its rejection poll would run out its deadline
	// — the spec fails closed, it never passes vacuously.
	tc.waitForEchoBackendRequest(t, probe, alpha.ingress.Name, handshakePath)

	// Delete the shard's credentials Secret out of band, forcing a rotation.
	require.NoError(t, tc.Client().Delete(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: alphaCreds, Namespace: tc.gatewayNamespace()},
	}), "delete alpha's credentials Secret")
	g.Eventually(func() (string, error) {
		return tc.fetchCookieSecretValue(ctx, alphaCreds)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).ShouldNot(Equal(originalSecret),
		"alpha's cookie secret must rotate after its credentials Secret is deleted")
	for credsName, expected := range siblingCookies {
		value, fetchErr := tc.fetchCookieSecretValue(ctx, credsName)
		require.NoError(t, fetchErr, "credentials secret %s should exist", credsName)
		require.Equal(t, expected, value,
			"cookie secret of %s must be preserved by alpha's rotation", credsName)
	}

	// Collect the probe's value-free evidence and assert it.
	out := tc.collectCurlProbePod(t, probe, "LOGIN1_OK", rotationProbeCollectTimeout)
	g.Expect(out.lines["LOGIN1_OK"]).To(Equal("yes"),
		"the initial login through %s should complete, raw:\n%s", alpha.ingress.Hostname, out.redacted())
	g.Expect(out.lines["LOGIN1_BODY"]).To(ContainSubstring(alpha.marker),
		"the initial login should reach alpha's backend, got %q", out.lines["LOGIN1_BODY"])
	g.Expect(out.lines["OLD_COOKIE_PRESENT"]).To(Equal("yes"),
		"the initial login should establish the session cookie %s", cookieName)
	g.Expect(out.lines["HANDSHAKE_STATUS"]).To(Equal("200"),
		"the probe's authenticated handshake should serve, got %q", out.lines["HANDSHAKE_STATUS"])
	g.Expect(out.lines["ROTATION_OBSERVED"]).To(Equal("yes"),
		"the shard should reject the pre-rotation session cookie once its credentials Secret is rotated, raw:\n%s", out.redacted())
	g.Expect(out.lines["LOGIN2_OK"]).To(Equal("yes"),
		"the re-login through %s after rotation should complete, raw:\n%s", alpha.ingress.Hostname, out.redacted())
	g.Expect(out.lines["LOGIN2_BODY"]).To(ContainSubstring(alpha.marker),
		"the re-login should reach alpha's backend, got %q", out.lines["LOGIN2_BODY"])
	g.Expect(out.lines["NEW_COOKIE_PRESENT"]).To(Equal("yes"),
		"the re-login should establish the session cookie %s", cookieName)
	g.Expect(out.lines["COOKIE_ROTATED"]).To(Equal("yes"),
		"the post-rotation session cookie must differ from the pre-rotation one (compared inside the probe; the values never leave it)")

	// The pre-rotation session cookie is rejected: the shard demands a fresh
	// login instead of serving the stale session.
	replayCode, replayRedirect := out.statusAndURL("REPLAY")
	g.Expect(replayCode).To(Equal("302"),
		"the pre-rotation session cookie must be rejected after rotation, raw:\n%s", out.redacted())
	g.Expect(replayRedirect).To(ContainSubstring("client_id="+fixtureOIDCClientID(alpha.ingress.Name)),
		"the rejection should redirect to the shard's own login, got %s", replayRedirect)
	g.Expect(out.lines["REPLAY_BODY"]).NotTo(ContainSubstring(alpha.marker),
		"the stale session must not reach alpha's backend, got %q", out.lines["REPLAY_BODY"])
}

// ValidateAdditionalIngressOAuthTraffic drives traffic through the shards on
// IntegratedOAuth clusters (the default customer configuration): unauthenticated
// requests redirect to the cluster OAuth server with each shard's own
// data-science-<ingress> client identity and same-hostname callback, and bearer
// ServiceAccount tokens reach each shard's own backend.
func (tc *GatewayTestCtx) ValidateAdditionalIngressOAuthTraffic(t *testing.T) {
	t.Helper()

	tc.SkipIfXKSCluster(t)
	skipUnless(t, Tier1)
	tc.SkipIfBYOIDC(t)
	if !tc.isOcpRouteMode(t) {
		t.Skip("Skipping test: requires OcpRoute ingress mode")
	}
	t.Log("Validating additional ingress OAuth traffic")

	fixture := newNotebookIngressFixture(t, tc)
	backends := tc.deployShardEchoBackends(t, fixture)
	tc.requireShardAuthReady(t, fixture)

	// Per-shard redirect identity: the OAuthClient the operator registered for
	// the shard, and a callback that returns via the shard's own hostname.
	for i, ingress := range fixture.ingresses {
		g := NewWithT(t)
		g.Eventually(func(g Gomega) {
			out := tc.requestViaShard(t, ingress.IngressControllerName, ingress.Hostname)
			code, redirect := out.statusAndURL("STATUS")
			g.Expect(code).To(Equal("302"), "unauthenticated request via %s should redirect, raw:\n%s", ingress.Hostname, out.redacted())
			g.Expect(redirect).To(ContainSubstring("/oauth/authorize"),
				"redirect should target the cluster OAuth server, got %s", redirect)
			g.Expect(redirect).To(ContainSubstring("client_id="+perIngressOAuthClientName(ingress.Name)),
				"redirect should carry shard %s's own OAuthClient id, got %s", ingress.Name, redirect)
			g.Expect(redirect).To(ContainSubstring(url.QueryEscape("https://"+ingress.Hostname+gateway.OAuthCallbackPath)),
				"redirect should return via the shard hostname callback, got %s", redirect)
			g.Expect(out.lines["BODY"]).NotTo(ContainSubstring(backends[i].marker),
				"unauthenticated request must not reach shard %s's backend", ingress.Name)
		}, tc.TestTimeouts.authGatewayTimeout, 5*time.Second).Should(Succeed())
	}

	// Bearer ServiceAccount tokens reach each shard's own backend through the
	// full shard path (router -> Gateway ext_authz -> backend).
	tokenSecret := tc.createShardTrafficTokenSecret(t)
	for i, ingress := range fixture.ingresses {
		g := NewWithT(t)
		g.Eventually(func(g Gomega) {
			out := tc.requestViaShardWithToken(t, ingress.IngressControllerName, ingress.Hostname, tokenSecret)
			code, _ := out.statusAndURL("STATUS")
			g.Expect(code).To(Equal("200"), "bearer request via %s should be authorized, raw:\n%s", ingress.Hostname, out.redacted())
			g.Expect(out.lines["BODY"]).To(ContainSubstring(backends[i].marker),
				"bearer request should reach only shard %s's backend, got %q", ingress.Name, out.lines["BODY"])
		}, tc.TestTimeouts.authGatewayTimeout, 5*time.Second).Should(Succeed())
	}
}

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

// RHOAIENG-94382: Notebook ingress assignment matrix.
//
// The odh-notebook-controller parents every Notebook HTTPRoute on the Gateway named
// by the namespace annotation opendatahub.io/ingress-name (absent = default Gateway).
// Unknown names delete the route with a Warning IngressNotFound event instead of
// falling back to the default Gateway. These specs pin that matrix on OCP with two
// real, selector-isolated, private IngressControllers:
//
//   - default assignment without annotation (Smoke/Tier1)
//   - valid assignment, admission, dataplane and annotation convergence (Tier1)
//   - unknown and removed ingress names: no fallback, recovery (Tier2)
//   - known-but-unready Gateway blocks admission until usable; controller restart
//     converges without recreation (Tier2)
//   - projected contract ConfigMap lifecycle: out-of-band deletion is healed,
//     stale entries dropped on ingress removal (Tier2, 94382 amendment)
//   - malformed/missing projected contract data: DEFERRED to RHOAIENG-102011
//     (contract v1) - red against the shipped build by design because the
//     contract heals before the failure is observable; opt in via
//     E2E_TEST_RUN_DEFERRED_CONTRACT_V1=true (Tier2)
//   - notebook controller RBAC isolation: no GatewayConfig/Workbenches access (Tier1)
//   - Gateway attachment permissions: allowedRoutes admits only the gateway and
//     applications namespaces; foreign namespaces are not admitted (Tier2)
//   - backend ReferenceGrants: grant present for valid assignments, deletion
//     is acted upon without default-Gateway fallback or workbench recreation,
//     restore recovers (Tier2)
//
// The fixtures (newNotebookIngressFixture / createAnnotatedNotebook) are also the
// shared substrate for the additional-ingress auth and traffic specs
// (RHOAIENG-94376/96913).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/opendatahub-io/odh-platform-utilities/framework/metadata/annotations"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

// notebookTestImage is the same image the odh-notebook-controller standalone e2e
// uses, so it is already exercised (and typically cached) on qualification clusters.
const notebookTestImage = "quay.io/thoth-station/s2i-minimal-notebook:v0.2.2"

// Fixture-owned OIDC identities for BYOIDC clusters. When the GatewayConfig
// already carries spec.oidc, the admission webhook rejects additional ingresses
// without auth.oidc ("mixed authentication modes"), so the fixture assigns each
// shard a distinct test client ID plus an administrator-style client Secret in
// the gateway namespace. The identities are valid against any issuer: client
// registration only matters at token exchange, which these tests never perform
// (the Dex bootstrap in gateway_dex_ocp_test.go registers matching clients by
// reading auth.oidc and these Secrets).
const fixtureOIDCClientSecretKey = "clientSecret"

// fixtureOIDCClientID returns the test OIDC client ID for an additional ingress.
func fixtureOIDCClientID(ingressName string) string {
	return "e2e-oidc-" + ingressName
}

// fixtureOIDCClientSecretName returns the test client Secret name (gateway
// namespace) carrying the OIDC client secret for an additional ingress.
func fixtureOIDCClientSecretName(ingressName string) string {
	return "e2e-oidc-client-" + ingressName
}

// randomClientSecret returns a random opaque client secret value.
func randomClientSecret(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 24)
	_, err := rand.Read(buf)
	require.NoError(t, err, "generate random client secret")
	return hex.EncodeToString(buf)
}

// injectFixtureOIDCIdentities assigns each ingress a test OIDC identity and
// creates the referenced client Secrets in the gateway namespace before the
// GatewayConfig starts referencing them. It appends the created Secret names to
// secretNames so the registered cleanup can delete them after the restore.
func (tc *GatewayTestCtx) injectFixtureOIDCIdentities(t *testing.T, ingresses []serviceApi.AdditionalIngress, secretNames *[]string) {
	t.Helper()
	namespace := tc.gatewayNamespace()

	for i := range ingresses {
		ingress := &ingresses[i]
		secretName := fixtureOIDCClientSecretName(ingress.Name)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				fixtureOIDCClientSecretKey: randomClientSecret(t),
			},
		}
		tc.EventuallyResourceCreatedOrUpdated(
			WithObjectToCreate(secret),
			WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
		)
		ingress.Auth.OIDC = &serviceApi.AdditionalIngressOIDCConfig{
			ClientID: fixtureOIDCClientID(ingress.Name),
			ClientSecretRef: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  fixtureOIDCClientSecretKey,
			},
			SecretNamespace: namespace,
		}
		*secretNames = append(*secretNames, secretName)
	}
}

// deleteFixtureOIDCIdentities removes the fixture-owned client Secrets.
func (tc *GatewayTestCtx) deleteFixtureOIDCIdentities(t *testing.T, secretNames []string) {
	t.Helper()
	cleanupCtx := tc.Context()
	namespace := tc.gatewayNamespace()
	for _, secretName := range secretNames {
		if err := tc.Client().Delete(cleanupCtx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name: secretName, Namespace: namespace,
		}}); err != nil && !k8serr.IsNotFound(err) {
			t.Errorf("delete fixture OIDC client secret %s: %v", secretName, err)
		}
	}
}

// notebookIngressFixture holds two additional ingresses backed by real,
// selector-isolated private IngressControllers, plus the state required to
// restore the cluster when the owning test finishes.
type notebookIngressFixture struct {
	gtc             *GatewayTestCtx
	original        *serviceApi.GatewayConfig
	originalGateway *gwapiv1.Gateway
	ingresses       []serviceApi.AdditionalIngress
	controllers     []types.NamespacedName
}

// newNotebookIngressFixture creates two additional ingresses backed by real,
// selector-isolated private IngressControllers, wires them into the GatewayConfig,
// and waits until both Gateways are admitted. It deliberately does not wait for
// the Workbenches projection: gateway- and resilience-suite specs assert only on
// the shards' auth stacks and run after the components suite may have set the
// Workbenches module to Removed. Specs that drive the notebook controller must
// call awaitNotebookProjection before relying on the projected contract.
func newNotebookIngressFixture(t *testing.T, gtc *GatewayTestCtx) *notebookIngressFixture {
	t.Helper()
	ctx := gtc.Context()

	original := &serviceApi.GatewayConfig{}
	require.NoError(t, gtc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, original))
	require.NotEmpty(t, original.Status.Domain)
	originalGateway, err := gtc.getGateway(ctx)
	require.NoError(t, err)

	fixture := &notebookIngressFixture{
		gtc:             gtc,
		original:        original,
		originalGateway: originalGateway,
	}
	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	names := make([]string, 0, 2)
	for _, shard := range []string{"alpha", "beta"} {
		controllerName := fmt.Sprintf("e2e-nb-%s-%s", shard, suffix)
		fixture.ingresses = append(fixture.ingresses, serviceApi.AdditionalIngress{
			Name:                  controllerName,
			Hostname:              controllerName + ".e2e.invalid",
			IngressControllerName: controllerName,
			RouteLabels:           map[string]string{"example.com/ingress": controllerName},
		})
		fixture.controllers = append(fixture.controllers, types.NamespacedName{
			Name: controllerName, Namespace: cluster.IngressControllerName.Namespace,
		})
		names = append(names, controllerName)
	}

	// Register cleanups before creating anything, so an aborted setup still
	// restores state; evidence is dumped before the restore mutates it.
	// The identity-Secret cleanup is registered first (runs last, LIFO) so the
	// Secrets are deleted only after the GatewayConfig restore no longer
	// references them.
	oidcSecretNames := make([]string, 0, len(fixture.ingresses))
	t.Cleanup(func() { gtc.deleteFixtureOIDCIdentities(t, oidcSecretNames) })
	t.Cleanup(func() {
		restoreAdditionalIngressGatewayTest(t, gtc, fixture.original, fixture.originalGateway, names, fixture.controllers...)
	})
	t.Cleanup(func() { gtc.logAdditionalIngressEvidence(t, fixture.controllers...) })

	replicas := int32(1)
	for i, ingress := range fixture.ingresses {
		require.NoError(t, gtc.Client().Create(ctx, &operatorv1.IngressController{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fixture.controllers[i].Name,
				Namespace: fixture.controllers[i].Namespace,
			},
			Spec: operatorv1.IngressControllerSpec{
				Domain:   ingress.Hostname,
				Replicas: &replicas,
				EndpointPublishingStrategy: &operatorv1.EndpointPublishingStrategy{
					Type: operatorv1.PrivateStrategyType,
				},
				RouteSelector: &metav1.LabelSelector{MatchLabels: ingress.RouteLabels},
			},
		}), "create temporary private IngressController %s", fixture.controllers[i].Name)
	}

	// On BYOIDC clusters (GatewayConfig already carries spec.oidc), assign the
	// shards their test OIDC identities before the GatewayConfig references
	// them; on IntegratedOAuth clusters the ingresses stay plain and the
	// operator registers data-science-* OAuthClients instead.
	if original.Spec.OIDC != nil {
		gtc.injectFixtureOIDCIdentities(t, fixture.ingresses, &oidcSecretNames)
	}

	g := NewWithT(t)
	g.Eventually(func() error {
		return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			current := &serviceApi.GatewayConfig{}
			if err := gtc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, current); err != nil {
				return err
			}
			current.Spec.AdditionalIngresses = fixture.ingresses
			return gtc.Client().Update(ctx, current)
		})
	}).Should(Succeed())

	for _, ingress := range fixture.ingresses {
		gtc.validateAdditionalGatewayAndRoute(t, ingress)
		gtc.validateAdditionalGatewayStatus(t, len(fixture.ingresses), ingress.Name, ingress.Hostname)
		validateAdditionalIngressRouteAdmissionStatus(t, gtc,
			types.NamespacedName{Name: ingress.Name, Namespace: gtc.gatewayNamespace()},
			ingress.Name, ingress.Hostname, ingress.IngressControllerName)
	}

	return fixture
}

// awaitNotebookProjection waits until Workbenches has projected the fixture's
// ingresses into the notebook controller's contract. The notebook controller
// only knows ingresses after Workbenches projects them; creating annotated
// Notebooks before that would surface as IngressNotFound. Only Workbenches-
// suite specs need this — their suite runs while the Workbenches module is
// still enabled — so the wait lives here rather than in
// newNotebookIngressFixture, which must stay usable once later suites have
// set the module to Removed.
func (f *notebookIngressFixture) awaitNotebookProjection(t *testing.T) {
	t.Helper()

	projected := make([]OmegaMatcher, 0, len(f.ingresses)+1)
	projected = append(projected,
		jq.Match(`.spec.ingresses | length == %d`, len(f.ingresses)+1))
	for _, ingress := range f.ingresses {
		projected = append(projected, jq.Match(
			`.spec.ingresses | map(select(.name == "%s")) == [{name: "%s", gatewayName: "%s", gatewayNamespace: "%s", hostname: "%s"}]`,
			ingress.Name, ingress.Name, ingress.Name, f.gtc.gatewayNamespace(), ingress.Hostname))
	}
	f.gtc.EnsureResourceExists(
		WithMinimalObject(gvk.Workbenches, types.NamespacedName{Name: componentApi.WorkbenchesInstanceName}),
		WithCondition(And(projected...)),
		WithCustomErrorMsg("Workbenches should project all %d notebook-shard ingresses", len(f.ingresses)),
	)
}

// configureAdditionalIngresses exposes the fixture to Workbenches-suite specs
// and awaits the Workbenches projection those specs rely on.
func (tc *WorkbenchesTestCtx) configureAdditionalIngresses(t *testing.T) *notebookIngressFixture {
	t.Helper()
	fixture := newNotebookIngressFixture(t, &GatewayTestCtx{TestContext: tc.TestContext})
	fixture.awaitNotebookProjection(t)
	return fixture
}

// createAnnotatedNotebook creates a namespace carrying the ingress assignment
// annotation (ingressName empty = no annotation) and a running Notebook in it.
// The Notebook uses inject-auth so its auth sidecar answers unauthenticated
// requests (401 challenge or 302 login redirect), which doubles as dataplane
// proof for shard routing.
func (tc *WorkbenchesTestCtx) createAnnotatedNotebook(t *testing.T, namespace, name, ingressName string) types.NamespacedName {
	t.Helper()
	ctx := tc.Context()

	namespaceMeta := metav1.ObjectMeta{Name: namespace}
	if ingressName != "" {
		namespaceMeta.Annotations = map[string]string{annotations.IngressName: ingressName}
	}
	require.NoError(t, tc.Client().Create(ctx, &corev1.Namespace{ObjectMeta: namespaceMeta}))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := tc.Client().Delete(cleanupCtx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}); err != nil && !k8serr.IsNotFound(err) {
			t.Errorf("delete notebook namespace %s: %v", namespace, err)
		}
	})

	notebook := &unstructured.Unstructured{}
	notebook.SetGroupVersionKind(gvk.Notebook)
	notebook.SetName(name)
	notebook.SetNamespace(namespace)
	notebook.SetAnnotations(map[string]string{"notebooks.opendatahub.io/inject-auth": "true"})
	notebook.Object["spec"] = map[string]any{
		"template": map[string]any{
			"spec": map[string]any{
				"containers": []any{map[string]any{
					"name":  name,
					"image": notebookTestImage,
					"ports": []any{map[string]any{
						"name":          "notebook-port",
						"containerPort": 8888,
						"protocol":      "TCP",
					}},
				}},
			},
		},
	}
	require.NoError(t, tc.Client().Create(ctx, notebook))

	key := types.NamespacedName{Namespace: namespace, Name: name}
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.StatefulSet, key),
		WithCondition(jq.Match(`.status.readyReplicas == 1`)),
		WithCustomErrorMsg("Notebook %s StatefulSet should become ready", key),
	)
	return key
}

// setNotebookNamespaceIngress sets or removes the ingress assignment annotation.
func (tc *WorkbenchesTestCtx) setNotebookNamespaceIngress(t *testing.T, namespace, ingressName string, present bool) {
	t.Helper()
	ctx := tc.Context()
	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		ns := &corev1.Namespace{}
		if err := tc.Client().Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
			return err
		}
		if ns.Annotations == nil {
			ns.Annotations = map[string]string{}
		}
		if present {
			ns.Annotations[annotations.IngressName] = ingressName
		} else {
			delete(ns.Annotations, annotations.IngressName)
		}
		return tc.Client().Update(ctx, ns)
	}))
}

// notebookRouteCount returns the number of central-namespace HTTPRoutes for the Notebook.
func (tc *WorkbenchesTestCtx) notebookRouteCount(notebook types.NamespacedName) (int, error) {
	routes := &gwapiv1.HTTPRouteList{}
	if err := tc.Client().List(tc.Context(), routes,
		client.InNamespace(tc.AppsNamespace),
		client.MatchingLabels{"notebook-name": notebook.Name, "notebook-namespace": notebook.Namespace},
	); err != nil {
		return 0, err
	}
	return len(routes.Items), nil
}

// getNotebookHTTPRoute returns the Notebook's central-namespace HTTPRoute.
func (tc *WorkbenchesTestCtx) getNotebookHTTPRoute(ctx context.Context, notebook types.NamespacedName) (*gwapiv1.HTTPRoute, error) {
	routes := &gwapiv1.HTTPRouteList{}
	if err := tc.Client().List(ctx, routes,
		client.InNamespace(tc.AppsNamespace),
		client.MatchingLabels{"notebook-name": notebook.Name, "notebook-namespace": notebook.Namespace},
	); err != nil {
		return nil, err
	}
	if len(routes.Items) != 1 {
		return nil, fmt.Errorf("expected 1 HTTPRoute for Notebook %s, got %d", notebook, len(routes.Items))
	}
	return &routes.Items[0], nil
}

// requireNotebookParent waits until the Notebook's HTTPRoute is parented on the
// named Gateway, and (when admitted is set) until the parent reports Accepted and
// ResolvedRefs True for the route's current generation.
func (tc *WorkbenchesTestCtx) requireNotebookParent(t *testing.T, notebook types.NamespacedName, gatewayName string, admitted bool) {
	t.Helper()
	ctx := tc.Context()
	g := NewWithT(t)
	g.Eventually(func(g Gomega) {
		route, err := tc.getNotebookHTTPRoute(ctx, notebook)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(route.Spec.ParentRefs).To(HaveLen(1))
		g.Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal(gatewayName))
		g.Expect(route.Spec.ParentRefs[0].Namespace).NotTo(BeNil())
		g.Expect(string(*route.Spec.ParentRefs[0].Namespace)).To(Equal(gateway.GetGatewayNamespace()))

		if !admitted {
			return
		}
		g.Expect(route.Status.Parents).To(HaveLen(1))
		accepted, resolved := false, false
		for _, condition := range route.Status.Parents[0].Conditions {
			switch condition.Type {
			case "Accepted":
				accepted = condition.Status == metav1.ConditionTrue
				g.Expect(condition.ObservedGeneration).To(Equal(route.Generation),
					"Accepted must be reported for the current route generation")
			case "ResolvedRefs":
				resolved = condition.Status == metav1.ConditionTrue
				g.Expect(condition.ObservedGeneration).To(Equal(route.Generation),
					"ResolvedRefs must be reported for the current route generation")
			}
		}
		g.Expect(accepted).To(BeTrue(), "HTTPRoute for %s must be Accepted by %s", notebook, gatewayName)
		g.Expect(resolved).To(BeTrue(), "HTTPRoute for %s must have ResolvedRefs on %s", notebook, gatewayName)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
}

// requireNotebookRouteGone waits until the Notebook has no HTTPRoute: the controller
// deletes it on unresolvable assignments instead of falling back to any Gateway.
func (tc *WorkbenchesTestCtx) requireNotebookRouteGone(t *testing.T, notebook types.NamespacedName) {
	t.Helper()
	g := NewWithT(t)
	g.Eventually(func(g Gomega) {
		count, err := tc.notebookRouteCount(notebook)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(count).To(BeZero(), "Notebook %s must have no HTTPRoute while its ingress is unresolvable", notebook)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
}

// requireIngressNotFoundEvent waits for the Warning IngressNotFound event on the Notebook.
func (tc *WorkbenchesTestCtx) requireIngressNotFoundEvent(t *testing.T, notebook types.NamespacedName) {
	t.Helper()
	g := NewWithT(t)
	g.Eventually(func(g Gomega) {
		events := &corev1.EventList{}
		g.Expect(tc.Client().List(tc.Context(), events,
			client.InNamespace(notebook.Namespace),
			client.MatchingFields{"involvedObject.name": notebook.Name, "involvedObject.kind": "Notebook"},
		)).To(Succeed())
		found := false
		for _, event := range events.Items {
			if event.Reason == "IngressNotFound" && event.Type == corev1.EventTypeWarning {
				found = true
			}
		}
		g.Expect(found).To(BeTrue(), "Notebook %s should emit a Warning IngressNotFound event", notebook)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
}

// requestThroughShardRouter runs an in-cluster HTTPS request to path with the shard
// hostname resolved against the shard router's service, and reports the HTTP status
// line via the probe pod's termination log. Private IngressControllers are only
// reachable from inside the cluster.
func (tc *WorkbenchesTestCtx) requestThroughShardRouter(t *testing.T, controllerName, hostname, path string) string {
	t.Helper()
	ctx := tc.Context()

	services := &corev1.ServiceList{}
	require.NoError(t, tc.Client().List(ctx, services,
		client.InNamespace("openshift-ingress"),
		client.MatchingLabels{"ingresscontroller.operator.openshift.io/owning-ingresscontroller": controllerName},
	))
	require.NotEmpty(t, services.Items, "no router service found for IngressController %s", controllerName)
	routerIP := services.Items[0].Spec.ClusterIP
	require.NotEmpty(t, routerIP)

	probe := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "notebook-shard-probe-",
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
				Args: []string{fmt.Sprintf(`
set -eu
code="$(curl --noproxy '*' -ksS --connect-timeout 5 --max-time 20 -o /dev/null -w '%%{http_code} %%{redirect_url}' --resolve %s:443:%s https://%s%s)"
echo "HTTP ${code}" > /dev/termination-log
`, hostname, routerIP, hostname, path)},
			}},
		},
	}
	require.NoError(t, tc.Client().Create(ctx, probe))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := tc.Client().Delete(cleanupCtx, probe); err != nil && !k8serr.IsNotFound(err) {
			t.Errorf("delete shard probe pod: %v", err)
		}
	})

	g := NewWithT(t)
	g.Eventually(func(g Gomega) {
		current := &corev1.Pod{}
		g.Expect(tc.Client().Get(ctx, types.NamespacedName{Name: probe.Name, Namespace: probe.Namespace}, current)).To(Succeed())
		g.Expect(current.Status.Phase).To(Equal(corev1.PodSucceeded), "shard probe pod should succeed")
		for _, status := range current.Status.ContainerStatuses {
			if status.State.Terminated != nil {
				g.Expect(status.State.Terminated.ExitCode).To(BeZero())
				g.Expect(status.State.Terminated.Message).To(ContainSubstring("HTTP "), "probe should report an HTTP status")
			}
		}
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())

	current := &corev1.Pod{}
	require.NoError(t, tc.Client().Get(ctx, types.NamespacedName{Name: probe.Name, Namespace: probe.Namespace}, current))
	for _, status := range current.Status.ContainerStatuses {
		if status.State.Terminated != nil {
			return status.State.Terminated.Message
		}
	}
	return ""
}

// skipNotebookIngressAssignment guards shared preconditions: the matrix needs
// OpenShift Route mode (private IngressControllers and bridge Routes) and is not
// meaningful on BYOIDC clusters.
func (tc *WorkbenchesTestCtx) skipNotebookIngressAssignment(t *testing.T) {
	t.Helper()
	tc.SkipIfXKSCluster(t)
	tc.SkipIfBYOIDC(t)

	config := &serviceApi.GatewayConfig{}
	require.NoError(t, tc.Client().Get(tc.Context(), types.NamespacedName{Name: gatewayConfigName}, config))
	if config.Spec.IngressMode != serviceApi.IngressModeOcpRoute {
		t.Skipf("Notebook ingress assignment matrix requires OcpRoute mode, cluster is in %s", config.Spec.IngressMode)
	}
}

// ValidateNotebookIngressDefaultAssignment pins the Smoke-level default: without the
// namespace annotation (and without any additional ingresses configured), Notebooks
// keep parenting their HTTPRoute on the default Gateway.
func (tc *WorkbenchesTestCtx) ValidateNotebookIngressDefaultAssignment(t *testing.T) {
	t.Helper()
	skipUnless(t, Smoke, Tier1)
	tc.skipNotebookIngressAssignment(t)

	notebook := tc.createAnnotatedNotebook(t, "e2e-notebook-default", "e2e-notebook-default", "")
	tc.requireNotebookParent(t, notebook, gateway.GetDefaultGatewayName(), true)
}

// ValidateNotebookIngressAssignmentMatrix covers the Tier1 matrix: annotated
// namespaces parent their Notebook HTTPRoutes on the selected shard Gateway with
// accepted routes; live traffic through the shard hostname reaches only the assigned
// Notebook; and annotation change/removal converge to the new parent.
func (tc *WorkbenchesTestCtx) ValidateNotebookIngressAssignmentMatrix(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)
	tc.skipNotebookIngressAssignment(t)

	fixture := tc.configureAdditionalIngresses(t)
	alpha, beta := fixture.ingresses[0], fixture.ingresses[1]

	alphaNotebook := tc.createAnnotatedNotebook(t, "e2e-notebook-alpha", "e2e-notebook-alpha", alpha.Name)
	betaNotebook := tc.createAnnotatedNotebook(t, "e2e-notebook-beta", "e2e-notebook-beta", beta.Name)

	tc.requireNotebookParent(t, alphaNotebook, alpha.Name, true)
	tc.requireNotebookParent(t, betaNotebook, beta.Name, true)

	// Dataplane proof: an in-cluster request through the alpha shard hostname
	// reaches the assigned Notebook and is answered by its auth sidecar — either a
	// 401 from kube-rbac-proxy or a 302 redirect to the OAuth login (IntegratedOAuth
	// mode redirects instead of challenging). Either response proves the request
	// traversed the shard router and Gateway; an unrouted hostname would surface as
	// the router's own 404.
	status := tc.requestThroughShardRouter(t, alpha.IngressControllerName, alpha.Hostname,
		fmt.Sprintf("/notebook/%s/%s/api", alphaNotebook.Namespace, alphaNotebook.Name))
	t.Logf("shard %s answered: %q", alpha.Hostname, status)
	require.True(t, strings.Contains(status, "HTTP 302") || strings.Contains(status, "HTTP 401"),
		"unauthenticated request through shard %s should be answered by the Notebook auth sidecar, got %q", alpha.Hostname, status)

	// The URL users see uses the shard hostname: in IntegratedOAuth mode the auth
	// sidecar redirects back to the shard's own callback (redirect_uri host =
	// shard hostname), and the Workbenches projection (asserted by the fixture)
	// publishes the same hostname for link building.
	if strings.Contains(status, "HTTP 302") {
		require.Contains(t, status, alpha.Hostname,
			"the auth redirect must target the shard hostname (displayed URL host)")
	}

	// Annotation change converges: alpha -> beta re-parents the HTTPRoute.
	tc.setNotebookNamespaceIngress(t, alphaNotebook.Namespace, beta.Name, true)
	tc.requireNotebookParent(t, alphaNotebook, beta.Name, true)

	// Annotation removal converges back to the default Gateway.
	tc.setNotebookNamespaceIngress(t, alphaNotebook.Namespace, "", false)
	tc.requireNotebookParent(t, alphaNotebook, gateway.GetDefaultGatewayName(), true)

	// The sibling keeps its assignment throughout.
	tc.requireNotebookParent(t, betaNotebook, beta.Name, true)
}

// ValidateNotebookIngressAssignmentFailures covers the Tier2 failure semantics for
// unresolvable names: an unknown ingress and an ingress removed from the
// GatewayConfig both delete the Notebook's HTTPRoute (never falling back to the
// default or a sibling Gateway) with a Warning IngressNotFound event, and correcting
// the assignment recreates the route on the right parent.
func (tc *WorkbenchesTestCtx) ValidateNotebookIngressAssignmentFailures(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier2)
	tc.skipNotebookIngressAssignment(t)

	fixture := tc.configureAdditionalIngresses(t)
	alpha := fixture.ingresses[0]

	notebook := tc.createAnnotatedNotebook(t, "e2e-notebook-failure", "e2e-notebook-failure", alpha.Name)
	tc.requireNotebookParent(t, notebook, alpha.Name, true)

	// Unknown ingress: the route must disappear (no default-Gateway fallback) and
	// the Notebook must surface a Warning IngressNotFound event; correcting the
	// annotation recreates the route on the right parent.
	tc.setNotebookNamespaceIngress(t, notebook.Namespace, "no-such-ingress", true)
	tc.requireNotebookRouteGone(t, notebook)
	tc.requireIngressNotFoundEvent(t, notebook)
	tc.setNotebookNamespaceIngress(t, notebook.Namespace, alpha.Name, true)
	tc.requireNotebookParent(t, notebook, alpha.Name, true)

	// Removed ingress: taking the shard out of the GatewayConfig while a namespace
	// still names it must behave exactly like an unknown ingress. The event
	// assertion is gated on the removal time: the unknown-ingress phase's
	// Warning event is still within its TTL and would otherwise satisfy a
	// second un-gated lookup without this phase emitting anything.
	removedAt := time.Now()
	g := NewWithT(t)
	g.Eventually(func() error {
		return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			current := &serviceApi.GatewayConfig{}
			if err := tc.Client().Get(tc.Context(), types.NamespacedName{Name: gatewayConfigName}, current); err != nil {
				return err
			}
			current.Spec.AdditionalIngresses = fixture.ingresses[1:]
			return tc.Client().Update(tc.Context(), current)
		})
	}).Should(Succeed())
	tc.requireNotebookRouteGone(t, notebook)
	tc.requireIngressNotFoundEventSince(tc.Context(), t, notebook, removedAt)
	g.Eventually(func() error {
		return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			current := &serviceApi.GatewayConfig{}
			if err := tc.Client().Get(tc.Context(), types.NamespacedName{Name: gatewayConfigName}, current); err != nil {
				return err
			}
			current.Spec.AdditionalIngresses = fixture.ingresses
			return tc.Client().Update(tc.Context(), current)
		})
	}).Should(Succeed())
	tc.requireNotebookParent(t, notebook, alpha.Name, true)
}

// ValidateNotebookIngressUnreadyGatewayAndRestart covers the remaining Tier2 cases:
// a known but unready Gateway (its IngressController does not exist yet) accepts the
// assignment but never admits the route until the Gateway becomes usable, and
// rolling odh-notebook-controller-manager converges without recreating the
// Notebook's StatefulSet or HTTPRoute.
func (tc *WorkbenchesTestCtx) ValidateNotebookIngressUnreadyGatewayAndRestart(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier2)
	tc.skipNotebookIngressAssignment(t)

	fixture := tc.configureAdditionalIngresses(t)
	gtc := fixture.gtc
	ctx := tc.Context()
	g := NewWithT(t)

	// gamma is a known ingress whose IngressController does not exist: the Gateway
	// is created but cannot become ready.
	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	gammaController := types.NamespacedName{
		Name:      fmt.Sprintf("e2e-nb-gamma-%s", suffix),
		Namespace: cluster.IngressControllerName.Namespace,
	}
	gamma := serviceApi.AdditionalIngress{
		Name:                  gammaController.Name,
		Hostname:              gammaController.Name + ".e2e.invalid",
		IngressControllerName: gammaController.Name,
		RouteLabels:           map[string]string{"example.com/ingress": gammaController.Name},
	}

	updateIngresses := func(additional serviceApi.AdditionalIngresses) {
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
	// gamma's IngressController is created by this spec below (the operator
	// watches IngressControllers but never owns them), so the spec must delete
	// it itself. Registered before gamma enters the GatewayConfig so the LIFO
	// cleanup order first removes gamma from the spec and only then deletes
	// the controller - otherwise every run leaks a router Deployment and
	// Service into openshift-ingress for every later suite.
	t.Cleanup(func() { gtc.deleteIngressControllerAndSettle(ctx, t, gammaController) })
	updateIngresses(append(fixture.ingresses, gamma))
	t.Cleanup(func() { updateIngresses(fixture.ingresses) })
	gtc.validateAdditionalGatewayAndRoute(t, gamma)

	notebook := tc.createAnnotatedNotebook(t, "e2e-notebook-unready", "e2e-notebook-unready", gamma.Name)

	// The assignment is known, so the route parents on gamma's Gateway (its Accepted
	// condition only reflects the parentRef, not serving readiness). The Gateway is
	// not usable: its bridge Route is admitted by no IngressController while gamma's
	// router does not exist, and serving readiness derives from that admission.
	tc.requireNotebookParent(t, notebook, gamma.Name, false)
	gammaRouteKey := types.NamespacedName{Name: gamma.Name, Namespace: gtc.gatewayNamespace()}
	g.Consistently(func(g Gomega) {
		bridge := &routev1.Route{}
		g.Expect(tc.Client().Get(ctx, gammaRouteKey, bridge)).To(Succeed())
		g.Expect(routeAdmittedByRouter(bridge, gamma.Hostname, gamma.IngressControllerName)).To(BeFalse(),
			"the unready Gateway's bridge Route must stay unadmitted")
	}, 10*time.Second, 2*time.Second).Should(Succeed())

	// Making the Gateway usable (creating its IngressController) must admit the route.
	replicas := int32(1)
	require.NoError(t, tc.Client().Create(ctx, &operatorv1.IngressController{
		ObjectMeta: metav1.ObjectMeta{Name: gammaController.Name, Namespace: gammaController.Namespace},
		Spec: operatorv1.IngressControllerSpec{
			Domain:   gamma.Hostname,
			Replicas: &replicas,
			EndpointPublishingStrategy: &operatorv1.EndpointPublishingStrategy{
				Type: operatorv1.PrivateStrategyType,
			},
			RouteSelector: &metav1.LabelSelector{MatchLabels: gamma.RouteLabels},
		},
	}), "create gamma IngressController to make the Gateway usable")
	tc.requireNotebookParent(t, notebook, gamma.Name, true)
	validateAdditionalIngressRouteAdmissionStatus(t, gtc,
		types.NamespacedName{Name: gamma.Name, Namespace: gtc.gatewayNamespace()},
		gamma.Name, gamma.Hostname, gamma.IngressControllerName)

	// Snapshot identity before the controller restart.
	route, err := tc.getNotebookHTTPRoute(ctx, notebook)
	require.NoError(t, err)
	routeUID := route.UID
	statefulSet := &appsv1.StatefulSet{}
	statefulSetKey := types.NamespacedName{Namespace: notebook.Namespace, Name: notebook.Name}
	require.NoError(t, tc.Client().Get(ctx, statefulSetKey, statefulSet))
	statefulSetUID := statefulSet.UID

	// Roll odh-notebook-controller-manager: wait for the old pods to disappear so
	// assertions run against the restarted controller, not a terminating one.
	controllerDeployment := &appsv1.Deployment{}
	controllerKey := types.NamespacedName{
		Name:      "odh-notebook-controller-manager",
		Namespace: tc.AppsNamespace,
	}
	require.NoError(t, tc.Client().Get(ctx, controllerKey, controllerDeployment))
	oldPods := &corev1.PodList{}
	require.NoError(t, tc.Client().List(ctx, oldPods,
		client.InNamespace(tc.AppsNamespace),
		client.MatchingLabels(controllerDeployment.Spec.Selector.MatchLabels)))
	oldNames := make(map[types.UID]struct{}, len(oldPods.Items))
	for _, pod := range oldPods.Items {
		oldNames[pod.UID] = struct{}{}
		require.NoError(t, tc.Client().Delete(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: pod.Name, Namespace: pod.Namespace},
		}))
	}
	g.Eventually(func(g Gomega) {
		deployment := &appsv1.Deployment{}
		g.Expect(tc.Client().Get(ctx, controllerKey, deployment)).To(Succeed())
		g.Expect(deployment.Status.ReadyReplicas).To(BeNumerically(">=", 1),
			"odh-notebook-controller-manager should become ready after the roll")
		pods := &corev1.PodList{}
		g.Expect(tc.Client().List(ctx, pods,
			client.InNamespace(tc.AppsNamespace),
			client.MatchingLabels(controllerDeployment.Spec.Selector.MatchLabels))).To(Succeed())
		g.Expect(pods.Items).NotTo(BeEmpty())
		for _, pod := range pods.Items {
			_, old := oldNames[pod.UID]
			g.Expect(old).To(BeFalse(), "pre-restart pod %s must be gone", pod.Name)
			g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))
		}
	}, tc.TestTimeouts.operandReadinessTimeout, 2*time.Second).Should(Succeed())

	// Convergence without recreation: same HTTPRoute, same StatefulSet. The
	// restarted controller only takes leadership and completes its initial
	// reconcile after its pod reports ready, so the no-recreation property
	// must hold over a window covering that reconcile — a one-shot check at
	// the moment of pod readiness could pass just before a recreation.
	g.Consistently(func(g Gomega) {
		sts := &appsv1.StatefulSet{}
		g.Expect(tc.Client().Get(ctx, statefulSetKey, sts)).To(Succeed())
		g.Expect(sts.UID).To(Equal(statefulSetUID),
			"Notebook StatefulSet must not be recreated by the controller roll")
		route, routeErr := tc.getNotebookHTTPRoute(ctx, notebook)
		g.Expect(routeErr).NotTo(HaveOccurred())
		g.Expect(route.UID).To(Equal(routeUID),
			"Notebook HTTPRoute must not be recreated by the controller roll")
	}, time.Minute, 2*time.Second).Should(Succeed())
	tc.requireNotebookParent(t, notebook, gamma.Name, true)
}

// RHOAIENG-94382 amendment: the projected contract between the platform and the
// notebook controller. The operator projects GatewayConfig status into the
// Workbenches CR (spec.ingresses), the in-cluster workbenches-operator renders
// that projection into the odh-notebook-controller-config ConfigMap
// (data.ingresses, JSON), and the odh-notebook-controller-manager consumes the
// ConfigMap plus the namespace annotation without any GatewayConfig access.

// notebookControllerConfigName is the ConfigMap carrying the projected ingress
// contract for the notebook controller (in the applications namespace).
const notebookControllerConfigName = "odh-notebook-controller-config"

// notebookControllerServiceAccount is the notebook controller's ServiceAccount
// (in the applications namespace) whose RBAC the isolation spec pins.
const notebookControllerServiceAccount = "odh-notebook-controller-manager"

// projectedIngress mirrors one entry of the ConfigMap's data.ingresses JSON.
type projectedIngress struct {
	Name             string `json:"name"`
	GatewayName      string `json:"gatewayName"`
	GatewayNamespace string `json:"gatewayNamespace"`
	Hostname         string `json:"hostname"`
	IsDefault        bool   `json:"isDefault"`
}

// notebookControllerConfigIngresses reads and parses the projected contract.
func (tc *WorkbenchesTestCtx) notebookControllerConfigIngresses() ([]projectedIngress, error) {
	cm := &corev1.ConfigMap{}
	if err := tc.Client().Get(tc.Context(),
		types.NamespacedName{Name: notebookControllerConfigName, Namespace: tc.AppsNamespace}, cm); err != nil {
		return nil, err
	}
	var ingresses []projectedIngress
	if err := json.Unmarshal([]byte(cm.Data["ingresses"]), &ingresses); err != nil {
		return nil, fmt.Errorf("parse %s data.ingresses %q: %w", notebookControllerConfigName, cm.Data["ingresses"], err)
	}
	return ingresses, nil
}

// projectedIngressNames returns the names of the projected entries.
func projectedIngressNames(entries []projectedIngress) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	return names
}

// setNotebookControllerConfigIngresses overwrites the projected contract data.
func (tc *WorkbenchesTestCtx) setNotebookControllerConfigIngresses(ctx context.Context, data string) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cm := &corev1.ConfigMap{}
		if err := tc.Client().Get(ctx,
			types.NamespacedName{Name: notebookControllerConfigName, Namespace: tc.AppsNamespace}, cm); err != nil {
			return err
		}
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		cm.Data["ingresses"] = data
		return tc.Client().Update(ctx, cm)
	})
}

// notebookStatefulSetUID returns the Notebook's StatefulSet UID, the workbench
// recreation signal: contract-data failures must never recreate the workbench.
func (tc *WorkbenchesTestCtx) notebookStatefulSetUID(ctx context.Context, t *testing.T, notebook types.NamespacedName) types.UID {
	t.Helper()
	sts := &unstructured.Unstructured{}
	sts.SetGroupVersionKind(gvk.StatefulSet)
	require.NoError(t, tc.Client().Get(ctx, notebook, sts),
		"the Notebook %s should have a StatefulSet", notebook)
	return sts.GetUID()
}

// notebookNeverOnDefaultGateway reports whether the Notebook's HTTPRoute is
// absent or parented anywhere but the default Gateway. The assignment contract
// forbids default-Gateway fallback on any failure.
func (tc *WorkbenchesTestCtx) notebookNeverOnDefaultGateway(ctx context.Context, notebook types.NamespacedName) bool {
	routes := &gwapiv1.HTTPRouteList{}
	if err := tc.Client().List(ctx, routes,
		client.InNamespace(tc.AppsNamespace),
		client.MatchingLabels{"notebook-name": notebook.Name, "notebook-namespace": notebook.Namespace}); err != nil {
		return false
	}
	if len(routes.Items) != 1 {
		// Absent (or ambiguous) route: nothing is attached to the default.
		return true
	}
	for _, ref := range routes.Items[0].Spec.ParentRefs {
		if string(ref.Name) == gateway.GetDefaultGatewayName() {
			return false
		}
	}
	return true
}

// requireIngressNotFoundEventSince waits for a Warning IngressNotFound event on
// the Notebook emitted after the given time, so repeated failures are not
// satisfied by an earlier event.
func (tc *WorkbenchesTestCtx) requireIngressNotFoundEventSince(ctx context.Context, t *testing.T, notebook types.NamespacedName, since time.Time) {
	t.Helper()
	g := NewWithT(t)
	// Event timestamps are serialized with second precision while since carries
	// nanoseconds: truncate to whole seconds and accept equality, so an event
	// emitted in the same second as the snapshot still matches (an event from
	// before the snapshot is excluded by the seconds-long waits between phases).
	since = since.Truncate(time.Second)
	g.Eventually(func(g Gomega) {
		events := &corev1.EventList{}
		g.Expect(tc.Client().List(ctx, events,
			client.InNamespace(notebook.Namespace),
			client.MatchingFields{"involvedObject.name": notebook.Name, "involvedObject.kind": "Notebook"},
		)).To(Succeed())
		found := false
		for _, event := range events.Items {
			eventTime := event.LastTimestamp
			if eventTime.IsZero() {
				eventTime = metav1.Time(event.EventTime)
			}
			// An events.k8s.io/v1 recorder leaves LastTimestamp empty and, on
			// repeats, keeps the original EventTime while advancing only
			// Series.LastObservedTime — use the later of the two so a fresh
			// repeat of an earlier message still matches `since` (the legacy
			// recorder path updates LastTimestamp and is unaffected).
			if event.Series != nil && !event.Series.LastObservedTime.IsZero() &&
				event.Series.LastObservedTime.After(eventTime.Time) {
				eventTime = metav1.Time(event.Series.LastObservedTime)
			}
			if event.Reason == "IngressNotFound" && event.Type == corev1.EventTypeWarning && !eventTime.Time.Before(since) {
				found = true
			}
		}
		g.Expect(found).To(BeTrue(),
			"Notebook %s should emit a Warning IngressNotFound event after %s", notebook, since.Format(time.RFC3339))
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
}

// skipNotebookIngressContract skips the mode-independent contract specs on XKS
// clusters and non-OcpRoute modes. Unlike the assignment-matrix specs these do
// not depend on the authentication mode - the notebook fixture injects per-shard
// OIDC identities on BYOIDC clusters - so they run on both cluster types.
func (tc *WorkbenchesTestCtx) skipNotebookIngressContract(t *testing.T) {
	t.Helper()
	tc.SkipIfXKSCluster(t)

	config := &serviceApi.GatewayConfig{}
	require.NoError(t, tc.Client().Get(tc.Context(), types.NamespacedName{Name: gatewayConfigName}, config))
	if config.Spec.IngressMode != serviceApi.IngressModeOcpRoute {
		t.Skipf("Notebook ingress contract specs require OcpRoute mode, cluster is in %s", config.Spec.IngressMode)
	}
}

// ValidateNotebookIngressContractConfigMapLifecycle pins the projected
// contract ConfigMap's lifecycle (94382): the Workbenches module recreates the
// ConfigMap after out-of-band deletion with the current contents, removes
// stale entries when an ingress is removed from the GatewayConfig, and
// re-projects re-added ingresses.
func (tc *WorkbenchesTestCtx) ValidateNotebookIngressContractConfigMapLifecycle(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier2)
	tc.skipNotebookIngressContract(t)
	t.Log("Validating notebook ingress contract ConfigMap lifecycle")

	fixture := tc.configureAdditionalIngresses(t)
	alpha, beta := fixture.ingresses[0], fixture.ingresses[1]
	g := NewWithT(t)
	ctx := tc.Context()

	requireProjection := func(want ...string) {
		g.Eventually(func() ([]string, error) {
			entries, err := tc.notebookControllerConfigIngresses()
			if err != nil {
				return nil, err
			}
			return projectedIngressNames(entries), nil
		}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(ConsistOf(want),
			"the projected contract should contain exactly %v", want)
	}

	// Baseline: both shards and the default are projected, with full entries.
	requireProjection(gateway.GetDefaultGatewayName(), alpha.Name, beta.Name)
	entries, err := tc.notebookControllerConfigIngresses()
	require.NoError(t, err)
	defaultEntry, alphaEntry := projectedIngress{}, projectedIngress{}
	for _, entry := range entries {
		switch entry.Name {
		case gateway.GetDefaultGatewayName():
			defaultEntry = entry
		case alpha.Name:
			alphaEntry = entry
		}
	}
	require.Equal(t, gateway.GetDefaultGatewayName(), defaultEntry.GatewayName)
	require.Equal(t, gateway.GetGatewayNamespace(), defaultEntry.GatewayNamespace)
	require.True(t, defaultEntry.IsDefault)
	require.NotEmpty(t, defaultEntry.Hostname, "the default entry should carry the cluster domain")
	require.Equal(t, alpha.Name, alphaEntry.GatewayName)
	require.Equal(t, gateway.GetGatewayNamespace(), alphaEntry.GatewayNamespace)
	require.Equal(t, alpha.Hostname, alphaEntry.Hostname)
	require.False(t, alphaEntry.IsDefault)

	// Out-of-band deletion: the Workbenches module must recreate the ConfigMap
	// with the current contents.
	require.NoError(t, tc.Client().Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: notebookControllerConfigName, Namespace: tc.AppsNamespace,
	}}), "delete the projected contract ConfigMap out of band")
	requireProjection(gateway.GetDefaultGatewayName(), alpha.Name, beta.Name)
	recreated, err := tc.notebookControllerConfigIngresses()
	require.NoError(t, err)
	require.Equal(t, entries, recreated,
		"the recreated contract ConfigMap should carry the same projection")

	// Removing a shard drops only its stale entry; the rest is preserved.
	updateAdditional := func(additional serviceApi.AdditionalIngresses) error {
		return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			current := &serviceApi.GatewayConfig{}
			if err := tc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, current); err != nil {
				return err
			}
			current.Spec.AdditionalIngresses = additional
			return tc.Client().Update(ctx, current)
		})
	}
	require.NoError(t, updateAdditional(serviceApi.AdditionalIngresses{beta}))
	requireProjection(gateway.GetDefaultGatewayName(), beta.Name)
	afterRemoval, err := tc.notebookControllerConfigIngresses()
	require.NoError(t, err)
	var defaultAfterRemoval projectedIngress
	for _, entry := range afterRemoval {
		if entry.Name == gateway.GetDefaultGatewayName() {
			defaultAfterRemoval = entry
		}
	}
	require.Equal(t, defaultEntry, defaultAfterRemoval,
		"the default entry must be preserved while the stale entry is dropped")

	// Re-adding the shard returns its entry.
	require.NoError(t, updateAdditional(fixture.ingresses))
	requireProjection(gateway.GetDefaultGatewayName(), alpha.Name, beta.Name)
}

// ValidateNotebookIngressContractDataFailures pins the contract-data failure
// cases. DEFERRED to RHOAIENG-102011 (contract v1): against the shipped 3.6
// build this spec is red by design - the in-cluster workbenches-operator
// reverts a corrupted odh-notebook-controller-config in ~26ms and the platform
// operator re-renders a corrupted Workbenches CR in ~2.5s, healing the
// contract before the notebook controller can observe the failure, and the
// IngressResolved status the original AC names does not exist yet. The spec is
// the implementation target for 102011's fail-closed contract decoding: run it
// with E2E_TEST_RUN_DEFERRED_CONTRACT_V1=true once that implementation lands.
// When enabled it asserts: corrupting the projected contract (malformed JSON,
// or a valid projection missing the assigned shard's entry) must surface as
// IngressNotFound with the route removed, must never fall back to the default
// Gateway, must not recreate the workbench, and must reconverge once the
// contract data is valid again.
func (tc *WorkbenchesTestCtx) ValidateNotebookIngressContractDataFailures(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier2)
	// Deferred to RHOAIENG-102011 (contract v1): the shipped build heals
	// malformed/missing contract data before the notebook controller can
	// observe it (see the doc comment), so the AC's failure semantics are not
	// realizable yet. Opting in runs the red implementation target.
	if os.Getenv("E2E_TEST_RUN_DEFERRED_CONTRACT_V1") != "true" {
		t.Skipf("deferred to RHOAIENG-102011 (contract v1): the shipped build heals corrupted contract data " +
			"before it is observable; set E2E_TEST_RUN_DEFERRED_CONTRACT_V1=true to run this red implementation target")
	}
	tc.skipNotebookIngressAssignment(t)
	t.Log("Validating notebook ingress contract data failures")

	fixture := tc.configureAdditionalIngresses(t)
	alpha := fixture.ingresses[0]
	notebook := tc.createAnnotatedNotebook(t, "e2e-notebook-contract", "e2e-notebook-contract", alpha.Name)
	tc.requireNotebookParent(t, notebook, alpha.Name, true)

	g := NewWithT(t)
	ctx := tc.Context()
	stsUID := tc.notebookStatefulSetUID(ctx, t, notebook)

	// The valid projection to restore after each corruption.
	cm := &corev1.ConfigMap{}
	require.NoError(t, tc.Client().Get(ctx,
		types.NamespacedName{Name: notebookControllerConfigName, Namespace: tc.AppsNamespace}, cm))
	validData := cm.Data["ingresses"]

	// A valid projection with alpha's entry removed (missing contract data).
	var entries []map[string]any
	require.NoError(t, json.Unmarshal([]byte(validData), &entries))
	withoutAlpha := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if entry["name"] != alpha.Name {
			withoutAlpha = append(withoutAlpha, entry)
		}
	}
	missingData, err := json.Marshal(withoutAlpha)
	require.NoError(t, err)

	routeCount := func() (int, error) {
		routes := &gwapiv1.HTTPRouteList{}
		if err := tc.Client().List(ctx, routes,
			client.InNamespace(tc.AppsNamespace),
			client.MatchingLabels{"notebook-name": notebook.Name, "notebook-namespace": notebook.Namespace}); err != nil {
			return 0, err
		}
		return len(routes.Items), nil
	}

	injectFailure := func(name, data string) {
		t.Helper()
		since := time.Now()
		require.NoError(t, tc.setNotebookControllerConfigIngresses(ctx, data),
			"inject %s contract data", name)
		// The controller must notice: a fresh IngressNotFound event fires and
		// the route is removed.
		tc.requireIngressNotFoundEventSince(ctx, t, notebook, since)
		g.Eventually(routeCount, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(BeZero(),
			"the Notebook's HTTPRoute should be removed on %s contract data", name)
		// Never the default Gateway, never a recreated workbench.
		g.Consistently(func() bool {
			return tc.notebookNeverOnDefaultGateway(ctx, notebook)
		}, 30*time.Second, 2*time.Second).Should(BeTrue(),
			"%s contract data must never fall back to the default Gateway", name)
		require.Equal(t, stsUID, tc.notebookStatefulSetUID(ctx, t, notebook),
			"%s contract data must not recreate the workbench", name)
	}

	// Missing contract data: a valid projection without the assigned shard.
	// This is the parseable failure: the shard becomes unknown to the
	// controller, which must surface IngressNotFound and remove the route.
	injectFailure("missing", string(missingData))

	// Restore the valid projection and wait for the assignment to reconverge
	// before the malformed phase: the missing phase above ends with the route
	// already removed, so starting the malformed assertions from that state
	// would make them pass vacuously even if the controller ignored malformed
	// data entirely (the route-less, no-fallback checks cannot fail twice).
	// Each failure phase must be observed from a healthy, route-admitted
	// state.
	require.NoError(t, tc.setNotebookControllerConfigIngresses(ctx, validData))
	tc.requireNotebookParent(t, notebook, alpha.Name, true)

	// Malformed contract data: the projection cannot be parsed at all. The
	// shipped controller notices (its ConfigMap watch fires and reconciles all
	// Notebooks) but the reconcile fails on the parse without emitting
	// IngressNotFound or removing the route - the stale route keeps serving.
	// This case encodes the AC behavior and currently FAILS as the product
	// finding on 94382 (routed to 94381/94380); see the qualification ledger.
	injectFailure("malformed", `[{"name": "truncated`)

	// Recovery: with valid contract data the assignment reconverges.
	require.NoError(t, tc.setNotebookControllerConfigIngresses(ctx, validData))
	tc.requireNotebookParent(t, notebook, alpha.Name, true)
	require.Equal(t, stsUID, tc.notebookStatefulSetUID(ctx, t, notebook),
		"contract-data recovery must not recreate the workbench")
}

// ValidateNotebookControllerRBACIsolation pins the notebook controller's
// permission boundary (94382): the controller holds NO GatewayConfig or
// Workbenches CR access (its whole ingress world is the projected ConfigMap,
// the namespace annotation and the Gateways), while holding the positive
// contract permissions on ConfigMaps, namespaces and Gateways.
func (tc *WorkbenchesTestCtx) ValidateNotebookControllerRBACIsolation(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)
	tc.SkipIfXKSCluster(t)
	t.Log("Validating notebook controller RBAC isolation")

	g := NewWithT(t)
	ctx := tc.Context()

	// Ask the API server's authorizer directly (SubjectAccessReview) instead
	// of walking RoleBindings: a ServiceAccount request carries not only the
	// user identity but also the groups system:serviceaccounts,
	// system:serviceaccounts:<namespace> and system:authenticated, so
	// permissions granted through any of those groups (or via role
	// aggregation, or an authorizer webhook) are invisible to a binding walk.
	// The authorizer's answer is exactly what the controller itself
	// experiences, which is also what this spec is about.
	saUser := fmt.Sprintf("system:serviceaccount:%s:%s", tc.AppsNamespace, notebookControllerServiceAccount)
	saGroups := []string{
		"system:serviceaccounts",
		"system:serviceaccounts:" + tc.AppsNamespace,
		"system:authenticated",
	}
	allowed := func(group, resource, verb string) bool {
		sar := &authorizationv1.SubjectAccessReview{
			Spec: authorizationv1.SubjectAccessReviewSpec{
				User:   saUser,
				Groups: saGroups,
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Group:    group,
					Resource: resource,
					Verb:     verb,
				},
			},
		}
		require.NoError(t, tc.Client().Create(ctx, sar))
		return sar.Status.Allowed
	}

	// Negative isolation: no GatewayConfig access (the controller must work
	// from the projection alone) and no Workbenches CR access either — no
	// verb at all (list and watch also return the full objects, so a get-only
	// check would pass while the controller could still read specs and
	// statuses).
	for _, verb := range []string{"get", "list", "watch", "create", "update", "patch", "delete"} {
		g.Expect(allowed("services.platform.opendatahub.io", "gatewayconfigs", verb)).To(BeFalse(),
			"the notebook controller must not %s GatewayConfigs", verb)
		g.Expect(allowed("components.platform.opendatahub.io", "workbenches", verb)).To(BeFalse(),
			"the notebook controller must not %s Workbenches CRs", verb)
	}

	// Positive contract: ConfigMaps, namespaces and Gateways are readable.
	for _, verb := range []string{"get", "list", "watch"} {
		g.Expect(allowed("", "configmaps", verb)).To(BeTrue(),
			"the notebook controller should %s ConfigMaps (the projected contract)", verb)
		g.Expect(allowed("", "namespaces", verb)).To(BeTrue(),
			"the notebook controller should %s namespaces (the assignment annotation)", verb)
		g.Expect(allowed("gateway.networking.k8s.io", "gateways", verb)).To(BeTrue(),
			"the notebook controller should %s Gateways", verb)
	}
}

// ValidateNotebookGatewayAttachmentPermissions pins the Gateway attachment
// boundary (94382): every operator-rendered Gateway (default and shards)
// admits HTTPRoutes only from the gateway and applications namespaces, and an
// HTTPRoute parented from any other namespace is not admitted.
func (tc *WorkbenchesTestCtx) ValidateNotebookGatewayAttachmentPermissions(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier2)
	tc.skipNotebookIngressContract(t)
	t.Log("Validating notebook Gateway attachment permissions")

	fixture := tc.configureAdditionalIngresses(t)
	alpha := fixture.ingresses[0]
	g := NewWithT(t)
	ctx := tc.Context()
	gatewayNamespace := gateway.GetGatewayNamespace()

	requireAdmitsConfiguredNamespaces := func(gatewayName string) {
		gw := &gwapiv1.Gateway{}
		require.NoError(t, tc.Client().Get(ctx, types.NamespacedName{Name: gatewayName, Namespace: gatewayNamespace}, gw),
			"Gateway %s should exist", gatewayName)
		require.NotEmpty(t, gw.Spec.Listeners, "Gateway %s should have listeners", gatewayName)
		for _, listener := range gw.Spec.Listeners {
			require.NotNil(t, listener.AllowedRoutes, "listener %s of %s should pin allowedRoutes", listener.Name, gatewayName)
			require.NotNil(t, listener.AllowedRoutes.Namespaces)
			require.NotNil(t, listener.AllowedRoutes.Namespaces.From)
			require.Equal(t, gwapiv1.NamespacesFromSelector, *listener.AllowedRoutes.Namespaces.From,
				"listener %s of %s should select namespaces, not admit all", listener.Name, gatewayName)
			selector := listener.AllowedRoutes.Namespaces.Selector
			require.NotNil(t, selector)
			admitted := false
			for _, expr := range selector.MatchExpressions {
				if expr.Key != "kubernetes.io/metadata.name" || expr.Operator != metav1.LabelSelectorOpIn {
					continue
				}
				values := map[string]bool{}
				for _, value := range expr.Values {
					values[value] = true
				}
				if values[gatewayNamespace] && values[tc.AppsNamespace] && len(expr.Values) == 2 {
					admitted = true
				}
			}
			require.True(t, admitted,
				"listener %s of %s should admit routes exactly from %s and %s",
				listener.Name, gatewayName, gatewayNamespace, tc.AppsNamespace)
		}
	}
	requireAdmitsConfiguredNamespaces(gateway.GetDefaultGatewayName())
	for _, ingress := range fixture.ingresses {
		requireAdmitsConfiguredNamespaces(ingress.Name)
	}

	// Negative: a route from a namespace outside allowedRoutes is not admitted.
	routeNamespace := fmt.Sprintf("e2e-attach-%x", time.Now().UnixNano())
	require.NoError(t, tc.Client().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: routeNamespace}}))
	t.Cleanup(func() {
		if err := tc.Client().Delete(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: routeNamespace}}); err != nil && !k8serr.IsNotFound(err) {
			t.Errorf("delete attachment-test namespace %s: %v", routeNamespace, err)
		}
	})
	parentGroup := gwapiv1.Group("gateway.networking.k8s.io")
	parentKind := gwapiv1.Kind("Gateway")
	parentNamespace := gwapiv1.Namespace(gatewayNamespace)
	backendGroup := gwapiv1.Group("")
	backendKind := gwapiv1.Kind("Service")
	backendPort := gwapiv1.PortNumber(80)
	route := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "not-admitted", Namespace: routeNamespace},
		Spec: gwapiv1.HTTPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{{
					Group:     &parentGroup,
					Kind:      &parentKind,
					Name:      gwapiv1.ObjectName(alpha.Name),
					Namespace: &parentNamespace,
				}},
			},
			Rules: []gwapiv1.HTTPRouteRule{{
				BackendRefs: []gwapiv1.HTTPBackendRef{{
					BackendRef: gwapiv1.BackendRef{
						BackendObjectReference: gwapiv1.BackendObjectReference{
							Group: &backendGroup,
							Kind:  &backendKind,
							Name:  "nowhere",
							Port:  &backendPort,
						},
					},
				}},
			}},
		},
	}
	require.NoError(t, tc.Client().Create(ctx, route))
	t.Cleanup(func() {
		if err := tc.Client().Delete(context.Background(), route); err != nil && !k8serr.IsNotFound(err) {
			t.Errorf("delete attachment-test HTTPRoute: %v", err)
		}
	})

	// The Gateway must actively evaluate and reject the foreign route: an
	// Accepted=False condition with reason NotAllowedByListeners proves the
	// listener's allowedRoutes filtering fired, so the never-admitted check
	// below cannot pass merely because the route's status was never
	// populated.
	g.Eventually(func(g Gomega) {
		current := &gwapiv1.HTTPRoute{}
		g.Expect(tc.Client().Get(ctx, types.NamespacedName{Name: route.Name, Namespace: routeNamespace}, current)).To(Succeed())
		rejected := false
		for _, parent := range current.Status.Parents {
			for _, condition := range parent.Conditions {
				if condition.Type == "Accepted" && condition.Status == metav1.ConditionFalse && condition.Reason == "NotAllowedByListeners" {
					rejected = true
				}
			}
		}
		g.Expect(rejected).To(BeTrue(),
			"the Gateway should reject the route with Accepted=False/NotAllowedByListeners (route from %s, Gateway %s)", routeNamespace, alpha.Name)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())

	g.Consistently(func(g Gomega) {
		current := &gwapiv1.HTTPRoute{}
		g.Expect(tc.Client().Get(ctx, types.NamespacedName{Name: route.Name, Namespace: routeNamespace}, current)).To(Succeed())
		for _, parent := range current.Status.Parents {
			for _, condition := range parent.Conditions {
				g.Expect(condition.Type == "Accepted" && condition.Status == metav1.ConditionTrue).To(BeFalse(),
					"a route from %s must not be Accepted by Gateway %s", routeNamespace, alpha.Name)
			}
		}
	}, time.Minute, 5*time.Second).Should(Succeed(),
		"an HTTPRoute in a namespace outside allowedRoutes must never be admitted")
}

// ValidateNotebookBackendReferenceGrants pins the backend ReferenceGrant
// behavior (94382): a valid assignment holds a ReferenceGrant permitting the
// central-namespace HTTPRoute to reference the user-namespace Notebook
// Service; deleting the grant is acted upon (the notebook controller's
// ReferenceGrant watch recreates it within tens of milliseconds, and the
// route transiently reports ResolvedRefs=False when the Gateway re-evaluates
// inside that gap) without default-Gateway fallback or workbench recreation;
// the assignment converges healthy again.
func (tc *WorkbenchesTestCtx) ValidateNotebookBackendReferenceGrants(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier2)
	tc.skipNotebookIngressContract(t)
	t.Log("Validating notebook backend ReferenceGrants")

	fixture := tc.configureAdditionalIngresses(t)
	alpha := fixture.ingresses[0]
	notebook := tc.createAnnotatedNotebook(t, "e2e-notebook-grants", "e2e-notebook-grants", alpha.Name)
	tc.requireNotebookParent(t, notebook, alpha.Name, true)

	g := NewWithT(t)
	ctx := tc.Context()
	stsUID := tc.notebookStatefulSetUID(ctx, t, notebook)

	// Positive: the notebook namespace holds the backend ReferenceGrant. The
	// e2e client scheme does not register the gateway-api v1beta1 types, so
	// the grants are read as unstructured objects and parsed locally.
	referenceGrantGVK := schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1beta1", Kind: "ReferenceGrant"}
	referenceGrantListGVK := schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1beta1", Kind: "ReferenceGrantList"}
	type referenceGrantContract struct {
		Spec struct {
			From []struct {
				Group     string `json:"group"`
				Kind      string `json:"kind"`
				Namespace string `json:"namespace"`
			} `json:"from"`
			To []struct {
				Group string `json:"group"`
				Kind  string `json:"kind"`
			} `json:"to"`
		} `json:"spec"`
	}
	grants := &unstructured.UnstructuredList{}
	grants.SetGroupVersionKind(referenceGrantListGVK)
	require.NoError(t, tc.Client().List(ctx, grants, client.InNamespace(notebook.Namespace)))
	var grant *unstructured.Unstructured
	for i := range grants.Items {
		candidate := &grants.Items[i]
		raw, err := candidate.MarshalJSON()
		require.NoError(t, err)
		var contract referenceGrantContract
		require.NoError(t, json.Unmarshal(raw, &contract))
		fromOK, toOK := false, false
		for _, from := range contract.Spec.From {
			if from.Group == "gateway.networking.k8s.io" && from.Kind == "HTTPRoute" &&
				from.Namespace == tc.AppsNamespace {
				fromOK = true
			}
		}
		for _, to := range contract.Spec.To {
			if to.Group == "" && to.Kind == "Service" {
				toOK = true
			}
		}
		if fromOK && toOK {
			grant = candidate
		}
	}
	require.NotNil(t, grant,
		"the notebook namespace should hold a ReferenceGrant allowing HTTPRoutes from %s to reference the Notebook Service",
		tc.AppsNamespace)

	// Negative: deleting the grant is acted upon, never ignored. The notebook
	// controller watches ReferenceGrants and recreates the grant within
	// ~50-65ms on the qualification clusters (measured on both), defending the
	// backend reference; when the Gateway implementation re-evaluates the
	// route inside that gap the route transiently reports ResolvedRefs=False
	// (observed on the BYOIDC cluster - see the qualification ledger). Which
	// of the two wins is a race, so the spec asserts the deterministic
	// outcomes: the grant comes back, the route never falls back to the
	// default Gateway, the workbench is not recreated, and the assignment
	// converges healthy again.
	saved := grant.DeepCopy()
	grantExists := func() bool {
		current := &unstructured.Unstructured{}
		current.SetGroupVersionKind(referenceGrantGVK)
		return tc.Client().Get(ctx, types.NamespacedName{Name: saved.GetName(), Namespace: saved.GetNamespace()}, current) == nil
	}
	require.NoError(t, tc.Client().Delete(ctx, grant))
	g.Eventually(grantExists, tc.TestTimeouts.authGatewayTimeout, 500*time.Millisecond).Should(BeTrue(),
		"the deleted backend ReferenceGrant must be recreated (the controller's backend-reference defense)")
	g.Consistently(func() bool {
		return tc.notebookNeverOnDefaultGateway(ctx, notebook)
	}, 30*time.Second, 2*time.Second).Should(BeTrue(),
		"a deleted backend ReferenceGrant must never fall back to the default Gateway")
	require.Equal(t, stsUID, tc.notebookStatefulSetUID(ctx, t, notebook),
		"a deleted backend ReferenceGrant must not recreate the workbench")

	// Restore: the route recovers (recreating the grant if nothing healed it).
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(referenceGrantGVK)
	if err := tc.Client().Get(ctx, types.NamespacedName{Name: saved.GetName(), Namespace: saved.GetNamespace()}, current); err != nil {
		require.True(t, k8serr.IsNotFound(err), "fetch the ReferenceGrant after deletion: %v", err)
		restored := saved.DeepCopy()
		restored.SetResourceVersion("")
		restored.SetUID("")
		require.NoError(t, tc.Client().Create(ctx, restored), "recreate the backend ReferenceGrant")
	}
	tc.requireNotebookParent(t, notebook, alpha.Name, true)
	require.Equal(t, stsUID, tc.notebookStatefulSetUID(ctx, t, notebook),
		"backend reference recovery must not recreate the workbench")
}

package e2e_test

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"

	. "github.com/onsi/gomega"
)

// validateAdditionalIngressAuthentication exercises the real authentication path:
// test client -> port-forward -> private OpenShift router -> bridge Route ->
// additional Gateway -> authentication filter -> test Service -> test Pod.
// Each ingress has its own backend and exact HTTPRoute path, attached only to its Gateway.
// Anonymous requests must redirect to that ingress's login; invalid tokens must fail;
// a valid ServiceAccount token must reach only that ingress's backend.
// Authenticated requests to sibling paths and the Dashboard API must return 404.
// Cleanup removes the fixtures and tunnel; the caller restores ingress configuration.
func (tc *GatewayTestCtx) validateAdditionalIngressAuthentication(
	t *testing.T,
	controllerKey types.NamespacedName,
	config *serviceApi.GatewayConfig,
	ingresses serviceApi.AdditionalIngresses,
) {
	t.Helper()
	if config.Spec.EnableK8sTokenValidation != nil && !*config.Spec.EnableK8sTokenValidation {
		t.Skip("authenticated ingress invocation requires spec.enableK8sTokenValidation=true")
	}
	ctx := tc.Context()
	// Temporarily admit both test hosts through the private router. The caller's
	// next update restores beta's missing-controller case before removing entries.
	for i := range ingresses {
		ingresses[i].IngressControllerName = controllerKey.Name
	}
	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &operatorv1.IngressController{}
		if err := tc.Client().Get(ctx, controllerKey, current); err != nil {
			return err
		}
		current.Spec.RouteSelector = &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key:      "example.com/ingress",
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"e2e-alpha", "e2e-beta"},
			}},
		}
		return tc.Client().Update(ctx, current)
	}))
	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &serviceApi.GatewayConfig{}
		if err := tc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, current); err != nil {
			return err
		}
		current.Spec.AdditionalIngresses = ingresses
		return tc.Client().Update(ctx, current)
	}))
	for _, ingress := range ingresses {
		validateAdditionalIngressRouteAdmissionStatus(t, tc, types.NamespacedName{
			Name:      ingress.Name,
			Namespace: tc.gatewayNamespace(),
		}, ingress.Name, ingress.Hostname, controllerKey.Name)
	}
	account := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "gateway-auth-e2e-",
			Namespace:    tc.AppsNamespace,
		},
	}
	require.NoError(t, tc.Client().Create(ctx, account))
	t.Cleanup(func() {
		if err := tc.Client().Delete(ctx, account); err != nil && !k8serr.IsNotFound(err) {
			t.Errorf("delete test ServiceAccount: %v", err)
		}
	})
	for _, ingress := range ingresses {
		tc.createAdditionalIngressBackend(t, ingress, account.Name+"-"+ingress.Name)
	}
	token := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: new(int64(600)),
		},
	}
	require.NoError(t, tc.Client().SubResource("token").Create(ctx, account, token))
	require.NotEmpty(t, token.Status.Token)

	type requestCase struct {
		name     string
		path     string
		token    string
		statuses []int
	}
	httpClient := tc.additionalIngressRouterClient(t, controllerKey)
	for _, ingress := range ingresses {
		t.Run(ingress.Name, func(t *testing.T) {
			testCases := []requestCase{
				{
					name:     "anonymous login redirect",
					path:     "/e2e/" + ingress.Name,
					statuses: []int{http.StatusFound, http.StatusTemporaryRedirect},
				},
				{
					name:     "invalid bearer token rejected",
					path:     "/e2e/" + ingress.Name,
					token:    "invalid-e2e-token",
					statuses: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusFound, http.StatusTemporaryRedirect},
				},
				{
					name:     "authenticated backend invocation",
					path:     "/e2e/" + ingress.Name,
					token:    token.Status.Token,
					statuses: []int{http.StatusOK},
				},
				{
					name:     "dashboard backend is not exposed",
					path:     "/api/k8s/apis",
					token:    token.Status.Token,
					statuses: []int{http.StatusNotFound},
				},
			}
			for _, sibling := range ingresses {
				if sibling.Name != ingress.Name {
					testCases = append(testCases, requestCase{
						name:     "sibling backend " + sibling.Name + " is not exposed",
						path:     "/e2e/" + sibling.Name,
						token:    token.Status.Token,
						statuses: []int{http.StatusNotFound},
					})
				}
			}
			for _, test := range testCases {
				t.Run(test.name, func(t *testing.T) {
					g := NewWithT(t)
					g.Eventually(func(g Gomega) {
						requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
						defer cancel()
						request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, "https://"+ingress.Hostname+test.path, nil)
						g.Expect(err).NotTo(HaveOccurred())
						if test.token != "" {
							request.Header.Set("Authorization", "Bearer "+test.token)
						}
						response, err := httpClient.Do(request)
						g.Expect(err).NotTo(HaveOccurred())
						defer response.Body.Close()
						g.Expect(response.StatusCode).To(BeElementOf(test.statuses))
						switch response.StatusCode {
						case http.StatusOK:
							body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
							g.Expect(err).NotTo(HaveOccurred())
							g.Expect(string(body)).To(Equal(account.Name + "-" + ingress.Name))
						case http.StatusFound, http.StatusTemporaryRedirect:
							location, err := url.Parse(response.Header.Get("Location"))
							g.Expect(err).NotTo(HaveOccurred())
							g.Expect(location.Query().Get("client_id")).To(Equal(gateway.AuthClientID + "-" + ingress.Name))
							g.Expect(location.Query().Get("redirect_uri")).To(Equal("https://" + ingress.Hostname + gateway.OAuthCallbackPath))
						}
					}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
				})
			}
		})
	}
}

func (tc *GatewayTestCtx) createAdditionalIngressBackend(t *testing.T, ingress serviceApi.AdditionalIngress, name string) {
	t.Helper()
	ctx := tc.Context()
	g := NewWithT(t)
	backendLabels := map[string]string{"app": name}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: tc.AppsNamespace, Labels: backendLabels},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken:  new(false),
			TerminationGracePeriodSeconds: new(int64(1)),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   new(true),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{{
				Name:  "backend",
				Image: "registry.k8s.io/e2e-test-images/agnhost:2.56",
				Args:  []string{"serve-hostname", "--port=8080"},
				Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: new(false),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
			}},
		},
	}
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: tc.AppsNamespace},
		Spec: corev1.ServiceSpec{
			Selector: backendLabels,
			Ports:    []corev1.ServicePort{{Name: "http", Port: 8080, TargetPort: intstr.FromInt32(8080)}},
		},
	}
	route := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: tc.AppsNamespace},
		Spec: gwapiv1.HTTPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{{
					Name:      gwapiv1.ObjectName(ingress.Name),
					Namespace: new(gwapiv1.Namespace(tc.gatewayNamespace())),
				}},
			},
			// Leave hostnames unrestricted so isolation depends on parentRefs.
			Rules: []gwapiv1.HTTPRouteRule{{
				Matches: []gwapiv1.HTTPRouteMatch{{
					Path: &gwapiv1.HTTPPathMatch{Type: new(gwapiv1.PathMatchExact), Value: new("/e2e/" + ingress.Name)},
				}},
				BackendRefs: []gwapiv1.HTTPBackendRef{{
					BackendRef: gwapiv1.BackendRef{BackendObjectReference: gwapiv1.BackendObjectReference{
						Name: gwapiv1.ObjectName(name), Port: new(gwapiv1.PortNumber(8080)),
					}},
				}},
			}},
		},
	}
	for _, resource := range []client.Object{pod, service, route} {
		require.NoError(t, tc.Client().Create(ctx, resource))
		t.Cleanup(func() {
			if err := tc.Client().Delete(ctx, resource); err != nil && !k8serr.IsNotFound(err) {
				t.Errorf("delete test %T %s: %v", resource, resource.GetName(), err)
			}
		})
	}
	g.Eventually(func(g Gomega) {
		current := &gwapiv1.HTTPRoute{}
		g.Expect(tc.Client().Get(ctx, client.ObjectKeyFromObject(route), current)).To(Succeed())
		var conditions []metav1.Condition
		for _, parent := range current.Status.Parents {
			if parent.ParentRef.Name == gwapiv1.ObjectName(ingress.Name) {
				conditions = parent.Conditions
				break
			}
		}
		for _, conditionType := range []gwapiv1.RouteConditionType{gwapiv1.RouteConditionAccepted, gwapiv1.RouteConditionResolvedRefs} {
			condition := meta.FindStatusCondition(conditions, string(conditionType))
			g.Expect(condition).NotTo(BeNil(), "missing %s condition", conditionType)
			g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(condition.ObservedGeneration).To(Equal(current.Generation))
		}
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed(), "test HTTPRoute must attach to %s", ingress.Name)
}

// Forward the private router's HTTPS port while retaining the request's hostname
// for TLS SNI and HTTP routing. Traffic still crosses the router and bridge Route.
func (tc *GatewayTestCtx) additionalIngressRouterClient(t *testing.T, controllerKey types.NamespacedName) *http.Client {
	t.Helper()
	ctx, cancel := context.WithCancel(tc.Context())
	// #nosec G204 -- fixed executable and argument boundaries; controller name is generated by this test
	command := exec.CommandContext(ctx, "kubectl", "--namespace", tc.gatewayNamespace(), "port-forward",
		"deployment/router-"+controllerKey.Name, "0:443", "--address=127.0.0.1")
	command.WaitDelay = 5 * time.Second
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		cancel()
		t.Fatalf("start private router port-forward: %v", err)
	}
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			if address, found := strings.CutPrefix(scanner.Text(), "Forwarding from 127.0.0.1:"); found {
				port, _, _ := strings.Cut(address, " -> ")
				ready <- "127.0.0.1:" + port
			}
		}
		done <- command.Wait()
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("private router port-forward did not stop")
		}
	})
	var address string
	select {
	case address = <-ready:
	case err := <-done:
		// Leave the result available for cleanup.
		done <- err
		t.Fatalf("private router port-forward stopped: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("private router port-forward did not become ready")
	}
	httpClient := tc.createHTTPClient()
	httpTransport, ok := httpClient.Transport.(*http.Transport)
	require.True(t, ok)
	httpTransport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	httpClient.Timeout = 30 * time.Second
	t.Cleanup(httpTransport.CloseIdleConnections)
	// Each request is independent: do not carry authentication cookies between targets.
	httpClient.Jar = nil
	return httpClient
}

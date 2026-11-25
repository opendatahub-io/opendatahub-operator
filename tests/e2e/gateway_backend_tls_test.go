package e2e_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"

	. "github.com/onsi/gomega"
)

const (
	backendTLSHostileName = "backend-tls-hostile"
	backendTLSTrustedName = "backend-tls-trusted"
	backendTLSHostilePath = "/backend-tls-hostile"
	backendTLSTrustedPath = "/backend-tls-trusted"
	backendTLSPort        = int32(8443)

	// backendTLSEnvoyConfigMapName names the shared Envoy bootstrap ConfigMap
	// both static backends run. backendTLSTokenExpiration bounds the probe
	// token lifetime (seconds).
	backendTLSEnvoyConfigMapName = "backend-tls-envoy-config"
	backendTLSTokenExpiration    = int64(3600)
)

// backendTLSEnvoyBootstrap is the Envoy configuration both backends run: a
// static HTTPS server that answers 200 to every request with whichever
// certificate is mounted at /etc/tls/private. It has no other behaviour, so
// the gateway's response reflects only whether upstream TLS verification
// passed. The Envoy v3 configuration API is stable across Envoy releases,
// unlike the flag surface of an auth proxy bent into a static server.
const backendTLSEnvoyBootstrap = `admin:
  address: { socket_address: { address: 127.0.0.1, port_value: 9901 } }
static_resources:
  listeners:
    - name: https
      address: { socket_address: { address: 0.0.0.0, port_value: 8443 } }
      filter_chains:
        - transport_socket:
            name: envoy.transport_sockets.tls
            typed_config:
              "@type": type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.DownstreamTlsContext
              common_tls_context:
                tls_certificates:
                  - certificate_chain: { filename: /etc/tls/private/tls.crt }
                    private_key: { filename: /etc/tls/private/tls.key }
          filters:
            - name: envoy.filters.network.http_connection_manager
              typed_config:
                "@type": type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
                stat_prefix: backend_tls
                codec_type: AUTO
                route_config:
                  name: local
                  virtual_hosts:
                    - name: backend
                      domains: ["*"]
                      routes:
                        - match: { prefix: "/" }
                          direct_response: { status: 200, body: { inline_string: ok } }
                http_filters:
                  - name: envoy.filters.http.router
                    typed_config:
                      "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
`

// ValidateBackendTLSPolicies verifies that the operator renders the trust-bundle
// ConfigMap and BackendTLSPolicies for its own gateway backends (RHAI-19666).
// The oauth-callback route backend (kube-auth-proxy) is rendered by the
// controller in every auth mode, so its policy is always expected. Its Service
// exposes two ports, so the policy is scoped to the routed https port.
func (tc *GatewayTestCtx) ValidateBackendTLSPolicies(t *testing.T) {
	t.Helper()

	ctx := tc.Context()

	// The trust bundle ConfigMap carries a PEM CA bundle under the Gateway API
	// documented key and is owned by the GatewayConfig.
	bundle := &corev1.ConfigMap{}
	tc.g.Eventually(func(g Gomega) {
		g.Expect(tc.Client().Get(ctx, types.NamespacedName{
			Name:      gateway.BackendTLSCABundleConfigMapName,
			Namespace: tc.gatewayNamespace(),
		}, bundle)).To(Succeed())
		assertGatewayConfigOwnership(g, bundle)
		g.Expect(bundle.Data).To(HaveKeyWithValue(gateway.BackendTLSCABundleConfigMapKey,
			ContainSubstring("-----BEGIN CERTIFICATE-----")),
			"trust bundle ConfigMap must carry a PEM CA bundle")
	}).Should(Succeed())

	// The kube-auth-proxy backend gets a port-scoped policy pinning its
	// service DNS on the routed https port.
	policy := &gwapiv1.BackendTLSPolicy{}
	tc.g.Eventually(func(g Gomega) {
		g.Expect(tc.Client().Get(ctx, types.NamespacedName{
			Name:      kubeAuthProxyName + ".https",
			Namespace: tc.gatewayNamespace(),
		}, policy)).To(Succeed())
		assertGatewayConfigOwnership(g, policy)
		g.Expect(policy.Spec.TargetRefs).To(HaveLen(1))
		g.Expect(string(policy.Spec.TargetRefs[0].Kind)).To(Equal("Service"))
		g.Expect(string(policy.Spec.TargetRefs[0].Name)).To(Equal(kubeAuthProxyName))
		g.Expect(policy.Spec.TargetRefs[0].SectionName).NotTo(BeNil())
		g.Expect(string(*policy.Spec.TargetRefs[0].SectionName)).To(Equal("https"))
		g.Expect(string(policy.Spec.Validation.Hostname)).To(Equal(
			fmt.Sprintf("%s.%s.svc.cluster.local", kubeAuthProxyName, tc.gatewayNamespace())))
		g.Expect(policy.Spec.Validation.CACertificateRefs).To(HaveLen(1))
		g.Expect(string(policy.Spec.Validation.CACertificateRefs[0].Kind)).To(Equal("ConfigMap"))
		g.Expect(string(policy.Spec.Validation.CACertificateRefs[0].Name)).To(Equal(gateway.BackendTLSCABundleConfigMapName))
	}).Should(Succeed())

	t.Log("BackendTLSPolicy and trust bundle rendering verified")
}

// ValidateBackendTLSRejectsUntrustedCertificate proves certificate verification
// is enforced (RHAI-19666 AC3). Two identical Envoy backends ride the gateway
// through operator-owned routes: a trusted one whose certificate is issued by
// the platform service CA, and a hostile one with a self-signed certificate.
// The trusted twin must serve traffic (non-503) while the hostile twin is
// rejected with a 503 — together the two distinguish certificate rejection
// from a dead upstream. Removing the routes garbage-collects the policies.
func (tc *GatewayTestCtx) ValidateBackendTLSRejectsUntrustedCertificate(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)
	tc.SkipIfXKSCluster(t)

	ctx := tc.Context()

	// The backends reuse the Envoy image the operator's own gateway pods run:
	// it is pullable wherever the gateway is deployed, mirrored registries
	// included, and needs no configuration beyond a static bootstrap.
	envoyImage := tc.getGatewayImage(t)
	if envoyImage == "" {
		t.Skip("no gateway pods found; backend TLS verification needs the gateway's Envoy image")
	}

	// The applications namespace is admitted by the gateway listener and
	// managed by the operator, so the operator renders the policies there.
	appsNamespace := tc.AppsNamespace

	// One bootstrap ConfigMap is shared by both backends.
	envoyConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: backendTLSEnvoyConfigMapName, Namespace: appsNamespace},
		Data:       map[string]string{"envoy.yaml": backendTLSEnvoyBootstrap},
	}
	err := tc.Client().Create(ctx, envoyConfig)
	tc.g.Expect(client.IgnoreAlreadyExists(err)).To(Succeed())
	// A closure, not a bare defer of the Expect call: defer evaluates call
	// arguments at defer time, so the Delete itself must live inside the
	// deferred function or it would run immediately.
	defer func() {
		tc.g.Expect(client.IgnoreNotFound(tc.Client().Delete(ctx, envoyConfig))).To(Succeed())
	}()

	// Register cleanup before setup: a readiness failure inside the setup
	// helper aborts the test's deferred calls, so a defer registered after a
	// failed create would leak the backend objects. The cleanup tolerates
	// resources that were never created.
	defer tc.deleteBackendTLSBackend(t, backendTLSTrustedName, appsNamespace)
	tc.createBackendTLSStaticBackend(t, envoyImage, backendTLSTrustedName, appsNamespace, true)
	defer tc.deleteBackendTLSBackend(t, backendTLSHostileName, appsNamespace)
	tc.createBackendTLSStaticBackend(t, envoyImage, backendTLSHostileName, appsNamespace, false)

	// A route that never attached to the gateway would send the probe to the
	// catch-all backend instead of ours: assert acceptance so a routing
	// failure cannot masquerade as a TLS verdict.
	for _, backend := range []string{backendTLSTrustedName, backendTLSHostileName} {
		backendName := backend
		tc.g.Eventually(func(g Gomega) {
			route := &gwapiv1.HTTPRoute{}
			g.Expect(tc.Client().Get(ctx, types.NamespacedName{
				Name: backendName, Namespace: appsNamespace,
			}, route)).To(Succeed())
			g.Expect(route.Status.Parents).NotTo(BeEmpty())
			accepted := meta.FindStatusCondition(route.Status.Parents[0].Conditions, string(gwapiv1.RouteConditionAccepted))
			g.Expect(accepted).NotTo(BeNil())
			g.Expect(accepted.Status).To(Equal(metav1.ConditionTrue),
				"route %s must attach to the gateway: %s", backendName, accepted.Message)
		}).Should(Succeed())
	}

	// The operator picks the routes up and renders a policy per backend: this
	// also proves the label-scoped backend enumeration follows new routes.
	for _, backend := range []string{backendTLSTrustedName, backendTLSHostileName} {
		policy := &gwapiv1.BackendTLSPolicy{}
		backendName := backend
		tc.g.Eventually(func(g Gomega) {
			g.Expect(tc.Client().Get(ctx, types.NamespacedName{
				Name:      backendName,
				Namespace: appsNamespace,
			}, policy)).To(Succeed())
		}).Should(Succeed(), "operator must render a BackendTLSPolicy for the new operator-owned route %s", backendName)
	}

	gatewayURL := fmt.Sprintf("https://%s", tc.getExpectedGatewayHostname(t))
	token := tc.createBackendTLSTestToken(t, appsNamespace)

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		// Redirects are never followed: a followed redirect would report the
		// destination's status (the OAuth login page answers 200) and weaken
		// every probe against it.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				// #nosec G402 -- e2e test environment; the gateway serves a cluster certificate.
				InsecureSkipVerify: true,
			},
		},
	}

	// Trusted twin: the service-CA-signed backend must answer with the
	// static body itself. This is the positive control that makes the
	// hostile twin's 503 meaningful.
	tc.g.Eventually(func(g Gomega) {
		status, body := tc.probeBackendTLSPath(g, ctx, httpClient, gatewayURL+backendTLSTrustedPath, token)
		g.Expect(status).To(Equal(http.StatusOK),
			"backend with a service-CA-signed certificate must not be rejected")
		g.Expect(body).To(Equal("ok"),
			"the trusted backend itself must answer, not some other hop")
	}).Should(Succeed())

	// Hostile twin: the self-signed backend must be rejected with a 503
	// (upstream TLS verification failure).
	tc.g.Eventually(func(g Gomega) {
		status, _ := tc.probeBackendTLSPath(g, ctx, httpClient, gatewayURL+backendTLSHostilePath, token)
		g.Expect(status).To(Equal(http.StatusServiceUnavailable),
			"gateway must reject a backend whose certificate does not chain to the trust bundle")
	}).Should(Succeed())

	// Isolation: the auth-proxy hop keeps working while the hostile backend is
	// rejected. The hop's health signal is the 302 login redirect itself.
	resp, err := httpClient.Get(gatewayURL + authProxyOAuth2Path + "/start") //nolint:noctx // e2e probe
	tc.g.Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	tc.g.Expect(resp.StatusCode).To(Equal(http.StatusFound),
		"auth-proxy hop must stay healthy while another backend is rejected")

	// Removing a route garbage-collects its policy.
	tc.g.Expect(tc.Client().Delete(ctx, &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: backendTLSHostileName, Namespace: appsNamespace},
	})).To(Succeed())
	tc.g.Eventually(func() error {
		err := tc.Client().Get(ctx, types.NamespacedName{
			Name:      backendTLSHostileName,
			Namespace: appsNamespace,
		}, &gwapiv1.BackendTLSPolicy{})
		if err == nil {
			return fmt.Errorf("BackendTLSPolicy %s/%s still exists", appsNamespace, backendTLSHostileName)
		}
		return client.IgnoreNotFound(err)
	}).Should(Succeed(), "BackendTLSPolicy must be garbage-collected after its route is removed")

	t.Log("Backend TLS verification enforcement and GC verified")
}

// probeBackendTLSPath sends an authenticated request through the gateway and
// returns the response status code and body.
func (tc *GatewayTestCtx) probeBackendTLSPath(g Gomega, ctx context.Context, httpClient *http.Client, url, token string) (int, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	g.Expect(err).NotTo(HaveOccurred())
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	g.Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	g.Expect(err).NotTo(HaveOccurred())

	return resp.StatusCode, string(body)
}

// getGatewayImage returns the container image the operator's default gateway
// pods run (the Istio data plane, Envoy), or an empty string when no gateway
// pod exists. The static TLS backends reuse it: wherever the gateway runs, the
// image is pullable on that cluster by definition, including disconnected
// clusters where the ref already points at a local mirror.
func (tc *GatewayTestCtx) getGatewayImage(t *testing.T) string {
	t.Helper()

	pods := &corev1.PodList{}
	err := tc.Client().List(tc.Context(), pods,
		client.InNamespace(tc.gatewayNamespace()),
		client.MatchingLabels{"gateway.networking.k8s.io/gateway-name": tc.gatewayName()})
	if err != nil {
		return ""
	}

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			if container.Image != "" {
				return container.Image
			}
		}
	}

	return ""
}

// createBackendTLSStaticBackend deploys a static-200 HTTPS backend (Envoy with
// the shared bootstrap) plus an operator-owned HTTPRoute on the default
// gateway. When trusted is true the Service carries the OpenShift serving-cert
// annotation, so the platform issues a service-CA-signed certificate;
// otherwise a self-signed certificate is generated and mounted.
func (tc *GatewayTestCtx) createBackendTLSStaticBackend(t *testing.T, image, name, namespace string, trusted bool) {
	t.Helper()

	ctx := tc.Context()

	serviceMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}
	if trusted {
		serviceMeta.Annotations = map[string]string{
			"service.beta.openshift.io/serving-cert-secret-name": name + "-tls",
		}
	} else {
		certPEM, keyPEM, err := generateBackendTLSHostileCert(
			fmt.Sprintf("%s.%s.svc.cluster.local", name, namespace))
		tc.g.Expect(err).NotTo(HaveOccurred(), "failed to generate the hostile backend certificate")
		tlsSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-tls", Namespace: namespace},
			Type:       corev1.SecretTypeTLS,
			Data:       map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
		}
		err = tc.Client().Create(ctx, tlsSecret)
		tc.g.Expect(client.IgnoreAlreadyExists(err)).To(Succeed())
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Replicas: &[]int32{1}[0],
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    "server",
						Image:   image,
						Command: []string{"envoy"},
						Args:    []string{"-c", "/etc/envoy/envoy.yaml", "--base-id", "1"},
						Ports:   []corev1.ContainerPort{{Name: "https", ContainerPort: backendTLSPort}},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "config", MountPath: "/etc/envoy", ReadOnly: true},
							{Name: "tls", MountPath: "/etc/tls/private", ReadOnly: true},
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{
									Port: intstr.FromInt32(backendTLSPort),
								},
							},
						},
					}},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: backendTLSEnvoyConfigMapName},
								},
							},
						},
						{
							Name: "tls",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{SecretName: name + "-tls"},
							},
						},
					},
				},
			},
		},
	}
	err := tc.Client().Create(ctx, deployment)
	tc.g.Expect(client.IgnoreAlreadyExists(err)).To(Succeed())

	service := &corev1.Service{
		ObjectMeta: serviceMeta,
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": name},
			Ports: []corev1.ServicePort{{
				Name:       "https",
				Port:       backendTLSPort,
				TargetPort: intstr.FromInt32(backendTLSPort),
			}},
		},
	}
	err = tc.Client().Create(ctx, service)
	tc.g.Expect(client.IgnoreAlreadyExists(err)).To(Succeed())

	pathValue := "/" + name
	backendPort := backendTLSPort
	gatewayNamespace := tc.gatewayNamespace()
	route := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			// The platform part-of label is what makes the route
			// operator-owned for the backend TLS enumeration.
			Labels: map[string]string{labels.PlatformPartOf: "backend-tls-e2e"},
		},
		Spec: gwapiv1.HTTPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				// Group and kind default to Gateway in
				// gateway.networking.k8s.io for parentRefs. The namespace is
				// set explicitly: Gateway API resolves an unset one to the
				// route's own namespace, where no gateway exists, and the
				// route would never attach.
				ParentRefs: []gwapiv1.ParentReference{{
					Name:      gwapiv1.ObjectName(tc.gatewayName()),
					Namespace: (*gwapiv1.Namespace)(&gatewayNamespace),
				}},
			},
			Rules: []gwapiv1.HTTPRouteRule{{
				Matches: []gwapiv1.HTTPRouteMatch{{
					Path: &gwapiv1.HTTPPathMatch{
						Type:  &backendTLSPathType,
						Value: &pathValue,
					},
				}},
				BackendRefs: []gwapiv1.HTTPBackendRef{{
					BackendRef: gwapiv1.BackendRef{
						BackendObjectReference: gwapiv1.BackendObjectReference{
							Name: gwapiv1.ObjectName(name),
							Port: &backendPort,
						},
					},
				}},
			}},
		},
	}
	err = tc.Client().Create(ctx, route)
	tc.g.Expect(client.IgnoreAlreadyExists(err)).To(Succeed())

	// Wait for the backend to serve TLS before probing through the gateway,
	// so the verification result is about the certificate, not a missing
	// endpoint.
	tc.g.Eventually(func(g Gomega) bool {
		ready := &appsv1.Deployment{}
		g.Expect(tc.Client().Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		}, ready)).To(Succeed())
		return ready.Status.ReadyReplicas > 0
	}).Should(BeTrue(), "backend deployment %s must become ready", name)
}

func (tc *GatewayTestCtx) deleteBackendTLSBackend(t *testing.T, name, namespace string) {
	t.Helper()

	ctx := tc.Context()
	for _, obj := range []client.Object{
		&gwapiv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name + "-tls", Namespace: namespace}},
	} {
		tc.g.Expect(client.IgnoreNotFound(tc.Client().Delete(ctx, obj))).To(Succeed())
	}
}

// createBackendTLSTestToken returns a service-account bearer token that
// kube-auth-proxy accepts (Kubernetes TokenReview); unauthenticated requests
// are redirected before reaching the routed backend.
func (tc *GatewayTestCtx) createBackendTLSTestToken(t *testing.T, namespace string) string {
	t.Helper()

	ctx := tc.Context()

	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: backendTLSHostileName + "-reader", Namespace: namespace},
	}
	err := tc.Client().Create(ctx, serviceAccount)
	tc.g.Expect(client.IgnoreAlreadyExists(err)).To(Succeed())

	tokenExpiration := backendTLSTokenExpiration
	tokenRequest := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: &tokenExpiration,
		},
	}
	err = tc.Client().SubResource("token").Create(ctx, serviceAccount, tokenRequest)
	tc.g.Expect(err).NotTo(HaveOccurred(), "failed to request a service account token")

	return tokenRequest.Status.Token
}

// generateBackendTLSHostileCert creates a self-signed certificate for the
// given DNS name. It is deliberately not signed by any CA in the operator
// trust bundle.
func generateBackendTLSHostileCert(dnsName string) ([]byte, []byte, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate a private key: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: dnsName},
		DNSNames:     []string{dnsName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create the certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	return certPEM, keyPEM, nil
}

// backendTLSPathType is the route path match type shared by the backend TLS
// specs.
var backendTLSPathType = gwapiv1.PathMatchPathPrefix

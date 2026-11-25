//go:build !integration

//nolint:testpackage
package gateway

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

const (
	testPEMA = `-----BEGIN CERTIFICATE-----
AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
-----END CERTIFICATE-----
`
	testPEMB = `-----BEGIN CERTIFICATE-----
BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB
BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB
-----END CERTIFICATE-----
`
)

func setClusterType(t *testing.T, clusterType string) {
	t.Helper()
	original := cluster.GetClusterInfo()
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: clusterType})
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
}

// gatewayRoute builds an operator-owned HTTPRoute (platform part-of label)
// attached to one of the operator's gateways.
func gatewayRoute(name, namespace string, gatewayName string, backendNamespace, backendName string, backendPort int32) *gwapiv1.HTTPRoute {
	return &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{labels.PlatformPartOf: "test-component"},
		},
		Spec: gwapiv1.HTTPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{{
					Name:      gwapiv1.ObjectName(gatewayName),
					Namespace: (*gwapiv1.Namespace)(&routeGatewayNamespace),
				}},
			},
			Rules: []gwapiv1.HTTPRouteRule{{
				BackendRefs: []gwapiv1.HTTPBackendRef{{
					BackendRef: gwapiv1.BackendRef{
						BackendObjectReference: gwapiv1.BackendObjectReference{
							Name:      gwapiv1.ObjectName(backendName),
							Namespace: (*gwapiv1.Namespace)(&backendNamespace),
							Port:      &backendPort,
						},
					},
				}},
			}},
		},
	}
}

var routeGatewayNamespace = "openshift-ingress"

// backendTLSCRD builds the BackendTLSPolicy CRD object as served: Established
// with a v1 version that is served.
func backendTLSCRD() *unstructured.Unstructured {
	crd := &unstructured.Unstructured{}
	crd.SetGroupVersionKind(crdGVK)
	crd.SetName(gvk.BackendTLSPolicyCRDName)
	_ = unstructured.SetNestedSlice(crd.Object, []any{
		map[string]any{"type": "Established", "status": "True"},
	}, "status", "conditions")
	_ = unstructured.SetNestedSlice(crd.Object, []any{
		map[string]any{"name": "v1", "served": true, "storage": true},
	}, "spec", "versions")
	return crd
}

// backendTLSCRDWithoutV1 builds an Established BackendTLSPolicy CRD that does
// not serve v1 — the shape Gateway API v1.3 installs (v1alpha3 only).
func backendTLSCRDWithoutV1() *unstructured.Unstructured {
	crd := backendTLSCRD()
	_ = unstructured.SetNestedSlice(crd.Object, []any{
		map[string]any{"name": "v1alpha3", "served": true, "storage": true},
	}, "spec", "versions")
	return crd
}

func TestMergePEM(t *testing.T) {
	t.Run("concatenates distinct bundles", func(t *testing.T) {
		g := gomega.NewWithT(t)
		got := mergePEM(testPEMA, testPEMB)
		g.Expect(got).To(gomega.ContainSubstring("AAAAAAAA"))
		g.Expect(got).To(gomega.ContainSubstring("BBBBBBBB"))
	})

	t.Run("drops duplicate blocks", func(t *testing.T) {
		g := gomega.NewWithT(t)
		got := mergePEM(testPEMA, testPEMB, testPEMA)
		g.Expect(got).To(gomega.Equal(testPEMA + testPEMB))
	})

	t.Run("ignores empty bundles", func(t *testing.T) {
		g := gomega.NewWithT(t)
		got := mergePEM("", testPEMA, "")
		g.Expect(got).To(gomega.Equal(testPEMA))
	})

	t.Run("all empty yields empty", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(mergePEM("", "")).To(gomega.Equal(""))
	})
}

func TestBackendTLSPolicyCRDServed(t *testing.T) {
	ctx := context.Background()

	t.Run("served CRD with Established condition reports true", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cli := setupTestClient().WithObjects(backendTLSCRD()).Build()

		served, err := backendTLSPolicyCRDServed(ctx, cli)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(served).To(gomega.BeTrue())
	})

	t.Run("missing CRD reports false without error", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cli := setupTestClient().Build()

		served, err := backendTLSPolicyCRDServed(ctx, cli)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(served).To(gomega.BeFalse())
	})

	t.Run("Established CRD that does not serve v1 reports false", func(t *testing.T) {
		g := gomega.NewWithT(t)
		// Gateway API v1.3 installs BackendTLSPolicy as v1alpha3 only: the
		// CRD is Established, but v1 objects cannot be rendered or watched.
		cli := setupTestClient().WithObjects(backendTLSCRDWithoutV1()).Build()

		served, err := backendTLSPolicyCRDServed(ctx, cli)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(served).To(gomega.BeFalse())
	})

	t.Run("Established CRD with v1 not served reports false", func(t *testing.T) {
		g := gomega.NewWithT(t)
		crd := backendTLSCRD()
		_ = unstructured.SetNestedSlice(crd.Object, []any{
			map[string]any{"name": "v1", "served": false, "storage": false},
			map[string]any{"name": "v1alpha3", "served": true, "storage": true},
		}, "spec", "versions")
		cli := setupTestClient().WithObjects(crd).Build()

		served, err := backendTLSPolicyCRDServed(ctx, cli)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(served).To(gomega.BeFalse())
	})
}

func TestFetchServiceCA(t *testing.T) {
	ctx := context.Background()

	t.Run("returns the CA from the gateway namespace ConfigMap on OpenShift", func(t *testing.T) {
		g := gomega.NewWithT(t)
		setClusterType(t, cluster.ClusterTypeOpenShift)

		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: ServiceCAConfigMapName, Namespace: GetGatewayNamespace()},
			Data:       map[string]string{ServiceCAConfigMapKey: testPEMA},
		}
		cli := setupTestClient().WithObjects(cm).Build()

		ca, err := fetchServiceCA(ctx, cli)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(ca).To(gomega.Equal(testPEMA))
	})

	t.Run("skips (empty) on non-OpenShift clusters", func(t *testing.T) {
		g := gomega.NewWithT(t)
		setClusterType(t, cluster.ClusterTypeKubernetes)

		cli := setupTestClient().Build()

		ca, err := fetchServiceCA(ctx, cli)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(ca).To(gomega.Equal(""))
	})

	t.Run("errors when the ConfigMap is missing on OpenShift", func(t *testing.T) {
		g := gomega.NewWithT(t)
		setClusterType(t, cluster.ClusterTypeOpenShift)

		cli := setupTestClient().Build()

		_, err := fetchServiceCA(ctx, cli)
		g.Expect(err).To(gomega.HaveOccurred())
	})
}

func TestFetchPlatformIssuerCA(t *testing.T) {
	ctx := context.Background()

	clusterIssuer := func(secretName string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(gvk.CertManagerClusterIssuer)
		u.SetName("opendatahub-ca-issuer")
		_ = unstructured.SetNestedField(u.Object, secretName, "spec", "ca", "secretName")
		return u
	}

	namespacedIssuer := func(secretName string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(gvk.CertManagerIssuer)
		u.SetNamespace(GetGatewayNamespace())
		u.SetName("gateway-issuer")
		_ = unstructured.SetNestedField(u.Object, secretName, "spec", "ca", "secretName")
		return u
	}

	t.Run("resolves a ClusterIssuer CA secret from the cert-manager namespace", func(t *testing.T) {
		g := gomega.NewWithT(t)
		setClusterType(t, cluster.ClusterTypeOpenShift)

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "opendatahub-ca", Namespace: "cert-manager"},
			Data:       map[string][]byte{"ca.crt": []byte(testPEMB)},
		}
		cli := setupTestClient().WithObjects(clusterIssuer("opendatahub-ca"), secret).Build()

		ca, err := fetchPlatformIssuerCA(ctx, cli, cli, nil)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(ca).To(gomega.Equal(testPEMB))
	})

	t.Run("resolves a namespaced Issuer CA secret from the gateway namespace", func(t *testing.T) {
		g := gomega.NewWithT(t)
		setClusterType(t, cluster.ClusterTypeKubernetes)

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "gateway-ca", Namespace: GetGatewayNamespace()},
			Data:       map[string][]byte{"ca.crt": []byte(testPEMA)},
		}
		cli := setupTestClient().WithObjects(namespacedIssuer("gateway-ca"), secret).Build()

		certSpec := &infrav1.CertificateSpec{IssuerRef: &infrav1.IssuerRef{Name: "gateway-issuer", Kind: "Issuer"}}
		ca, err := fetchPlatformIssuerCA(ctx, cli, cli, certSpec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(ca).To(gomega.Equal(testPEMA))
	})

	t.Run("skips (empty) when the issuer does not exist", func(t *testing.T) {
		g := gomega.NewWithT(t)
		setClusterType(t, cluster.ClusterTypeOpenShift)

		cli := setupTestClient().Build()

		ca, err := fetchPlatformIssuerCA(ctx, cli, cli, nil)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(ca).To(gomega.Equal(""))
	})

	t.Run("skips (empty) when the CA secret is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		setClusterType(t, cluster.ClusterTypeOpenShift)

		cli := setupTestClient().WithObjects(clusterIssuer("opendatahub-ca")).Build()

		ca, err := fetchPlatformIssuerCA(ctx, cli, cli, nil)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(ca).To(gomega.Equal(""))
	})

	t.Run("fails when the CA Secret read errors for another reason", func(t *testing.T) {
		g := gomega.NewWithT(t)
		setClusterType(t, cluster.ClusterTypeOpenShift)

		cli := setupTestClient().WithObjects(clusterIssuer("opendatahub-ca")).Build()

		// A Forbidden or timeout is not "no PKI": returning an empty CA here
		// would deploy a reduced trust bundle.
		ca, err := fetchPlatformIssuerCA(ctx, cli, errorReader{err: k8serr.NewForbidden(
			schema.GroupResource{Resource: "secrets"}, "denied", nil)}, nil)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(ca).To(gomega.Equal(""))
	})
}

// errorReader is a client.Reader whose every call fails, standing in for the
// uncached reader when the API server rejects a read.
type errorReader struct{ err error }

func (r errorReader) Get(_ context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
	return r.err
}

func (r errorReader) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	return r.err
}

func TestEnumerateBackendTLSTargets(t *testing.T) {
	ctx := context.Background()
	defaultGateway := GetDefaultGatewayName()
	appsNamespace := cluster.GetApplicationNamespace()

	t.Run("collects Service backends of operator-owned routes in allowed namespaces", func(t *testing.T) {
		g := gomega.NewWithT(t)

		route := gatewayRoute("dashboard-route", appsNamespace, defaultGateway, appsNamespace, "rhods-dashboard", 8443)
		cli := setupTestClient().WithObjects(route).Build()

		backends, err := enumerateBackendTLSTargets(ctx, cli, []string{defaultGateway})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(backends).To(gomega.HaveLen(1))
		g.Expect(backends[0].Namespace).To(gomega.Equal(appsNamespace))
		g.Expect(backends[0].Name).To(gomega.Equal("rhods-dashboard"))
		g.Expect(backends[0].Ports).To(gomega.ConsistOf(int32(8443)))
	})

	t.Run("collects gateway-namespace backends", func(t *testing.T) {
		g := gomega.NewWithT(t)

		route := gatewayRoute("callback-route", GetGatewayNamespace(), defaultGateway, GetGatewayNamespace(), "kube-auth-proxy", 8443)
		cli := setupTestClient().WithObjects(route).Build()

		backends, err := enumerateBackendTLSTargets(ctx, cli, []string{defaultGateway})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(backends).To(gomega.HaveLen(1))
		g.Expect(backends[0].Name).To(gomega.Equal("kube-auth-proxy"))
	})

	t.Run("excludes foreign routes without the platform part-of label", func(t *testing.T) {
		g := gomega.NewWithT(t)

		foreign := gatewayRoute("user-app", appsNamespace, defaultGateway, appsNamespace, "user-svc", 443)
		foreign.Labels = map[string]string{"app": "user"}
		cli := setupTestClient().WithObjects(foreign).Build()

		backends, err := enumerateBackendTLSTargets(ctx, cli, []string{defaultGateway})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(backends).To(gomega.BeEmpty())
	})

	t.Run("excludes labeled routes from namespaces the operator does not manage", func(t *testing.T) {
		g := gomega.NewWithT(t)

		forged := gatewayRoute("forged-route", "user-namespace", defaultGateway, "user-namespace", "user-svc", 443)
		cli := setupTestClient().WithObjects(forged).Build()

		backends, err := enumerateBackendTLSTargets(ctx, cli, []string{defaultGateway})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(backends).To(gomega.BeEmpty())
	})

	t.Run("excludes backends outside the allowed namespaces", func(t *testing.T) {
		g := gomega.NewWithT(t)

		route := gatewayRoute("module-route", appsNamespace, defaultGateway, "rhoai-model-registries", "model-catalog", 8443)
		cli := setupTestClient().WithObjects(route).Build()

		backends, err := enumerateBackendTLSTargets(ctx, cli, []string{defaultGateway})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(backends).To(gomega.BeEmpty())
	})

	t.Run("excludes routes attached to other gateways or non-Gateway kinds", func(t *testing.T) {
		g := gomega.NewWithT(t)

		otherGateway := gatewayRoute("other-gw-route", appsNamespace, "some-other-gateway", appsNamespace, "rhods-dashboard", 8443)
		wrongKind := gatewayRoute("wrong-kind-route", appsNamespace, defaultGateway, appsNamespace, "rhods-dashboard", 8443)
		notGateway := "NotGateway"
		wrongKind.Spec.ParentRefs[0].Kind = (*gwapiv1.Kind)(&notGateway)
		cli := setupTestClient().WithObjects(otherGateway, wrongKind).Build()

		backends, err := enumerateBackendTLSTargets(ctx, cli, []string{defaultGateway})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(backends).To(gomega.BeEmpty())
	})

	t.Run("excludes routes whose parentRef leaves the namespace unset", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Gateway API resolves an unset parentRef namespace to the route's
		// own namespace: such a route in the applications namespace never
		// attaches to the operator's gateway and must not pull a policy in.
		implicit := gatewayRoute("implicit-ns-route", appsNamespace, defaultGateway, appsNamespace, "rhods-dashboard", 8443)
		implicit.Spec.ParentRefs[0].Namespace = nil
		explicit := gatewayRoute("explicit-ns-route", appsNamespace, defaultGateway, appsNamespace, "rhods-dashboard", 8443)
		cli := setupTestClient().WithObjects(implicit, explicit).Build()

		backends, err := enumerateBackendTLSTargets(ctx, cli, []string{defaultGateway})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(backends).To(gomega.HaveLen(1))
	})

	t.Run("deduplicates backends referenced by multiple routes and merges ports", func(t *testing.T) {
		g := gomega.NewWithT(t)

		first := gatewayRoute("route-a", GetGatewayNamespace(), defaultGateway, appsNamespace, "rhods-dashboard", 8443)
		second := gatewayRoute("route-b", appsNamespace, defaultGateway, appsNamespace, "rhods-dashboard", 8443)
		third := gatewayRoute("route-c", appsNamespace, defaultGateway, appsNamespace, "rhods-dashboard", 8943)
		cli := setupTestClient().WithObjects(first, second, third).Build()

		backends, err := enumerateBackendTLSTargets(ctx, cli, []string{defaultGateway})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(backends).To(gomega.HaveLen(1))
		g.Expect(backends[0].Ports).To(gomega.ConsistOf(int32(8443), int32(8943)))
	})

	t.Run("excludes non-Service backend references", func(t *testing.T) {
		g := gomega.NewWithT(t)

		route := gatewayRoute("weird-route", appsNamespace, defaultGateway, appsNamespace, "some-backend", 8443)
		otherKind := "ServiceImport"
		route.Spec.Rules[0].BackendRefs[0].Kind = (*gwapiv1.Kind)(&otherKind)
		cli := setupTestClient().WithObjects(route).Build()

		backends, err := enumerateBackendTLSTargets(ctx, cli, []string{defaultGateway})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(backends).To(gomega.BeEmpty())
	})

	t.Run("no routes yields empty without error", func(t *testing.T) {
		g := gomega.NewWithT(t)

		cli := setupTestClient().Build()

		backends, err := enumerateBackendTLSTargets(ctx, cli, []string{defaultGateway})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(backends).To(gomega.BeEmpty())
	})
}

func TestCreateBackendTLSPolicies(t *testing.T) {
	ctx := context.Background()
	defaultGateway := GetDefaultGatewayName()
	appsNamespace := cluster.GetApplicationNamespace()

	newRequest := func(cli client.Client) *odhtypes.ReconciliationRequest {
		return &odhtypes.ReconciliationRequest{
			Client:   cli,
			Instance: &serviceApi.GatewayConfig{ObjectMeta: metav1.ObjectMeta{Name: "default-gateway"}},
		}
	}

	t.Run("skips rendering when the BackendTLSPolicy CRD is not served", func(t *testing.T) {
		g := gomega.NewWithT(t)
		setClusterType(t, cluster.ClusterTypeOpenShift)

		route := gatewayRoute("callback-route", GetGatewayNamespace(), defaultGateway, GetGatewayNamespace(), "kube-auth-proxy", 8443)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: ServiceCAConfigMapName, Namespace: GetGatewayNamespace()},
			Data:       map[string]string{ServiceCAConfigMapKey: testPEMA},
		}
		cli := setupTestClient().WithObjects(route, cm).Build()
		rr := newRequest(cli)

		g.Expect(createBackendTLSPolicies(ctx, rr, cli)).To(gomega.Succeed())
		g.Expect(rr.Resources).To(gomega.BeEmpty())
	})

	t.Run("skips rendering when no trust bundle can be assembled", func(t *testing.T) {
		g := gomega.NewWithT(t)
		setClusterType(t, cluster.ClusterTypeKubernetes)

		// Resolve the gateway name for the selected cluster type and drop the
		// hardcoded parentRef namespace so the route attaches to the XKS
		// gateway; an unattached route would take the no-backends path and
		// this subtest would pass vacuously.
		route := gatewayRoute("callback-route", GetGatewayNamespace(), GetDefaultGatewayName(), GetGatewayNamespace(), "kube-auth-proxy", 8443)
		route.Spec.ParentRefs[0].Namespace = nil
		cli := setupTestClient().WithObjects(backendTLSCRD(), route).Build()
		rr := newRequest(cli)

		backends, err := enumerateBackendTLSTargets(ctx, cli, []string{GetDefaultGatewayName()})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(backends).To(gomega.HaveLen(1),
			"the route must be enumerated, so the empty bundle — not an empty backend set — is what skips rendering")

		g.Expect(createBackendTLSPolicies(ctx, rr, cli)).To(gomega.Succeed())
		g.Expect(rr.Resources).To(gomega.BeEmpty())

		_, recorded := backendTLSDesiredArtifacts(rr)
		g.Expect(recorded).To(gomega.BeFalse(),
			"an unusable bundle must preserve the deployed artifacts, not record an empty desired set")
	})

	t.Run("fails instead of deploying a reduced bundle when the platform CA Secret read fails", func(t *testing.T) {
		g := gomega.NewWithT(t)
		setClusterType(t, cluster.ClusterTypeOpenShift)

		route := gatewayRoute("callback-route", GetGatewayNamespace(), defaultGateway, GetGatewayNamespace(), "kube-auth-proxy", 8443)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: ServiceCAConfigMapName, Namespace: GetGatewayNamespace()},
			Data:       map[string]string{ServiceCAConfigMapKey: testPEMA},
		}
		issuer := &unstructured.Unstructured{}
		issuer.SetGroupVersionKind(gvk.CertManagerIssuer)
		issuer.SetNamespace(GetGatewayNamespace())
		issuer.SetName("gateway-issuer")
		_ = unstructured.SetNestedField(issuer.Object, "gateway-ca", "spec", "ca", "secretName")
		cli := setupTestClient().WithObjects(backendTLSCRD(), route, cm, issuer).Build()
		rr := newRequest(cli)
		gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
		g.Expect(ok).To(gomega.BeTrue())
		gatewayConfig.Spec.Certificate = &infrav1.CertificateSpec{
			IssuerRef: &infrav1.IssuerRef{Name: "gateway-issuer", Kind: "Issuer"},
		}

		// The service CA alone would assemble a bundle; deploying it without
		// the platform issuer CA would stop trusting platform-signed backends.
		err := createBackendTLSPolicies(ctx, rr, errorReader{err: k8serr.NewForbidden(
			schema.GroupResource{Resource: "secrets"}, "denied", nil)})
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(rr.Resources).To(gomega.BeEmpty(), "no reduced bundle may be deployed on a failed platform CA read")
	})

	t.Run("renders a shared bundle ConfigMap and per-port policies for multi-port services", func(t *testing.T) {
		g := gomega.NewWithT(t)
		setClusterType(t, cluster.ClusterTypeOpenShift)

		route := gatewayRoute("callback-route", GetGatewayNamespace(), defaultGateway, GetGatewayNamespace(), "kube-auth-proxy", 8443)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: ServiceCAConfigMapName, Namespace: GetGatewayNamespace()},
			Data:       map[string]string{ServiceCAConfigMapKey: testPEMA},
		}
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "kube-auth-proxy", Namespace: GetGatewayNamespace()},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Name: "https", Port: 8443},
					{Name: "metrics", Port: 9000},
				},
			},
		}
		cli := setupTestClient().WithObjects(backendTLSCRD(), route, cm, service).Build()
		rr := newRequest(cli)

		g.Expect(createBackendTLSPolicies(ctx, rr, cli)).To(gomega.Succeed())
		g.Expect(rr.Resources).To(gomega.HaveLen(2))

		bundle := &corev1.ConfigMap{}
		g.Expect(rr.Resources[0].GetKind()).To(gomega.Equal("ConfigMap"))
		g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(rr.Resources[0].Object, bundle)).To(gomega.Succeed())
		g.Expect(bundle.Name).To(gomega.Equal(BackendTLSCABundleConfigMapName))
		g.Expect(bundle.Namespace).To(gomega.Equal(GetGatewayNamespace()))
		g.Expect(bundle.Data).To(gomega.HaveKeyWithValue(BackendTLSCABundleConfigMapKey, testPEMA))

		policy := &gwapiv1.BackendTLSPolicy{}
		g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(rr.Resources[1].Object, policy)).To(gomega.Succeed())
		g.Expect(policy.Name).To(gomega.Equal("kube-auth-proxy.https"))
		g.Expect(policy.Spec.TargetRefs).To(gomega.HaveLen(1))
		g.Expect(policy.Spec.TargetRefs[0].SectionName).NotTo(gomega.BeNil())
		g.Expect(string(*policy.Spec.TargetRefs[0].SectionName)).To(gomega.Equal("https"))
		g.Expect(string(policy.Spec.Validation.CACertificateRefs[0].Name)).To(gomega.Equal(BackendTLSCABundleConfigMapName))
	})

	t.Run("renders one service-wide policy for single-port services across namespaces", func(t *testing.T) {
		g := gomega.NewWithT(t)
		setClusterType(t, cluster.ClusterTypeOpenShift)

		gatewayRouteObj := gatewayRoute("callback-route", GetGatewayNamespace(), defaultGateway, GetGatewayNamespace(), "kube-auth-proxy", 8443)
		dashboardRoute := gatewayRoute("dashboard-route", appsNamespace, defaultGateway, appsNamespace, "rhods-dashboard", 8443)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: ServiceCAConfigMapName, Namespace: GetGatewayNamespace()},
			Data:       map[string]string{ServiceCAConfigMapKey: testPEMA},
		}
		dashboardService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "rhods-dashboard", Namespace: appsNamespace},
			Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "https", Port: 8443}}},
		}
		cli := setupTestClient().WithObjects(backendTLSCRD(), gatewayRouteObj, dashboardRoute, cm, dashboardService).Build()
		rr := newRequest(cli)

		g.Expect(createBackendTLSPolicies(ctx, rr, cli)).To(gomega.Succeed())
		// Two bundle ConfigMaps (one per namespace) + two policies.
		g.Expect(rr.Resources).To(gomega.HaveLen(4))

		names := make([]string, 0, 4)
		for _, res := range rr.Resources {
			names = append(names, res.GetNamespace()+"/"+res.GetName())
		}
		g.Expect(names).To(gomega.ConsistOf(
			GetGatewayNamespace()+"/"+BackendTLSCABundleConfigMapName,
			appsNamespace+"/"+BackendTLSCABundleConfigMapName,
			GetGatewayNamespace()+"/kube-auth-proxy",
			appsNamespace+"/rhods-dashboard",
		))
	})

	t.Run("includes additional ingress gateways in the enumeration", func(t *testing.T) {
		g := gomega.NewWithT(t)
		setClusterType(t, cluster.ClusterTypeOpenShift)

		route := gatewayRoute("ingress-route", GetGatewayNamespace(), "extra-ingress", GetGatewayNamespace(), "extra-proxy", 8443)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: ServiceCAConfigMapName, Namespace: GetGatewayNamespace()},
			Data:       map[string]string{ServiceCAConfigMapKey: testPEMA},
		}
		cli := setupTestClient().WithObjects(backendTLSCRD(), route, cm).Build()
		rr := newRequest(cli)
		gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
		g.Expect(ok).To(gomega.BeTrue())
		gatewayConfig.Spec.AdditionalIngresses = []serviceApi.AdditionalIngress{
			{Name: "extra-ingress", Hostname: "extra.example.com"},
		}

		g.Expect(createBackendTLSPolicies(ctx, rr, cli)).To(gomega.Succeed())
		g.Expect(rr.Resources).To(gomega.HaveLen(2))
		g.Expect(rr.Resources[1].GetName()).To(gomega.Equal("extra-proxy"))
	})
}

// TestBackendTLSArtifactCollection covers the interaction between the backend
// TLS action and the gateway garbage collector: artifacts the latest reconcile
// does not render any more must become collectable without waiting for a
// GatewayConfig generation bump (RHAI-19666).
func TestBackendTLSArtifactCollection(t *testing.T) {
	ctx := context.Background()
	defaultGateway := GetDefaultGatewayName()

	artifact := func(kind schema.GroupVersionKind, namespace, name string) unstructured.Unstructured {
		obj := unstructured.Unstructured{}
		obj.SetGroupVersionKind(kind)
		obj.SetNamespace(namespace)
		obj.SetName(name)

		return obj
	}

	t.Run("keeps rendered artifacts and collects the rest", func(t *testing.T) {
		g := gomega.NewWithT(t)
		setClusterType(t, cluster.ClusterTypeOpenShift)

		route := gatewayRoute("callback-route", GetGatewayNamespace(), defaultGateway, GetGatewayNamespace(), "kube-auth-proxy", 8443)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: ServiceCAConfigMapName, Namespace: GetGatewayNamespace()},
			Data:       map[string]string{ServiceCAConfigMapKey: testPEMA},
		}
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "kube-auth-proxy", Namespace: GetGatewayNamespace()},
			Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "https", Port: 8443}}},
		}
		cli := setupTestClient().WithObjects(backendTLSCRD(), route, cm, service).Build()
		rr := &odhtypes.ReconciliationRequest{
			Client:   cli,
			Instance: &serviceApi.GatewayConfig{ObjectMeta: metav1.ObjectMeta{Name: "default-gateway"}},
		}

		g.Expect(createBackendTLSPolicies(ctx, rr, cli)).To(gomega.Succeed())

		for _, test := range []struct {
			name        string
			kind        schema.GroupVersionKind
			namespace   string
			artifact    string
			collectable bool
			reason      string
		}{
			{
				name: "rendered policy", kind: gvk.BackendTLSPolicy, namespace: GetGatewayNamespace(),
				artifact: "kube-auth-proxy", collectable: false, reason: "a policy the latest route enumeration still implies must survive",
			},
			{
				name: "policy of a removed route", kind: gvk.BackendTLSPolicy, namespace: cluster.GetApplicationNamespace(),
				artifact: "backend-tls-trusted", collectable: true, reason: "no route references this backend any more",
			},
			{
				name: "rendered bundle", kind: gvk.ConfigMap, namespace: GetGatewayNamespace(),
				artifact: BackendTLSCABundleConfigMapName, collectable: false, reason: "the bundle backs a rendered policy",
			},
			{
				name: "bundle of a namespace without backends", kind: gvk.ConfigMap, namespace: cluster.GetApplicationNamespace(),
				artifact: BackendTLSCABundleConfigMapName, collectable: true, reason: "no policy in that namespace references it",
			},
			{
				name: "unrelated ConfigMap", kind: gvk.ConfigMap, namespace: GetGatewayNamespace(),
				artifact: ServiceCAConfigMapName, collectable: false, reason: "the action must not touch ConfigMaps it does not own",
			},
		} {
			t.Run(test.name, func(t *testing.T) {
				g := gomega.NewWithT(t)
				collectable, err := gatewayResourceCollectable(rr, artifact(test.kind, test.namespace, test.artifact))
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(collectable).To(gomega.Equal(test.collectable), test.reason)
			})
		}
	})

	t.Run("collects every artifact when no route is left", func(t *testing.T) {
		g := gomega.NewWithT(t)
		setClusterType(t, cluster.ClusterTypeOpenShift)

		cli := setupTestClient().WithObjects(backendTLSCRD()).Build()
		rr := &odhtypes.ReconciliationRequest{
			Client:   cli,
			Instance: &serviceApi.GatewayConfig{ObjectMeta: metav1.ObjectMeta{Name: "default-gateway"}},
		}

		g.Expect(createBackendTLSPolicies(ctx, rr, cli)).To(gomega.Succeed())
		g.Expect(rr.Resources).To(gomega.BeEmpty())

		for _, obj := range []unstructured.Unstructured{
			artifact(gvk.BackendTLSPolicy, cluster.GetApplicationNamespace(), "backend-tls-trusted"),
			artifact(gvk.ConfigMap, cluster.GetApplicationNamespace(), BackendTLSCABundleConfigMapName),
		} {
			collectable, err := gatewayResourceCollectable(rr, obj)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(collectable).To(gomega.BeTrue(), obj.GetName())
		}
	})

	t.Run("leaves the decision to the default rules when artifacts were preserved", func(t *testing.T) {
		g := gomega.NewWithT(t)
		// The service CA ConfigMap carries no usable PEM block, so no trust
		// bundle can be assembled: the action keeps whatever is already
		// deployed and must not record a desired set that would collect it.
		setClusterType(t, cluster.ClusterTypeOpenShift)

		route := gatewayRoute("callback-route", GetGatewayNamespace(), defaultGateway, GetGatewayNamespace(), "kube-auth-proxy", 8443)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: ServiceCAConfigMapName, Namespace: GetGatewayNamespace()},
			Data:       map[string]string{ServiceCAConfigMapKey: "not-a-certificate"},
		}
		cli := setupTestClient().WithObjects(backendTLSCRD(), route, cm).Build()
		rr := &odhtypes.ReconciliationRequest{
			Client:   cli,
			Instance: &serviceApi.GatewayConfig{ObjectMeta: metav1.ObjectMeta{Name: "default-gateway", UID: "config"}},
			Release:  common.Release{Name: cluster.OpenDataHub},
		}

		g.Expect(createBackendTLSPolicies(ctx, rr, cli)).To(gomega.Succeed())
		g.Expect(rr.Resources).To(gomega.BeEmpty())

		_, recorded := backendTLSDesiredArtifacts(rr)
		g.Expect(recorded).To(gomega.BeFalse())

		obj := artifact(gvk.BackendTLSPolicy, GetGatewayNamespace(), "kube-auth-proxy")
		obj.SetAnnotations(map[string]string{
			"platform.opendatahub.io/version":             "0.0.0",
			"platform.opendatahub.io/type":                string(cluster.OpenDataHub),
			"platform.opendatahub.io/instance.uid":        "config",
			"platform.opendatahub.io/instance.generation": "0",
		})
		collectable, err := gatewayResourceCollectable(rr, obj)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(collectable).To(gomega.BeFalse(), "preserved artifacts keep the generation-based rules")
	})
}

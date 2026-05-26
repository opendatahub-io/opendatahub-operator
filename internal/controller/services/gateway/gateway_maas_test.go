//go:build !integration

//nolint:testpackage
package gateway

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

func TestIsMaaSEnabled(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		objects  []client.Object
		expected bool
	}{
		{
			name:     "returns false when no DSC exists",
			objects:  nil,
			expected: false,
		},
		{
			name: "returns false when Kserve is Removed",
			objects: []client.Object{
				newDSC(operatorv1.Removed, operatorv1.Managed),
			},
			expected: false,
		},
		{
			name: "returns false when ModelsAsService is Removed",
			objects: []client.Object{
				newDSC(operatorv1.Managed, operatorv1.Removed),
			},
			expected: false,
		},
		{
			name: "returns false when both are Removed",
			objects: []client.Object{
				newDSC(operatorv1.Removed, operatorv1.Removed),
			},
			expected: false,
		},
		{
			name: "returns true when both Kserve and ModelsAsService are Managed",
			objects: []client.Object{
				newDSC(operatorv1.Managed, operatorv1.Managed),
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			ctx := t.Context()

			builder := setupTestClient()
			if len(tc.objects) > 0 {
				builder = builder.WithObjects(tc.objects...)
			}
			cli := builder.Build()

			result := isMaaSEnabled(ctx, cli)
			g.Expect(result).To(Equal(tc.expected))
		})
	}
}

func TestMaaSGatewayConstants(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	g.Expect(MaaSGatewayClassName).To(Equal("maas-gateway-class"))
	g.Expect(MaaSGatewayName).To(Equal("maas-default-gateway"))
	g.Expect(MaaSGatewaySubdomain).To(Equal("maas"))
	g.Expect(MaaSGatewayTLSSecretName).To(Equal("maas-gateway-tls"))
}

func TestCleanupMaaSGatewayResources(t *testing.T) {
	t.Parallel()

	t.Run("deletes existing Gateway and GatewayClass", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)
		ctx := t.Context()

		existingGateway := &gwapiv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      MaaSGatewayName,
				Namespace: GatewayNamespace,
			},
			Spec: gwapiv1.GatewaySpec{
				GatewayClassName: MaaSGatewayClassName,
			},
		}
		existingClass := &gwapiv1.GatewayClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: MaaSGatewayClassName,
			},
			Spec: gwapiv1.GatewayClassSpec{
				ControllerName: GatewayControllerName,
			},
		}

		cli := setupTestClient().WithObjects(existingGateway, existingClass).Build()

		err := cleanupMaaSGatewayResources(ctx, cli)
		g.Expect(err).NotTo(HaveOccurred())

		gw := &gwapiv1.Gateway{}
		err = cli.Get(ctx, client.ObjectKey{Name: MaaSGatewayName, Namespace: GatewayNamespace}, gw)
		g.Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
		g.Expect(err).To(HaveOccurred(), "Gateway should be deleted")

		gc := &gwapiv1.GatewayClass{}
		err = cli.Get(ctx, client.ObjectKey{Name: MaaSGatewayClassName}, gc)
		g.Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
		g.Expect(err).To(HaveOccurred(), "GatewayClass should be deleted")
	})

	t.Run("succeeds when resources do not exist", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)
		ctx := t.Context()

		cli := setupTestClient().Build()

		err := cleanupMaaSGatewayResources(ctx, cli)
		g.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("deletes Gateway even when GatewayClass is absent", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)
		ctx := t.Context()

		existingGateway := &gwapiv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      MaaSGatewayName,
				Namespace: GatewayNamespace,
			},
			Spec: gwapiv1.GatewaySpec{
				GatewayClassName: MaaSGatewayClassName,
			},
		}

		cli := setupTestClient().WithObjects(existingGateway).Build()

		err := cleanupMaaSGatewayResources(ctx, cli)
		g.Expect(err).NotTo(HaveOccurred())

		gw := &gwapiv1.Gateway{}
		err = cli.Get(ctx, client.ObjectKey{Name: MaaSGatewayName, Namespace: GatewayNamespace}, gw)
		g.Expect(err).To(HaveOccurred(), "Gateway should be deleted")
	})
}

func TestCreateMaaSGateway_Disabled(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	existingGateway := &gwapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MaaSGatewayName,
			Namespace: GatewayNamespace,
		},
		Spec: gwapiv1.GatewaySpec{
			GatewayClassName: MaaSGatewayClassName,
		},
	}
	existingClass := &gwapiv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: MaaSGatewayClassName,
		},
		Spec: gwapiv1.GatewayClassSpec{
			ControllerName: GatewayControllerName,
		},
	}

	cli := setupTestClient().
		WithObjects(
			newDSC(operatorv1.Managed, operatorv1.Removed),
			existingGateway,
			existingClass,
		).Build()

	rr := newTestReconciliationRequest(cli)

	err := createMaaSGateway(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rr.Resources).To(BeEmpty(), "no resources should be added when MaaS is disabled")

	gw := &gwapiv1.Gateway{}
	err = cli.Get(ctx, client.ObjectKey{Name: MaaSGatewayName, Namespace: GatewayNamespace}, gw)
	g.Expect(err).To(HaveOccurred(), "Gateway should be cleaned up")

	gc := &gwapiv1.GatewayClass{}
	err = cli.Get(ctx, client.ObjectKey{Name: MaaSGatewayClassName}, gc)
	g.Expect(err).To(HaveOccurred(), "GatewayClass should be cleaned up")
}

func TestCreateMaaSGateway_Enabled(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	cli := setupTestClient().
		WithObjects(
			newDSC(operatorv1.Managed, operatorv1.Managed),
			newOpenshiftIngress("apps.test-cluster.example.com"),
		).Build()

	rr := newTestReconciliationRequest(cli)

	err := createMaaSGateway(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rr.Resources).To(HaveLen(2), "should add GatewayClass and Gateway")

	var foundGateway, foundGatewayClass bool
	for _, res := range rr.Resources {
		switch res.GetKind() {
		case "GatewayClass":
			foundGatewayClass = true
			g.Expect(res.GetName()).To(Equal(MaaSGatewayClassName))
			controllerName, _, _ := unstructured.NestedString(res.Object, "spec", "controllerName")
			g.Expect(controllerName).To(Equal(string(GatewayControllerName)))

		case "Gateway":
			foundGateway = true
			g.Expect(res.GetName()).To(Equal(MaaSGatewayName))
			g.Expect(res.GetNamespace()).To(Equal(GatewayNamespace))

			// Verify labels
			labels := res.GetLabels()
			g.Expect(labels).To(HaveKeyWithValue(IstioRevisionLabel, IstioRevisionValue))
			g.Expect(labels).To(HaveKeyWithValue("app.kubernetes.io/name", "maas"))
			g.Expect(labels).To(HaveKeyWithValue("app.kubernetes.io/instance", MaaSGatewayName))
			g.Expect(labels).To(HaveKeyWithValue("app.kubernetes.io/component", "gateway"))

			// Verify authorino TLS bootstrap annotation
			annotations := res.GetAnnotations()
			g.Expect(annotations).To(HaveKeyWithValue("security.opendatahub.io/authorino-tls-bootstrap", "true"))

			// Verify GatewayClassName
			className, _, _ := unstructured.NestedString(res.Object, "spec", "gatewayClassName")
			g.Expect(className).To(Equal(string(MaaSGatewayClassName)))

			// Verify listeners
			listeners, _, _ := unstructured.NestedSlice(res.Object, "spec", "listeners")
			g.Expect(listeners).To(HaveLen(2), "should have HTTP and HTTPS listeners")

			httpListener, ok := listeners[0].(map[string]interface{})
			g.Expect(ok).To(BeTrue(), "listener[0] should be a map")
			g.Expect(httpListener["name"]).To(Equal("http"))
			g.Expect(httpListener["protocol"]).To(Equal("HTTP"))
			g.Expect(httpListener["port"]).To(BeNumerically("==", 80))

			httpsListener, ok := listeners[1].(map[string]interface{})
			g.Expect(ok).To(BeTrue(), "listener[1] should be a map")
			g.Expect(httpsListener["name"]).To(Equal("https"))
			g.Expect(httpsListener["protocol"]).To(Equal("HTTPS"))
			g.Expect(httpsListener["port"]).To(BeNumerically("==", StandardHTTPSPort))

			// Verify TLS config references MaaS-specific secret
			certRefs, _, _ := unstructured.NestedSlice(httpsListener, "tls", "certificateRefs")
			g.Expect(certRefs).To(HaveLen(1))
			certRef, ok := certRefs[0].(map[string]interface{})
			g.Expect(ok).To(BeTrue(), "certRef should be a map")
			g.Expect(certRef["name"]).To(Equal(MaaSGatewayTLSSecretName))

			// Verify hostname
			hostname, _, _ := unstructured.NestedString(httpListener, "hostname")
			g.Expect(hostname).To(Equal("maas.apps.test-cluster.example.com"))
		}
	}

	g.Expect(foundGatewayClass).To(BeTrue(), "GatewayClass should be in resources")
	g.Expect(foundGateway).To(BeTrue(), "Gateway should be in resources")
}

func TestCreateMaaSGateway_NoDSC(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	cli := setupTestClient().Build()
	rr := newTestReconciliationRequest(cli)

	err := createMaaSGateway(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rr.Resources).To(BeEmpty(), "no resources when DSC does not exist")
}

func TestHandleMaaSCertificates_PreservesOriginalConfig(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	cli := setupTestClient().Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}

	origCert := &infrav1.CertificateSpec{
		Type:       infrav1.Provided,
		SecretName: "original-secret",
	}
	gatewayConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{
			Certificate: origCert,
		},
	}

	secretName, err := handleMaaSCertificates(ctx, rr, gatewayConfig, "maas.apps.example.com")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secretName).To(Equal(MaaSGatewayTLSSecretName))

	// Verify original config is restored
	g.Expect(gatewayConfig.Spec.Certificate).To(Equal(origCert))
	g.Expect(gatewayConfig.Spec.Certificate.SecretName).To(Equal("original-secret"))
}

func TestHandleMaaSCertificates_NilCertConfig(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	cli := setupTestClient().Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}

	gatewayConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{
			Certificate: nil,
		},
	}

	// Provided type would be set by default (OpenshiftDefaultIngress), but that
	// requires a real cluster. With nil cert, it creates a default spec with
	// MaaS secret name. This tests that nil cert doesn't panic.
	_, err := handleMaaSCertificates(ctx, rr, gatewayConfig, "maas.apps.example.com")
	// Error is expected here because OpenshiftDefaultIngress needs the real cluster's
	// ingress controller cert -- but we verify no nil pointer panic occurred.
	g.Expect(err).To(HaveOccurred())

	// Verify original nil config is restored
	g.Expect(gatewayConfig.Spec.Certificate).To(BeNil())
}

// --- helpers ---

func newDSC(kserveState, maasState operatorv1.ManagementState) *dscv2.DataScienceCluster {
	return &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsc",
		},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: kserveState,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: maasState,
						},
					},
				},
			},
		},
	}
}

func newTestReconciliationRequest(cli client.Client) *odhtypes.ReconciliationRequest {
	return &odhtypes.ReconciliationRequest{
		Client: cli,
		Instance: &serviceApi.GatewayConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name: serviceApi.GatewayConfigName,
			},
			Spec: serviceApi.GatewayConfigSpec{
				Certificate: &infrav1.CertificateSpec{
					Type:       infrav1.Provided,
					SecretName: MaaSGatewayTLSSecretName,
				},
			},
		},
	}
}

func newOpenshiftIngress(domain string) *unstructured.Unstructured {
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")
	_ = unstructured.SetNestedField(ingress.Object, domain, "spec", "domain")
	return ingress
}

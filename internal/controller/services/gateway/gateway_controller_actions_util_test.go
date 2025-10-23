//nolint:testpackage
package gateway

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
)

const (
	// Test constants.
	testCertSecret = "test-cert-secret"
	testDomain     = "gateway.example.com"
	odhTestCert    = "odh-cert"
	odhTestDomain  = "data-science-gateway.apps.cluster.example.com"

	// Gateway constants for testing.
	GatewayControllerName = "openshift.io/gateway-controller/v1"
)

// setupTestClient creates a fake client with the required scheme for Gateway API tests.
func setupTestClient() client.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(gwapiv1.Install(scheme))
	utilruntime.Must(serviceApi.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).Build()
}

// setupTestClientWithObjects creates a fake client with pre-existing objects.
func setupTestClientWithObjects(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(gwapiv1.Install(scheme))
	utilruntime.Must(serviceApi.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

// gatewayConfigWithCert creates a test GatewayConfig with certificate spec.
func gatewayConfigWithCert(name string, certType infrav1.CertType, secretName ...string) *serviceApi.GatewayConfig {
	gc := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: serviceApi.GatewayConfigSpec{
			IngressGateway: serviceApi.GatewaySpec{
				Certificate: infrav1.CertificateSpec{Type: certType},
			},
		},
	}
	if len(secretName) > 0 && secretName[0] != "" {
		gc.Spec.IngressGateway.Certificate.SecretName = secretName[0]
	}
	return gc
}

// gatewayConfigNoCert creates a test GatewayConfig without certificate spec.
func gatewayConfigNoCert(name string) *serviceApi.GatewayConfig {
	return &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       serviceApi.GatewayConfigSpec{},
	}
}

// createTestGatewayConfig creates a test GatewayConfig for testing.
func createTestGatewayConfig(name, domain string, certType infrav1.CertType) *serviceApi.GatewayConfig {
	return &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: serviceApi.GatewayConfigSpec{
			IngressGateway: serviceApi.GatewaySpec{
				Domain:      domain,
				Certificate: infrav1.CertificateSpec{Type: certType},
			},
		},
	}
}

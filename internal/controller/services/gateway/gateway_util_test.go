//nolint:testpackage
package gateway

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"

	. "github.com/onsi/gomega"
)

// createTestScheme creates a reusable scheme for test clients.
func createTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(serviceApi.AddToScheme(scheme))
	return scheme
}

// setupTestClient creates a fake client with the required scheme for Gateway API tests.
func setupTestClient() client.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(gwapiv1.Install(scheme))
	utilruntime.Must(serviceApi.AddToScheme(scheme))
	utilruntime.Must(dsciv1.AddToScheme(scheme))
	utilruntime.Must(dsciv2.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).Build()
}

// setupTestClientWithObjects creates a fake client with pre-existing objects.
func setupTestClientWithObjects(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(gwapiv1.Install(scheme))
	utilruntime.Must(serviceApi.AddToScheme(scheme))
	utilruntime.Must(dsciv1.AddToScheme(scheme))
	utilruntime.Must(dsciv2.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

// convertToGatewayClass converts unstructured resource to typed GatewayClass.
func convertToGatewayClass(g *WithT, resource *unstructured.Unstructured) *gwapiv1.GatewayClass {
	gatewayClass := &gwapiv1.GatewayClass{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, gatewayClass)
	g.Expect(err).NotTo(HaveOccurred())
	return gatewayClass
}

// convertToGateway converts unstructured resource to typed Gateway.
func convertToGateway(g *WithT, resource *unstructured.Unstructured) *gwapiv1.Gateway {
	gateway := &gwapiv1.Gateway{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, gateway)
	g.Expect(err).NotTo(HaveOccurred())
	return gateway
}

// setupSupportTestClient creates a fake client with required schemes for support function tests.
func setupSupportTestClient() client.Client {
	return fake.NewClientBuilder().WithScheme(createTestScheme()).Build()
}

// setupSupportTestClientWithClusterIngress creates a fake client with a mock cluster ingress object.
func setupSupportTestClientWithClusterIngress(domain string) client.Client {
	clusterIngress := &unstructured.Unstructured{}
	clusterIngress.SetGroupVersionKind(gvk.OpenshiftIngress)
	clusterIngress.SetName("cluster")

	// Set the spec.domain field
	_ = unstructured.SetNestedField(clusterIngress.Object, domain, "spec", "domain")

	return fake.NewClientBuilder().WithScheme(createTestScheme()).WithObjects(clusterIngress).Build()
}

// gatewayConfigWithCert creates a test GatewayConfig with certificate spec.
func gatewayConfigWithCert(name string, certType infrav1.CertType, secretName ...string) *serviceApi.GatewayConfig {
	gc := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: serviceApi.GatewayConfigSpec{
			Certificate: &infrav1.CertificateSpec{Type: certType},
		},
	}
	if len(secretName) > 0 && secretName[0] != "" {
		gc.Spec.Certificate.SecretName = secretName[0]
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
			Domain: domain,
			Certificate: &infrav1.CertificateSpec{
				Type: certType,
			},
		},
	}
}

// createTestGatewayConfigSupport creates a GatewayConfig for support function testing.
func createTestGatewayConfigSupport(domain string, certSpec *infrav1.CertificateSpec) *serviceApi.GatewayConfig {
	return createTestGatewayConfigSupportWithSubdomain(domain, "", certSpec)
}

// createTestGatewayConfigSupportWithSubdomain creates a GatewayConfig with subdomain for support function testing.
func createTestGatewayConfigSupportWithSubdomain(domain, subdomain string, certSpec *infrav1.CertificateSpec) *serviceApi.GatewayConfig {
	return &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName,
		},
		Spec: serviceApi.GatewayConfigSpec{
			Domain:      domain,
			Subdomain:   subdomain,
			Certificate: certSpec,
		},
	}
}

//go:build integration

package gateway_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"

	. "github.com/onsi/gomega"
)

// TestKubernetesCertManagerCertificateFallback verifies that an empty certificate
// object requests a cert-manager-issued certificate instead of the OpenShift ingress certificate.
func TestKubernetesCertManagerCertificateFallback(t *testing.T) {
	tc := OAuthTestEnv
	g := NewWithT(t)

	previousClusterInfo := cluster.GetClusterInfo()
	kubernetesClusterInfo := previousClusterInfo
	kubernetesClusterInfo.Type = cluster.ClusterTypeKubernetes
	cluster.SetClusterInfo(kubernetesClusterInfo)
	t.Cleanup(func() { cluster.SetClusterInfo(previousClusterInfo) })
	t.Cleanup(func() {
		// envtest has no garbage collector; do not leave the Kubernetes controller
		// name on the shared GatewayClass for subsequent OpenShift tests.
		gatewayClass := &gwapiv1.GatewayClass{ObjectMeta: metav1.ObjectMeta{Name: gateway.GatewayClassName}}
		if err := tc.K8sClient.Delete(tc.Ctx, gatewayClass); !k8serr.IsNotFound(err) {
			g.Expect(err).NotTo(HaveOccurred())
		}
		g.Eventually(func() bool {
			return k8serr.IsNotFound(tc.K8sClient.Get(tc.Ctx, types.NamespacedName{Name: gateway.GatewayClassName}, gatewayClass))
		}, TestTimeout, TestInterval).Should(BeTrue(), "Kubernetes GatewayClass should be deleted")
	})

	// The shared OAuth environment only creates the OpenShift gateway namespace.
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: gateway.GetGatewayNamespace()}}
	if err := tc.K8sClient.Create(tc.Ctx, namespace); !k8serr.IsAlreadyExists(err) {
		g.Expect(err).NotTo(HaveOccurred())
	}

	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, serviceApi.GatewayConfigSpec{
		IngressMode: serviceApi.IngressModeLoadBalancer,
		Domain:      "apps.kubernetes.example.com",
		Certificate: &infrav1.CertificateSpec{},
	})
	defer DeleteGatewayConfig(t, tc.Ctx, tc.K8sClient)

	certificate := &unstructured.Unstructured{}
	certificate.SetGroupVersionKind(gvk.CertManagerCertificate)
	g.Eventually(func() error {
		return tc.K8sClient.Get(tc.Ctx, types.NamespacedName{
			Name:      "default-gateway-tls",
			Namespace: gateway.GetGatewayNamespace(),
		}, certificate)
	}, TestTimeout, TestInterval).Should(Succeed(), "Kubernetes fallback should create a cert-manager Certificate")

	secretName, found, err := unstructured.NestedString(certificate.Object, "spec", "secretName")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(secretName).To(Equal("default-gateway-tls"))
	dnsNames, found, err := unstructured.NestedStringSlice(certificate.Object, "spec", "dnsNames")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(dnsNames).To(Equal([]string{"rh-ai.apps.kubernetes.example.com"}))

	created := &serviceApi.GatewayConfig{}
	g.Expect(tc.K8sClient.Get(tc.Ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, created)).To(Succeed())
	g.Expect(created.Spec.Certificate).NotTo(BeNil())
	g.Expect(created.Spec.Certificate.Type).To(BeEmpty(), "the API server must not default the certificate type")
}

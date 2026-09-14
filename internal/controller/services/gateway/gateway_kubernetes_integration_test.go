//go:build integration

package gateway_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

// TestKubernetesSelfSignedCertificateFallback verifies that an empty certificate
// object uses the Kubernetes fallback instead of the OpenShift ingress certificate.
func TestKubernetesSelfSignedCertificateFallback(t *testing.T) {
	tc := OAuthTestEnv
	g := NewWithT(t)

	previousClusterInfo := cluster.GetClusterInfo()
	kubernetesClusterInfo := previousClusterInfo
	kubernetesClusterInfo.Type = cluster.ClusterTypeKubernetes
	cluster.SetClusterInfo(kubernetesClusterInfo)
	t.Cleanup(func() { cluster.SetClusterInfo(previousClusterInfo) })

	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, serviceApi.GatewayConfigSpec{
		IngressMode: serviceApi.IngressModeLoadBalancer,
		Domain:      "apps.kubernetes.example.com",
		Certificate: &infrav1.CertificateSpec{},
	})
	defer DeleteGatewayConfig(t, tc.Ctx, tc.K8sClient)

	g.Eventually(func() bool {
		certificate := &corev1.Secret{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{
			Name:      "default-gateway-tls",
			Namespace: gateway.GatewayNamespace,
		}, certificate); err != nil {
			return false
		}
		return certificate.Type == corev1.SecretTypeTLS &&
			len(certificate.Data[corev1.TLSCertKey]) > 0 &&
			len(certificate.Data[corev1.TLSPrivateKeyKey]) > 0
	}, TestTimeout, TestInterval).Should(BeTrue(), "Kubernetes fallback should create a self-signed TLS secret")

	created := &serviceApi.GatewayConfig{}
	g.Expect(tc.K8sClient.Get(tc.Ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, created)).To(Succeed())
	g.Expect(created.Spec.Certificate).NotTo(BeNil())
	g.Expect(created.Spec.Certificate.Type).To(BeEmpty(), "the API server must not default the certificate type")
}

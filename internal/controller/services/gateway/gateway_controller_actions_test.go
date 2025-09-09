//nolint:testpackage
package gateway

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

func TestCreateListeners(t *testing.T) {
	g := NewWithT(t)

	t.Run("creates HTTPS listener when certificate provided", func(t *testing.T) {
		listeners := createListeners("test-cert-secret", "apps.test-cluster.com")

		g.Expect(listeners).To(HaveLen(1))
		g.Expect(string(listeners[0].Name)).To(Equal("https"))
		g.Expect(listeners[0].Protocol).To(Equal(gwapiv1.HTTPSProtocolType))
		g.Expect(listeners[0].Port).To(Equal(gwapiv1.PortNumber(443)))
		g.Expect(listeners[0].Hostname).NotTo(BeNil())
		g.Expect(string(*listeners[0].Hostname)).To(Equal("apps.test-cluster.com"))
		g.Expect(listeners[0].TLS).NotTo(BeNil())
		g.Expect(listeners[0].TLS.CertificateRefs).To(HaveLen(1))
		g.Expect(string(listeners[0].TLS.CertificateRefs[0].Name)).To(Equal("test-cert-secret"))
	})

	t.Run("creates no listeners when no certificate", func(t *testing.T) {
		listeners := createListeners("", "apps.test-cluster.com")

		g.Expect(listeners).To(BeEmpty())
	})
}

func TestCreateGatewayClass(t *testing.T) {
	g := NewWithT(t)

	scheme := runtime.NewScheme()
	utilruntime.Must(gwapiv1.Install(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	rr := &odhtypes.ReconciliationRequest{
		Client: client,
	}

	err := createGatewayClass(rr)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rr.Resources).To(HaveLen(1))
}

func TestGetCertificateType(t *testing.T) {
	g := NewWithT(t)

	t.Run("returns default when certificate is nil", func(t *testing.T) {
		gateway := &serviceApi.GatewayConfig{
			Spec: serviceApi.GatewayConfigSpec{
				Certificate: nil,
			},
		}

		certType := getCertificateType(gateway)
		g.Expect(certType).To(Equal(string(infrav1.OpenshiftDefaultIngress)))
	})

	t.Run("returns certificate type when specified", func(t *testing.T) {
		gateway := &serviceApi.GatewayConfig{
			Spec: serviceApi.GatewayConfigSpec{
				Certificate: &infrav1.CertificateSpec{
					Type: infrav1.SelfSigned,
				},
			},
		}

		certType := getCertificateType(gateway)
		g.Expect(certType).To(Equal(string(infrav1.SelfSigned)))
	})
}

//nolint:testpackage
package gateway

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

func TestCreateListeners(t *testing.T) {
	g := NewWithT(t)

	t.Run("creates HTTPS listener when certificate provided", func(t *testing.T) {
		listeners := createListeners("test-cert-secret")

		g.Expect(listeners).To(HaveLen(1))
		g.Expect(string(listeners[0].Name)).To(Equal("https"))
		g.Expect(listeners[0].Protocol).To(Equal(gwapiv1.HTTPSProtocolType))
		g.Expect(listeners[0].Port).To(Equal(gwapiv1.PortNumber(443)))
		g.Expect(listeners[0].TLS).NotTo(BeNil())
		g.Expect(listeners[0].TLS.CertificateRefs).To(HaveLen(1))
		g.Expect(string(listeners[0].TLS.CertificateRefs[0].Name)).To(Equal("test-cert-secret"))
	})

	t.Run("creates no listeners when no certificate", func(t *testing.T) {
		listeners := createListeners("")

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

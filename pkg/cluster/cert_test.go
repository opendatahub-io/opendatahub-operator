package cluster_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

type ingressLookupClient struct {
	client.Client

	ingressLookups int
}

func (c *ingressLookupClient) Get(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
	if ingress, ok := obj.(*operatorv1.IngressController); ok {
		c.ingressLookups++
		ingress.Name = "default"
		return nil
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func TestIsGatewayCertificateSecretEmptyTypeUsesPlatformDefault(t *testing.T) {
	for _, tc := range []struct {
		name        string
		clusterType string
		wantMatch   bool
		wantLookups int
	}{
		{name: "Kubernetes", clusterType: cluster.ClusterTypeKubernetes},
		{name: "OpenShift", clusterType: cluster.ClusterTypeOpenShift, wantMatch: true, wantLookups: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			previousClusterInfo := cluster.GetClusterInfo()
			cluster.SetClusterInfo(cluster.ClusterInfo{Type: tc.clusterType})
			t.Cleanup(func() { cluster.SetClusterInfo(previousClusterInfo) })

			scheme := runtime.NewScheme()
			g.Expect(serviceApi.AddToScheme(scheme)).To(Succeed())
			gatewayConfig := &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
				Spec: serviceApi.GatewayConfigSpec{
					Certificate: &infrav1.CertificateSpec{},
				},
			}
			cli := &ingressLookupClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(gatewayConfig).Build()}
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "router-certs-default", Namespace: "gateway-namespace"}}

			matched := cluster.IsGatewayCertificateSecret(t.Context(), cli, secret, secret.Namespace)
			g.Expect(matched).To(Equal(tc.wantMatch))
			g.Expect(cli.ingressLookups).To(Equal(tc.wantLookups))
		})
	}
}

func generateTestCertPEM(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestValidateCustomCABundle(t *testing.T) {
	t.Parallel()

	validCert := generateTestCertPEM(t)
	validChain := validCert + validCert

	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "Empty string is valid",
			input:   "",
			wantErr: false,
		},
		{
			name:    "Valid single certificate",
			input:   validCert,
			wantErr: false,
		},
		{
			name:    "Valid certificate chain",
			input:   validChain,
			wantErr: false,
		},
		{
			name:    "Garbage data",
			input:   "this is not a PEM",
			wantErr: true,
		},
		{
			name:    "Valid PEM block but not a certificate",
			input:   string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("fake")})),
			wantErr: true,
		},
		{
			name:    "Certificate PEM header with invalid DER",
			input:   string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not-a-cert")})),
			wantErr: true,
		},
		{
			name:    "Valid cert followed by garbage",
			input:   validCert + "some trailing garbage",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			err := cluster.ValidateCustomCABundle(tc.input)
			if tc.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}
		})
	}
}

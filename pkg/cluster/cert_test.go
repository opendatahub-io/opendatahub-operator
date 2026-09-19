package cluster_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

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

// generateTestSelfSignedCert creates a self-signed TLS certificate and private key PEM
// for the given domain with the specified validity duration.
func generateTestSelfSignedCert(t *testing.T, domain string, validity time.Duration) ([]byte, []byte) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   domain,
			Organization: []string{"opendatahub-self-signed"},
		},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	if ip := net.ParseIP(domain); ip != nil {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
	} else {
		if strings.HasPrefix(domain, "*.") {
			tmpl.DNSNames = append(tmpl.DNSNames, domain[2:])
		}
		tmpl.DNSNames = append(tmpl.DNSNames, domain)
	}
	tmpl.DNSNames = append(tmpl.DNSNames, "localhost")

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	return certBytes, keyBytes
}

func generateTestFutureDatedCert(t *testing.T, domain string, notBefore, notAfter time.Time) ([]byte, []byte) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   domain,
			Organization: []string{"opendatahub-self-signed"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	if ip := net.ParseIP(domain); ip != nil {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
	} else {
		if strings.HasPrefix(domain, "*.") {
			tmpl.DNSNames = append(tmpl.DNSNames, domain[2:])
		}
		tmpl.DNSNames = append(tmpl.DNSNames, domain)
	}
	tmpl.DNSNames = append(tmpl.DNSNames, "localhost")

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	return certBytes, keyBytes
}

func createTLSSecret(certPEM, keyPEM []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cert",
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       certPEM,
			corev1.TLSPrivateKeyKey: keyPEM,
		},
		Type: corev1.SecretTypeTLS,
	}
}

func TestCreateSelfSignedCertificate_PreservesExistingValidCert(t *testing.T) {
	t.Parallel()

	const (
		secretName = "test-cert"
		namespace  = "test-ns"
		domain     = "test.example.com"
	)

	t.Run("creates certificate when secret does not exist", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		err = cluster.CreateSelfSignedCertificate(context.Background(), cli, secretName, domain, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify the secret was created
		secret := &corev1.Secret{}
		err = cli.Get(context.Background(), client.ObjectKey{Name: secretName, Namespace: namespace}, secret)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(secret.Data).To(HaveKey(corev1.TLSCertKey))
		g.Expect(secret.Data).To(HaveKey(corev1.TLSPrivateKeyKey))
	})

	t.Run("preserves existing valid certificate", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		// Create a certificate valid for 365 days (well beyond the 30-day renewal threshold)
		certPEM, keyPEM := generateTestSelfSignedCert(t, domain, 365*24*time.Hour)
		existingSecret := createTLSSecret(certPEM, keyPEM)

		cli, err := fakeclient.New(fakeclient.WithObjects(existingSecret))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = cluster.CreateSelfSignedCertificate(context.Background(), cli, secretName, domain, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify the certificate was NOT regenerated — data should be identical
		secret := &corev1.Secret{}
		err = cli.Get(context.Background(), client.ObjectKey{Name: secretName, Namespace: namespace}, secret)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(secret.Data[corev1.TLSCertKey]).To(Equal(certPEM))
		g.Expect(secret.Data[corev1.TLSPrivateKeyKey]).To(Equal(keyPEM))
	})

	t.Run("regenerates certificate when domain changes", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		// Create a certificate for a different domain
		certPEM, keyPEM := generateTestSelfSignedCert(t, "old.example.com", 365*24*time.Hour)
		existingSecret := createTLSSecret(certPEM, keyPEM)

		cli, err := fakeclient.New(fakeclient.WithObjects(existingSecret))
		g.Expect(err).ShouldNot(HaveOccurred())

		// Request certificate for a new domain
		err = cluster.CreateSelfSignedCertificate(context.Background(), cli, secretName, domain, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify the certificate was regenerated — cert data should differ
		secret := &corev1.Secret{}
		err = cli.Get(context.Background(), client.ObjectKey{Name: secretName, Namespace: namespace}, secret)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(secret.Data[corev1.TLSCertKey]).ToNot(Equal(certPEM))
	})

	t.Run("regenerates certificate approaching expiration", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		// Create a certificate that expires in 15 days (within the 30-day renewal window)
		certPEM, keyPEM := generateTestSelfSignedCert(t, domain, 15*24*time.Hour)
		existingSecret := createTLSSecret(certPEM, keyPEM)

		cli, err := fakeclient.New(fakeclient.WithObjects(existingSecret))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = cluster.CreateSelfSignedCertificate(context.Background(), cli, secretName, domain, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify the certificate was regenerated
		secret := &corev1.Secret{}
		err = cli.Get(context.Background(), client.ObjectKey{Name: secretName, Namespace: namespace}, secret)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(secret.Data[corev1.TLSCertKey]).ToNot(Equal(certPEM))
	})

	t.Run("regenerates certificate with future NotBefore", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		now := time.Now()
		certPEM, keyPEM := generateTestFutureDatedCert(t, domain, now.Add(24*time.Hour), now.Add(365*24*time.Hour))
		existingSecret := createTLSSecret(certPEM, keyPEM)

		cli, err := fakeclient.New(fakeclient.WithObjects(existingSecret))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = cluster.CreateSelfSignedCertificate(context.Background(), cli, secretName, domain, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		secret := &corev1.Secret{}
		err = cli.Get(context.Background(), client.ObjectKey{Name: secretName, Namespace: namespace}, secret)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(secret.Data[corev1.TLSCertKey]).ToNot(Equal(certPEM))
	})

	t.Run("regenerates certificate with corrupt data", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		existingSecret := createTLSSecret([]byte("not-a-cert"), []byte("not-a-key"))

		cli, err := fakeclient.New(fakeclient.WithObjects(existingSecret))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = cluster.CreateSelfSignedCertificate(context.Background(), cli, secretName, domain, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify the certificate was regenerated with valid data
		secret := &corev1.Secret{}
		err = cli.Get(context.Background(), client.ObjectKey{Name: secretName, Namespace: namespace}, secret)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(secret.Data[corev1.TLSCertKey]).ToNot(Equal([]byte("not-a-cert")))
	})

	t.Run("regenerates certificate with empty tls.crt", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		existingSecret := createTLSSecret([]byte{}, []byte("some-key"))

		cli, err := fakeclient.New(fakeclient.WithObjects(existingSecret))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = cluster.CreateSelfSignedCertificate(context.Background(), cli, secretName, domain, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify a new cert was generated
		secret := &corev1.Secret{}
		err = cli.Get(context.Background(), client.ObjectKey{Name: secretName, Namespace: namespace}, secret)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(secret.Data[corev1.TLSCertKey]).ToNot(BeEmpty())
	})

	t.Run("preserves wildcard certificate for matching subdomain", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		wildcardDomain := "*.example.com"
		certPEM, keyPEM := generateTestSelfSignedCert(t, wildcardDomain, 365*24*time.Hour)
		existingSecret := createTLSSecret(certPEM, keyPEM)

		cli, err := fakeclient.New(fakeclient.WithObjects(existingSecret))
		g.Expect(err).ShouldNot(HaveOccurred())

		// A wildcard cert for *.example.com should be valid for sub.example.com
		err = cluster.CreateSelfSignedCertificate(context.Background(), cli, secretName, "sub.example.com", namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		secret := &corev1.Secret{}
		err = cli.Get(context.Background(), client.ObjectKey{Name: secretName, Namespace: namespace}, secret)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(secret.Data[corev1.TLSCertKey]).To(Equal(certPEM))
	})

	t.Run("regenerates certificate with valid cert but corrupt private key", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		certPEM, _ := generateTestSelfSignedCert(t, domain, 365*24*time.Hour)
		existingSecret := createTLSSecret(certPEM, []byte("not-a-key"))

		cli, err := fakeclient.New(fakeclient.WithObjects(existingSecret))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = cluster.CreateSelfSignedCertificate(context.Background(), cli, secretName, domain, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		secret := &corev1.Secret{}
		err = cli.Get(context.Background(), client.ObjectKey{Name: secretName, Namespace: namespace}, secret)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(secret.Data[corev1.TLSCertKey]).ToNot(Equal(certPEM))
	})

	t.Run("regenerates certificate with missing private key", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		certPEM, _ := generateTestSelfSignedCert(t, domain, 365*24*time.Hour)
		existingSecret := createTLSSecret(certPEM, nil)
		delete(existingSecret.Data, corev1.TLSPrivateKeyKey)

		cli, err := fakeclient.New(fakeclient.WithObjects(existingSecret))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = cluster.CreateSelfSignedCertificate(context.Background(), cli, secretName, domain, namespace)
		g.Expect(err).ShouldNot(HaveOccurred())

		secret := &corev1.Secret{}
		err = cli.Get(context.Background(), client.ObjectKey{Name: secretName, Namespace: namespace}, secret)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(secret.Data[corev1.TLSCertKey]).ToNot(BeEmpty())
		g.Expect(secret.Data[corev1.TLSPrivateKeyKey]).ToNot(BeEmpty())
	})

	t.Run("refreshes metadata on preserved valid certificate", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		certPEM, keyPEM := generateTestSelfSignedCert(t, domain, 365*24*time.Hour)
		existingSecret := createTLSSecret(certPEM, keyPEM)

		cli, err := fakeclient.New(fakeclient.WithObjects(existingSecret))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = cluster.CreateSelfSignedCertificate(context.Background(), cli, secretName, domain, namespace,
			cluster.WithLabels("app.kubernetes.io/managed-by", "odh-operator"),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		secret := &corev1.Secret{}
		err = cli.Get(context.Background(), client.ObjectKey{Name: secretName, Namespace: namespace}, secret)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(secret.Data[corev1.TLSCertKey]).To(Equal(certPEM))
		g.Expect(secret.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "odh-operator"))
	})
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

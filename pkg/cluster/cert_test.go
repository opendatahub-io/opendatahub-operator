package cluster_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

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

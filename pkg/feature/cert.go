package feature

import (
	"bytes"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"math/big"
	"math/rand"
	"net"
	"strings"
	"time"
)

var seededRand = rand.New(rand.NewSource(time.Now().UnixNano()))

func GenerateSelfSignedCertificateAsSecret(addr string, objectMeta metav1.ObjectMeta) (*corev1.Secret, error) {
	cert, key, err := generateCertificate(addr)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &corev1.Secret{
		ObjectMeta: objectMeta,
		Data: map[string][]byte{
			corev1.TLSCertKey:       cert,
			corev1.TLSPrivateKeyKey: key,
		},
	}, nil
}

func generateCertificate(addr string) ([]byte, []byte, error) {
	key, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}

	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber: new(big.Int).SetInt64(seededRand.Int63()),
		Subject: pkix.Name{
			CommonName:   addr,
			Organization: []string{"opendatahub-self-signed"},
		},
		NotBefore:             now.UTC(),
		NotAfter:              now.Add(time.Second * 60 * 60 * 24 * 365).UTC(),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	if ip := net.ParseIP(addr); ip != nil {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
	} else {
		if strings.HasPrefix(addr, "*.") {
			tmpl.DNSNames = append(tmpl.DNSNames, addr[2:])
		}
		tmpl.DNSNames = append(tmpl.DNSNames, addr)
	}

	tmpl.DNSNames = append(tmpl.DNSNames, "localhost")

	certDERBytes, err := x509.CreateCertificate(cryptorand.Reader, &tmpl, &tmpl, key.Public(), key)
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}
	certificate, err := x509.ParseCertificate(certDERBytes)
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}

	certBuffer := bytes.Buffer{}
	if err := pem.Encode(&certBuffer, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certificate.Raw,
	}); err != nil {
		return nil, nil, errors.WithStack(err)
	}

	keyBuffer := bytes.Buffer{}
	if err := pem.Encode(&keyBuffer, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}); err != nil {
		return nil, nil, errors.WithStack(err)
	}

	return certBuffer.Bytes(), keyBuffer.Bytes(), nil
}

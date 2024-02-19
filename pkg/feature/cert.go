package feature

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/infrastructure/v1"
)

func (f *Feature) CreateSelfSignedCertificate(secretName string, certificateType infrav1.CertType, domain, namespace string) error {
	if certificateType != infrav1.SelfSigned {
		return nil
	}

	meta := metav1.ObjectMeta{
		Name:      secretName,
		Namespace: namespace,
		OwnerReferences: []metav1.OwnerReference{
			f.AsOwnerReference(),
		},
	}

	certSecret, err := GenerateSelfSignedCertificateAsSecret(domain, meta)
	if err != nil {
		return fmt.Errorf("failed generating self-signed certificate: %w", err)
	}

	if createErr := f.Client.Create(context.TODO(), certSecret); client.IgnoreAlreadyExists(createErr) != nil {
		return fmt.Errorf("failed creating certificate secret: %w", createErr)
	}

	return nil
}

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
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}

	seededRand, cryptErr := rand.Int(rand.Reader, big.NewInt(time.Now().UnixNano()))
	if cryptErr != nil {
		return nil, nil, errors.WithStack(cryptErr)
	}

	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber: seededRand,
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

	certDERBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, key.Public(), key)
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

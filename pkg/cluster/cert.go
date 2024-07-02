package cluster

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

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateSelfSignedCertificate(ctx context.Context, c client.Client, secretName, domain, namespace string, metaOptions ...MetaOptions) error {
	certSecret, err := GenerateSelfSignedCertificateAsSecret(secretName, domain, namespace)
	if err != nil {
		return fmt.Errorf("failed generating self-signed certificate: %w", err)
	}

	if err := ApplyMetaOptions(certSecret, metaOptions...); err != nil {
		return err
	}
	if err = generateCertSecret(ctx, c, certSecret, secretName, namespace); err != nil {
		return fmt.Errorf("failed update self-signed certificate secret: %w", err)
	}
	return nil
}

func GenerateSelfSignedCertificateAsSecret(name, addr, namespace string) (*corev1.Secret, error) {
	cert, key, err := generateCertificate(addr)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       cert,
			corev1.TLSPrivateKeyKey: key,
		},
		Type: corev1.SecretTypeTLS,
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

// PropagateDefaultIngressCertificate copies ingress cert secrets from openshift-ingress ns to given namespace.
func PropagateDefaultIngressCertificate(ctx context.Context, c client.Client, secretName, namespace string) error {
	// Add IngressController to scheme
	runtime.Must(operatorv1.Install(c.Scheme()))
	defaultIngressCtrl, err := FindAvailableIngressController(ctx, c)
	if err != nil {
		return fmt.Errorf("failed to get ingress controller: %w", err)
	}

	defaultIngressCertName := GetDefaultIngressCertSecretName(defaultIngressCtrl)

	defaultIngressSecret, err := GetSecret(ctx, c, "openshift-ingress", defaultIngressCertName)
	if err != nil {
		return err
	}

	return copySecretToNamespace(ctx, c, defaultIngressSecret, secretName, namespace)
}

func FindAvailableIngressController(ctx context.Context, c client.Client) (*operatorv1.IngressController, error) {
	defaultIngressCtrl := &operatorv1.IngressController{}

	err := c.Get(ctx, client.ObjectKey{Namespace: "openshift-ingress-operator", Name: "default"}, defaultIngressCtrl)
	if err != nil {
		return nil, fmt.Errorf("error getting ingresscontroller resource :%w", err)
	}
	return defaultIngressCtrl, nil
}

func GetDefaultIngressCertSecretName(ingressCtrl *operatorv1.IngressController) string {
	if ingressCtrl.Spec.DefaultCertificate != nil {
		return ingressCtrl.Spec.DefaultCertificate.Name
	}
	return "router-certs-" + ingressCtrl.Name
}

func GetSecret(ctx context.Context, c client.Client, namespace, name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, secret)
	if err != nil {
		return nil, err
	}
	return secret, nil
}

func copySecretToNamespace(ctx context.Context, c client.Client, secret *corev1.Secret, newSecretName, namespace string) error {
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      newSecretName,
			Namespace: namespace,
		},
		Data: secret.Data,
		Type: secret.Type,
	}
	if err := generateCertSecret(ctx, c, newSecret, newSecretName, namespace); err != nil {
		return fmt.Errorf("failed to deploy default cert secret to namespace %s: %w", namespace, err)
	}
	return nil
}

// recreateSecret deletes the existing secret and creates a new one.
func recreateSecret(ctx context.Context, c client.Client, existingSecret, newSecret *corev1.Secret) error {
	if err := c.Delete(ctx, existingSecret); err != nil {
		return fmt.Errorf("failed to delete existing secret before recreating new one: %w", err)
	}
	if err := c.Create(ctx, newSecret); err != nil {
		return fmt.Errorf("failed to create new secret after existing one has been deleted: %w", err)
	}
	return nil
}

// generateCertSecret creates a secret if it does not exist;  recreate this secret if type not match; update data if outdated.
func generateCertSecret(ctx context.Context, c client.Client, certSecret *corev1.Secret, secretName, namespace string) error {
	existingSecret := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{Name: secretName, Namespace: namespace}, existingSecret)
	switch {
	case err == nil:
		// Secret exists but with a different type, delete and create it again
		if existingSecret.Type != certSecret.Type {
			return recreateSecret(ctx, c, existingSecret, certSecret)
		}
		// update data if found with same type but outdated content
		if isSecretOutdated(existingSecret.Data, certSecret.Data) {
			if err = c.Update(ctx, certSecret); err != nil {
				return fmt.Errorf("failed to update existing secret: %w", err)
			}
		}
	case k8serr.IsNotFound(err):
		// Secret does not exist, create it
		if err := c.Create(ctx, certSecret); err != nil {
			return fmt.Errorf("failed creating new certificate secret: %w", err)
		}
	default:
		return fmt.Errorf("failed getting certificate secret: %w", err)
	}

	return nil
}

// isSecretOutdated compares two secret data of type map[string][]byte and returns true if they are not equal.
func isSecretOutdated(existingSecretData, newSecretData map[string][]byte) bool {
	if len(existingSecretData) != len(newSecretData) {
		return true
	}

	for key, value1 := range existingSecretData {
		value2, ok := newSecretData[key]
		if !ok {
			return true
		}
		if !bytes.Equal(value1, value2) {
			return true
		}
	}

	return false
}

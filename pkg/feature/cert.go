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

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (f *Feature) CreateSelfSignedCertificate(secretName string, domain, namespace string) error {
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

// GetDefaultIngressCertificate copies ingress cert secrets from openshift-ingress ns to given namespace.
func (f *Feature) GetDefaultIngressCertificate(namespace string) error {
	defaultIngressCtrl, err := FindAvailableIngressController(f.Client)
	if err != nil {
		return fmt.Errorf("failed to get ingress controller: %w", err)
	}

	defaultIngressCertName := GetDefaultIngressCertSecretName(defaultIngressCtrl)

	defaultIngressSecret, err := f.getSecret("openshift-ingress", defaultIngressCertName)
	if err != nil {
		return err
	}

	err = f.copySecretToNamespace(defaultIngressSecret, namespace)
	if err != nil {
		return err
	}

	return nil
}

func FindAvailableIngressController(cli client.Client) (*operatorv1.IngressController, error) {
	defaultIngressCtrlList := &operatorv1.IngressControllerList{}
	listOpts := []client.ListOption{
		client.InNamespace("openshift-ingress-operator"),
	}

	err := cli.List(context.TODO(), defaultIngressCtrlList, listOpts...)
	if err != nil {
		return nil, err
	}

	for _, ingressCtrl := range defaultIngressCtrlList.Items {
		for _, condition := range ingressCtrl.Status.Conditions {
			if condition.Type == operatorv1.IngressControllerAvailableConditionType && condition.Status == operatorv1.ConditionTrue {
				return &ingressCtrl, nil
			}
		}
	}
	return nil, err
}

func GetDefaultIngressCertSecretName(ingressCtrl *operatorv1.IngressController) string {
	if ingressCtrl.Spec.DefaultCertificate != nil {
		return ingressCtrl.Spec.DefaultCertificate.Name
	}
	return "router-certs-" + ingressCtrl.Name
}

func (f *Feature) getSecret(namespace, name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := f.Client.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, secret)
	if err != nil {
		return nil, err
	}
	return secret, nil
}

func (f *Feature) copySecretToNamespace(secret *corev1.Secret, namespace string) error {
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name,
			Namespace: namespace,
		},
		Data: secret.Data,
		Type: secret.Type,
	}

	existingSecret := &corev1.Secret{}
	err := f.Client.Get(context.TODO(), client.ObjectKey{Name: secret.Name, Namespace: namespace}, existingSecret)
	if apierrs.IsNotFound(err) {
		err = f.Client.Create(context.TODO(), newSecret)
		if err != nil {
			return err
		}
	} else if err == nil {
		// Check if secret needs to be updated
		if isSecretOutdated(existingSecret.Data, newSecret.Data) {
			err = f.Client.Update(context.TODO(), newSecret)
			if err != nil {
				return err
			}
		}
		return nil
	}

	return err
}

// isSecretOutdated compares two secret data of type map[string][]byte and returns true if they are not equal equal
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

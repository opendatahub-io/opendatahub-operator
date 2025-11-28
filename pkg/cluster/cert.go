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
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	CertFieldOwner   = resources.PlatformFieldOwner + "/cert"
	IngressNamespace = "openshift-ingress"
)

var IngressControllerName = types.NamespacedName{
	Namespace: "openshift-ingress-operator",
	Name:      "default",
}

func CreateSelfSignedCertificate(ctx context.Context, c client.Client, secretName, domain, namespace string, metaOptions ...MetaOptions) error {
	certSecret, err := GenerateSelfSignedCertificateAsSecret(secretName, domain, namespace)
	if err != nil {
		return fmt.Errorf("failed generating self-signed certificate: %w", err)
	}

	if errApply := ApplyMetaOptions(certSecret, metaOptions...); errApply != nil {
		return errApply
	}

	opts := []client.PatchOption{
		client.ForceOwnership,
		client.FieldOwner(CertFieldOwner),
	}
	err = resources.Apply(ctx, c, certSecret, opts...)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func GenerateSelfSignedCertificateAsSecret(name, addr, namespace string) (*corev1.Secret, error) {
	cert, key, err := generateCertificate(addr)
	if err != nil {
		return nil, fmt.Errorf("error generating certificate: %w", err)
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.Secret.Kind,
			APIVersion: gvk.Secret.Version,
		},
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
		return nil, nil, fmt.Errorf("error generating key: %w", err)
	}

	seededRand, cryptErr := rand.Int(rand.Reader, big.NewInt(time.Now().UnixNano()))
	if cryptErr != nil {
		return nil, nil, fmt.Errorf("error generating random: %w", cryptErr)
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
		return nil, nil, fmt.Errorf("error creating certificate: %w", err)
	}
	certificate, err := x509.ParseCertificate(certDERBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("error parsing certificate: %w", err)
	}

	certBuffer := bytes.Buffer{}
	if err := pem.Encode(&certBuffer, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certificate.Raw,
	}); err != nil {
		return nil, nil, fmt.Errorf("error encoding pem: %w", err)
	}

	keyBuffer := bytes.Buffer{}
	if err := pem.Encode(&keyBuffer, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}); err != nil {
		return nil, nil, fmt.Errorf("error encoding pem: %w", err)
	}

	return certBuffer.Bytes(), keyBuffer.Bytes(), nil
}

func FindDefaultIngressSecret(ctx context.Context, c client.Client) (*corev1.Secret, error) {
	defaultIngressCtrl, err := FindAvailableIngressController(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("failed to get ingress controller: %w", err)
	}

	defaultIngressCertName := GetDefaultIngressCertSecretName(defaultIngressCtrl)

	defaultIngressSecret, err := GetSecret(ctx, c, IngressNamespace, defaultIngressCertName)
	if err != nil {
		return nil, err
	}

	return defaultIngressSecret, nil
}

// PropagateDefaultIngressCertificate copies ingress cert secrets from openshift-ingress ns to given namespace.
func PropagateDefaultIngressCertificate(ctx context.Context, c client.Client, secretName, namespace string, metaOptions ...MetaOptions) error {
	defaultIngressSecret, err := FindDefaultIngressSecret(ctx, c)
	if err != nil {
		return err
	}

	return copySecretToNamespace(ctx, c, defaultIngressSecret, secretName, namespace, metaOptions...)
}

func FindAvailableIngressController(ctx context.Context, c client.Client) (*operatorv1.IngressController, error) {
	defaultIngressCtrl := &operatorv1.IngressController{}

	err := c.Get(ctx, IngressControllerName, defaultIngressCtrl)
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

// IsGatewayCertificateSecret returns true if obj is a certificate secret used by the GatewayConfig.
// It checks for both OpenShift default ingress certificates and provided certificates.
func IsGatewayCertificateSecret(ctx context.Context, cli client.Client, obj client.Object, gatewayNamespace string) bool {
	if obj.GetNamespace() != gatewayNamespace {
		return false
	}

	gatewayConfig := &serviceApi.GatewayConfig{}
	if err := cli.Get(ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gatewayConfig); err != nil {
		return false
	}

	if gatewayConfig.Spec.Certificate == nil {
		return false
	}

	certConfig := *gatewayConfig.Spec.Certificate
	certType := certConfig.Type

	switch certType {
	case infrav1.OpenshiftDefaultIngress, "":
		ingressCtrl, err := FindAvailableIngressController(ctx, cli)
		if err != nil {
			return false
		}

		ingressCertName := GetDefaultIngressCertSecretName(ingressCtrl)
		return obj.GetName() == ingressCertName

	case infrav1.Provided:
		expectedName := certConfig.SecretName
		if expectedName == "" {
			expectedName = fmt.Sprintf("%s-tls", gatewayConfig.Name)
		}
		return obj.GetName() == expectedName

	default:
		// no need action on selfsigned as operator create it which has the ownerreference on it with reconcile.
		return false
	}
}

func GetSecret(ctx context.Context, c client.Client, namespace, name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, secret)
	if err != nil {
		return nil, err
	}
	return secret, nil
}

func copySecretToNamespace(ctx context.Context, c client.Client, secret *corev1.Secret, newSecretName, namespace string, metaOptions ...MetaOptions) error {
	newSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.Secret.Kind,
			APIVersion: gvk.Secret.Version,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      newSecretName,
			Namespace: namespace,
		},
		Data: secret.Data,
		Type: secret.Type,
	}

	if errApply := ApplyMetaOptions(newSecret, metaOptions...); errApply != nil {
		return errApply
	}

	opts := []client.PatchOption{
		client.ForceOwnership,
		client.FieldOwner(CertFieldOwner),
	}
	err := resources.Apply(ctx, c, newSecret, opts...)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		return err
	}

	return nil
}

package e2e_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
)

const (
	xksDexNamespace     = "dex-system"
	xksDexName          = "dex"
	xksDexPort          = 5556
	xksDexTelemetryPort = 5558
	xksDexImage         = "ghcr.io/dexidp/dex:v2.41.1"
	xksDexTLSName       = "dex-tls"
	xksDexConfigName    = "dex-config"
	// In-cluster issuer URL reachable from kube-auth-proxy pods (Dex serves OIDC discovery here).
	xksGatewayOIDCIssuerURL = "https://dex.dex-system.svc.cluster.local:5556/dex"
)

func xksDexRedirectURI() string {
	return fmt.Sprintf("https://%s.%s%s", gateway.DefaultGatewaySubdomain, xksGatewayDomain, gateway.OAuthCallbackPath)
}

// ensureDexForXKS deploys a minimal Dex OIDC provider for KinD / vanilla Kubernetes e2e.
// kube-auth-proxy uses --skip-oidc-discovery=false, so the issuer must be reachable at startup.
func (tc *TestContext) ensureDexForXKS(t *testing.T) {
	t.Helper()

	if !tc.IsXKS() {
		return
	}

	dexDeploy := &appsv1.Deployment{}
	err := tc.Client().Get(tc.Context(), types.NamespacedName{
		Name:      xksDexName,
		Namespace: xksDexNamespace,
	}, dexDeploy)
	if err == nil {
		t.Logf("Dex deployment already exists in %s, waiting for readiness", xksDexNamespace)
		tc.waitForDexDeploymentReady(t)
		return
	}
	if !k8serr.IsNotFound(err) {
		t.Fatalf("failed to check for existing Dex deployment: %v", err)
	}

	t.Logf("Bootstrapping Dex OIDC provider (issuer=%s)", xksGatewayOIDCIssuerURL)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateNamespaceWithLabels(xksDexNamespace, nil)),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)

	certPEM, keyPEM := generateDexTLSAssets(t)
	dexTLS := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      xksDexTLSName,
			Namespace: xksDexNamespace,
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": keyPEM,
		},
	}
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(dexTLS),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)

	dexConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      xksDexConfigName,
			Namespace: xksDexNamespace,
		},
		Data: map[string]string{
			"config.yaml": fmt.Sprintf(`issuer: %s

storage:
  type: memory

web:
  https: 0.0.0.0:%d
  tlsCert: /etc/dex/tls/tls.crt
  tlsKey: /etc/dex/tls/tls.key

oauth2:
  skipApprovalScreen: true

telemetry:
  http: 0.0.0.0:%d

enablePasswordDB: false

staticClients:
  - id: %s
    name: %s
    secret: %s
    redirectURIs:
      - %q
`, xksGatewayOIDCIssuerURL, xksDexPort, xksDexTelemetryPort, xksGatewayOIDCClientID, xksGatewayOIDCClientID,
				xksGatewayOIDCClientSecret, xksDexRedirectURI()),
		},
	}
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(dexConfig),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)

	dexDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      xksDexName,
			Namespace: xksDexNamespace,
			Labels: map[string]string{
				"app": xksDexName,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: new(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": xksDexName},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": xksDexName},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  xksDexName,
							Image: xksDexImage,
							Args:  []string{"serve", "/etc/dex/config.yaml"},
							Ports: []corev1.ContainerPort{
								{Name: "https", ContainerPort: xksDexPort},
								{Name: "telemetry", ContainerPort: xksDexTelemetryPort},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "config", MountPath: "/etc/dex", ReadOnly: true},
								{Name: "tls", MountPath: "/etc/dex/tls", ReadOnly: true},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/healthz/ready",
										Port:   intstr.FromInt32(xksDexTelemetryPort),
										Scheme: corev1.URISchemeHTTP,
									},
								},
								InitialDelaySeconds: 2,
								PeriodSeconds:       5,
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: xksDexConfigName},
								},
							},
						},
						{
							Name: "tls",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: xksDexTLSName,
								},
							},
						},
					},
				},
			},
		},
	}
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(dexDeployment),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)

	dexService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      xksDexName,
			Namespace: xksDexNamespace,
			Labels: map[string]string{
				"app": xksDexName,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": xksDexName},
			Ports: []corev1.ServicePort{
				{
					Name:       "https",
					Port:       xksDexPort,
					TargetPort: intstr.FromInt32(xksDexPort),
				},
			},
		},
	}
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(dexService),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)

	tc.waitForDexDeploymentReady(t)
	t.Log("Dex OIDC provider bootstrap completed")
}

func (tc *TestContext) waitForDexDeploymentReady(t *testing.T) {
	t.Helper()

	// Poll via EnsureResourceExists; EnsureDeploymentReady is point-in-time and races pod rollout.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Name: xksDexName, Namespace: xksDexNamespace}),
		WithCondition(jq.Match(
			`.status.readyReplicas == 1 and (.status.conditions[] | select(.type == "Available") | .status) == "True"`,
		)),
		WithCustomErrorMsg("Dex deployment %s/%s should be Available with 1 ready replica", xksDexNamespace, xksDexName),
		WithEventuallyTimeout(tc.TestTimeouts.componentReadinessTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)
}

func generateDexTLSAssets(t *testing.T) ([]byte, []byte) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate Dex TLS key: %v", err)
	}

	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "dex.dex-system.svc.cluster.local",
		},
		DNSNames: []string{
			"dex.dex-system.svc.cluster.local",
			"dex.dex-system.svc",
			fmt.Sprintf("dex.%s.svc.cluster.local", xksDexNamespace),
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create Dex TLS certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	return certPEM, keyPEM
}

package e2e_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"sort"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"

	. "github.com/onsi/gomega"
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
							Name:    xksDexName,
							Image:   xksDexImage,
							Command: []string{"/usr/local/bin/dex"},
							Args:    []string{"serve", "/etc/dex/config.yaml"},
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

	defer func() {
		if t.Failed() {
			tc.logDexBootstrapDiagnostics(t)
		}
	}()

	nn := types.NamespacedName{Name: xksDexName, Namespace: xksDexNamespace}
	lastStatusLog := time.Now()

	// Poll deployment readiness; log compact status periodically and full diagnostics on failure.
	tc.g.Eventually(func(g Gomega) {
		deployment := &appsv1.Deployment{}
		g.Expect(tc.Client().Get(tc.Context(), nn, deployment)).To(Succeed(),
			"DEX-DEBUG: failed to get deployment %s/%s", nn.Namespace, nn.Name)

		if time.Since(lastStatusLog) >= 30*time.Second {
			tc.logDexDeploymentStatus(t, deployment)
			for _, cond := range deployment.Status.Conditions {
				t.Logf("DEX-DEBUG: deployment condition type=%s status=%s reason=%s message=%s",
					cond.Type, cond.Status, cond.Reason, cond.Message)
			}
			lastStatusLog = time.Now()
		}

		g.Expect(deployment.Status.ReadyReplicas).To(Equal(int32(1)),
			"DEX-DEBUG: expected 1 ready replica, got %d (available=%d updated=%d unavailable=%d)",
			deployment.Status.ReadyReplicas, deployment.Status.AvailableReplicas,
			deployment.Status.UpdatedReplicas, deployment.Status.UnavailableReplicas)

		available := false
		for _, cond := range deployment.Status.Conditions {
			if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionTrue {
				available = true
				break
			}
		}
		g.Expect(available).To(BeTrue(), "DEX-DEBUG: DeploymentAvailable condition is not True")
	}).
		WithTimeout(tc.TestTimeouts.componentReadinessTimeout).
		WithPolling(tc.TestTimeouts.defaultEventuallyPollInterval).
		Should(Succeed(), "Dex deployment %s/%s should be Available with 1 ready replica", xksDexNamespace, xksDexName)
}

func (tc *TestContext) logDexDeploymentStatus(t *testing.T, deployment *appsv1.Deployment) {
	t.Helper()
	t.Logf("DEX-DEBUG: deployment %s/%s ready=%d available=%d updated=%d unavailable=%d replicas=%d",
		deployment.Namespace, deployment.Name,
		deployment.Status.ReadyReplicas, deployment.Status.AvailableReplicas,
		deployment.Status.UpdatedReplicas, deployment.Status.UnavailableReplicas, deployment.Status.Replicas)
}

// logDexBootstrapDiagnostics dumps Dex namespace state to test logs when bootstrap fails.
func (tc *TestContext) logDexBootstrapDiagnostics(t *testing.T) {
	t.Helper()
	t.Log("DEX-DEBUG: collecting Dex bootstrap diagnostics")

	deployment := &appsv1.Deployment{}
	if err := tc.Client().Get(tc.Context(), types.NamespacedName{Name: xksDexName, Namespace: xksDexNamespace}, deployment); err != nil {
		t.Logf("DEX-DEBUG: failed to get deployment: %v", err)
	} else {
		tc.logDexDeploymentStatus(t, deployment)
		for _, cond := range deployment.Status.Conditions {
			t.Logf("DEX-DEBUG: deployment condition type=%s status=%s reason=%s message=%s",
				cond.Type, cond.Status, cond.Reason, cond.Message)
		}
	}

	rsList := &appsv1.ReplicaSetList{}
	if err := tc.Client().List(tc.Context(), rsList, client.InNamespace(xksDexNamespace), client.MatchingLabels{"app": xksDexName}); err != nil {
		t.Logf("DEX-DEBUG: failed to list ReplicaSets: %v", err)
	} else {
		for _, rs := range rsList.Items {
			t.Logf("DEX-DEBUG: replicaset %s ready=%d available=%d replicas=%d",
				rs.Name, rs.Status.ReadyReplicas, rs.Status.AvailableReplicas, rs.Status.Replicas)
		}
	}

	podList := &corev1.PodList{}
	if err := tc.Client().List(tc.Context(), podList, client.InNamespace(xksDexNamespace), client.MatchingLabels{"app": xksDexName}); err != nil {
		t.Logf("DEX-DEBUG: failed to list pods: %v", err)
	} else if len(podList.Items) == 0 {
		t.Logf("DEX-DEBUG: no pods found with label app=%s in namespace %s", xksDexName, xksDexNamespace)
	} else {
		for _, pod := range podList.Items {
			t.Logf("DEX-DEBUG: pod %s phase=%s node=%s podIP=%s",
				pod.Name, pod.Status.Phase, pod.Spec.NodeName, pod.Status.PodIP)
			for _, cond := range pod.Status.Conditions {
				t.Logf("DEX-DEBUG:   pod condition type=%s status=%s reason=%s message=%s",
					cond.Type, cond.Status, cond.Reason, cond.Message)
			}
			for _, cs := range pod.Status.ContainerStatuses {
				state := "running"
				detail := ""
				switch {
				case cs.State.Waiting != nil:
					state = "waiting"
					detail = fmt.Sprintf("reason=%s message=%s", cs.State.Waiting.Reason, cs.State.Waiting.Message)
				case cs.State.Terminated != nil:
					state = "terminated"
					detail = fmt.Sprintf("reason=%s exitCode=%d message=%s",
						cs.State.Terminated.Reason, cs.State.Terminated.ExitCode, cs.State.Terminated.Message)
				}
				t.Logf("DEX-DEBUG:   container %s ready=%v restarts=%d state=%s %s image=%s",
					cs.Name, cs.Ready, cs.RestartCount, state, detail, cs.Image)
				tc.logDexContainerLogs(t, pod.Namespace, pod.Name, cs.Name, false)
				if cs.RestartCount > 0 {
					tc.logDexContainerLogs(t, pod.Namespace, pod.Name, cs.Name, true)
				}
			}
		}
	}

	eventList := &corev1.EventList{}
	if err := tc.Client().List(tc.Context(), eventList, client.InNamespace(xksDexNamespace)); err != nil {
		t.Logf("DEX-DEBUG: failed to list events: %v", err)
	} else {
		events := eventList.Items
		sort.Slice(events, func(i, j int) bool {
			return events[i].LastTimestamp.Time.After(events[j].LastTimestamp.Time)
		})
		limit := min(len(events), 30)
		t.Logf("DEX-DEBUG: last %d events in namespace %s:", limit, xksDexNamespace)
		for _, event := range events[:limit] {
			t.Logf("DEX-DEBUG:   %s %s/%s reason=%s message=%s",
				event.Type, event.InvolvedObject.Kind, event.InvolvedObject.Name, event.Reason, event.Message)
		}
	}

	cm := &corev1.ConfigMap{}
	if err := tc.Client().Get(tc.Context(), types.NamespacedName{Name: xksDexConfigName, Namespace: xksDexNamespace}, cm); err != nil {
		t.Logf("DEX-DEBUG: failed to get configmap %s: %v", xksDexConfigName, err)
	} else if configYAML, ok := cm.Data["config.yaml"]; ok {
		t.Logf("DEX-DEBUG: dex config.yaml:\n%s", configYAML)
	}

	tlsSecret := &corev1.Secret{}
	if err := tc.Client().Get(tc.Context(), types.NamespacedName{Name: xksDexTLSName, Namespace: xksDexNamespace}, tlsSecret); err != nil {
		t.Logf("DEX-DEBUG: failed to get TLS secret %s: %v", xksDexTLSName, err)
	} else {
		t.Logf("DEX-DEBUG: TLS secret %s type=%s keys=%v", xksDexTLSName, tlsSecret.Type, sortedSecretKeys(tlsSecret.Data))
	}
}

func sortedSecretKeys(data map[string][]byte) []string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (tc *TestContext) logDexContainerLogs(t *testing.T, namespace, podName, containerName string, previous bool) {
	t.Helper()
	logType := "current"
	if previous {
		logType = "previous"
	}
	logs, err := retrievePodLogs(namespace, podName, containerName, previous)
	if err != nil {
		t.Logf("DEX-DEBUG: failed to fetch %s logs for %s/%s container %s: %v",
			logType, namespace, podName, containerName, err)
		return
	}
	if logs == "" {
		t.Logf("DEX-DEBUG: %s logs for %s/%s container %s: (empty)", logType, namespace, podName, containerName)
		return
	}
	t.Logf("DEX-DEBUG: %s logs for %s/%s container %s:\n%s", logType, namespace, podName, containerName, redactSensitiveInfo(logs))
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

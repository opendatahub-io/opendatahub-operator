/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

// RHOAIENG-96913: OIDC-mode substrate for OpenShift e2e on BYOIDC clusters
// (cluster Authentication type OIDC — the only configuration where the
// operator renders the OIDC kube-auth-proxy stack; on IntegratedOAuth
// clusters spec.oidc is silently ignored, finding on RHOAIENG-96913). The XKS
// bootstrap in gateway_dex_bootstrap_test.go stays untouched; everything here
// is additive and self-contained.

const (
	// The OCP bootstrap deliberately reuses the XKS Dex namespace and names so
	// the shared generateDexTLSAssets SANs (dex.dex-system.svc.cluster.local)
	// match this deployment too; the XKS and OCP suites never run on one
	// cluster, so the resources cannot collide.
	ocpDexNamespace = xksDexNamespace
	ocpDexIssuerURL = xksGatewayOIDCIssuerURL

	// Static identity for the default gateway's proxy while the GatewayConfig
	// points at the test Dex. The per-ingress identities are owned by the
	// notebook-ingress fixture (injectFixtureOIDCIdentities); this bootstrap
	// registers matching Dex clients by reading them, so only the default
	// identity lives here.
	ocpDexDefaultClientID      = "e2e-dex-default"
	ocpDexDefaultSecretName    = "e2e-dex-client-default"
	ocpDexProviderCASecretName = "e2e-dex-provider-ca"
)

// dexStaticClient is one client registration in the Dex server config.
type dexStaticClient struct {
	id          string
	secret      string
	redirectURI string
}

// ocpDexConfigYAML returns the Dex server config: the issuer is the in-cluster
// service URL, mockCallback avoids interactive login, and one static client +
// redirect URI is registered for the default gateway and for each additional
// ingress so every shard runs its own OAuth dance against the shared issuer.
func ocpDexConfigYAML(clients []dexStaticClient) string {
	entries := make([]string, 0, len(clients))
	for _, c := range clients {
		entries = append(entries, fmt.Sprintf("  - id: %s\n    name: %s\n    secret: %s\n    redirectURIs:\n      - %s\n",
			c.id, c.id, c.secret, c.redirectURI))
	}
	return fmt.Sprintf(`issuer: %s

storage:
  type: memory

web:
  https: 0.0.0.0:%d
  tlsCert: /etc/dex/tls/tls.crt
  tlsKey: /etc/dex/tls/tls.key

oauth2:
  skipApprovalScreen: true

enablePasswordDB: false

connectors:
  - type: mockCallback
    id: mock
    name: Mock

staticClients:
%s`, ocpDexIssuerURL, xksDexPort, strings.Join(entries, ""))
}

// newOCPDexConfigSecret builds the Secret holding the Dex server config. The
// config embeds the static clients' credentials, so it is stored as a Secret
// rather than a plain ConfigMap (CWE-312: ConfigMap read access is usually
// broader than Secret read access, and ConfigMaps are not covered by etcd
// encryption-at-rest for Secrets). Dex reads the file identically from either
// volume source.
func newOCPDexConfigSecret(clients []dexStaticClient) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      xksDexConfigName,
			Namespace: ocpDexNamespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"config.yaml": ocpDexConfigYAML(clients),
		},
	}
}

// ensureOCPDexConfigSecret applies the Dex server config. Always rewritten so
// an idempotent re-bootstrap registers the current shard client list. The
// mutate func is load-bearing: controllerutil.CreateOrUpdate decodes the live
// object into the desired one before running the mutation, so without it a
// re-bootstrap writes the existing config back unchanged and never registers
// the new clients (surfacing as Dex "Invalid client_id" on every shard login).
func (tc *TestContext) ensureOCPDexConfigSecret(t *testing.T, clients []dexStaticClient) {
	t.Helper()

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(newOCPDexConfigSecret(clients)),
		WithMutateFunc(testf.Mutate[*corev1.Secret](tc.Scheme(), func(s *corev1.Secret) error {
			s.Data = nil
			s.StringData = map[string]string{"config.yaml": ocpDexConfigYAML(clients)}
			return nil
		})),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)
}

// ensureOCPDexConfigVolumeIsSecret migrates a pre-existing Dex Deployment
// from the retired ConfigMap config volume to the Secret one, reporting
// whether the Deployment changed. Clusters bootstrapped before the config
// moved into a Secret keep reading the stale ConfigMap otherwise. The volume
// swap also bumps the pod-template restart annotation, so the caller must
// still wait for the rollout to complete.
func (tc *TestContext) ensureOCPDexConfigVolumeIsSecret(t *testing.T) bool {
	t.Helper()

	deployment := &appsv1.Deployment{}
	nn := types.NamespacedName{Name: xksDexName, Namespace: ocpDexNamespace}
	if err := tc.Client().Get(tc.Context(), nn, deployment); err != nil {
		t.Fatalf("failed to get Dex deployment for config volume migration: %v", err)
	}
	for i, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Name != "config" || volume.ConfigMap == nil {
			continue
		}
		deployment.Spec.Template.Spec.Volumes[i].ConfigMap = nil
		deployment.Spec.Template.Spec.Volumes[i].Secret = &corev1.SecretVolumeSource{SecretName: xksDexConfigName}
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = map[string]string{}
		}
		deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339Nano)
		if err := tc.Client().Update(tc.Context(), deployment); err != nil {
			t.Fatalf("failed to migrate Dex config volume to Secret: %v", err)
		}
		return true
	}
	return false
}

// deleteStaleOCPDexConfigMap removes the retired ConfigMap-based Dex config
// so the client credentials it carried do not outlive the migration to the
// Secret (CWE-312). Best-effort and idempotent.
func (tc *TestContext) deleteStaleOCPDexConfigMap(t *testing.T) {
	t.Helper()

	err := tc.Client().Delete(tc.Context(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: xksDexConfigName, Namespace: ocpDexNamespace},
	})
	if err != nil && !k8serr.IsNotFound(err) {
		t.Logf("failed to delete retired Dex config ConfigMap %s/%s: %v", ocpDexNamespace, xksDexConfigName, err)
	}
}

// ensureOCPDexProviderCASecret mirrors the Dex serving certificate into the
// gateway namespace as the provider CA Secret that kube-auth-proxy mounts
// (--provider-ca-file). Dex's certificate is self-signed, so it is its own CA;
// reading it from the Dex TLS Secret keeps the two consistent across
// idempotent re-bootstraps.
func (tc *TestContext) ensureOCPDexProviderCASecret(t *testing.T) {
	t.Helper()

	dexTLS := &corev1.Secret{}
	require.NoError(t, tc.Client().Get(tc.Context(), types.NamespacedName{
		Name:      xksDexTLSName,
		Namespace: ocpDexNamespace,
	}, dexTLS), "Dex TLS secret should exist")
	require.NotEmpty(t, dexTLS.Data["tls.crt"], "Dex TLS secret should carry tls.crt")

	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ocpDexProviderCASecretName,
			Namespace: gateway.GetGatewayNamespace(),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"ca.crt": dexTLS.Data["tls.crt"],
		},
	}
	// The mutate func makes the "consistent across re-bootstraps" guarantee
	// hold even if the Dex TLS Secret is ever regenerated.
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(caSecret),
		WithMutateFunc(testf.Mutate[*corev1.Secret](tc.Scheme(), func(s *corev1.Secret) error {
			if s.Data == nil {
				s.Data = map[string][]byte{}
			}
			s.Data["ca.crt"] = dexTLS.Data["tls.crt"]
			return nil
		})),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)
}

// ocpDexClients builds the Dex client list: a static client for the default
// gateway (persisting its client Secret in the gateway namespace) plus one
// client per additional ingress carrying the fixture's auth.oidc identity,
// with the client secret read from the Secret the fixture created. The
// registered cleanup deletes the default-gateway Secret and the provider CA
// Secret; it is expected to run after configureGatewayConfigForOIDC's restore
// (LIFO) so the Secrets are only removed once nothing references them.
func (tc *TestContext) ocpDexClients(t *testing.T, defaultHostname string, ingresses []serviceApi.AdditionalIngress) []dexStaticClient {
	t.Helper()
	namespace := gateway.GetGatewayNamespace()

	defaultSecretValue := randomClientSecret(t)
	t.Cleanup(func() {
		cleanupCtx := tc.Context()
		for _, secretName := range []string{ocpDexDefaultSecretName, ocpDexProviderCASecretName} {
			if err := tc.Client().Delete(cleanupCtx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name: secretName, Namespace: namespace,
			}}); err != nil && !k8serr.IsNotFound(err) {
				t.Errorf("delete Dex bootstrap secret %s: %v", secretName, err)
			}
		}
	})

	defaultSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ocpDexDefaultSecretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			fixtureOIDCClientSecretKey: defaultSecretValue,
		},
	}
	// The mutate func keeps the Secret in lockstep with the ConfigMap rewrite:
	// ocpDexClients generates a fresh random secret per run and registers it as
	// the default client's Dex credential, so the Secret must carry the same
	// value on re-bootstrap or the default gateway's token exchange breaks.
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(defaultSecret),
		WithMutateFunc(testf.Mutate[*corev1.Secret](tc.Scheme(), func(s *corev1.Secret) error {
			if s.StringData == nil {
				s.StringData = map[string]string{}
			}
			s.StringData[fixtureOIDCClientSecretKey] = defaultSecretValue
			return nil
		})),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)

	clients := make([]dexStaticClient, 0, 1+len(ingresses))
	clients = append(clients, dexStaticClient{
		id:          ocpDexDefaultClientID,
		secret:      defaultSecretValue,
		redirectURI: fmt.Sprintf("https://%s%s", defaultHostname, gateway.OAuthCallbackPath),
	})
	for _, ingress := range ingresses {
		require.NotNil(t, ingress.Auth.OIDC,
			"ingress %s must carry the fixture's auth.oidc identity on BYOIDC clusters", ingress.Name)
		secretNamespace := ingress.Auth.OIDC.SecretNamespace
		if secretNamespace == "" {
			secretNamespace = namespace
		}
		clientSecret := &corev1.Secret{}
		require.NoError(t, tc.Client().Get(tc.Context(), types.NamespacedName{
			Name:      ingress.Auth.OIDC.ClientSecretRef.Name,
			Namespace: secretNamespace,
		}, clientSecret), "fixture client secret for ingress %s should exist", ingress.Name)
		secretValue, ok := clientSecret.Data[ingress.Auth.OIDC.ClientSecretRef.Key]
		require.True(t, ok && len(secretValue) > 0,
			"fixture client secret for ingress %s should carry key %s", ingress.Name, ingress.Auth.OIDC.ClientSecretRef.Key)
		clients = append(clients, dexStaticClient{
			id:          ingress.Auth.OIDC.ClientID,
			secret:      string(secretValue),
			redirectURI: fmt.Sprintf("https://%s%s", ingress.Hostname, gateway.OAuthCallbackPath),
		})
	}
	return clients
}

// ensureDexForOCP deploys a minimal in-cluster Dex OIDC provider for OpenShift
// e2e on BYOIDC clusters, mirroring the XKS bootstrap (which stays untouched):
// Deployment + Service + TLS Secret + ConfigMap in a dedicated namespace, one
// static client + redirect URI for the default gateway and each additional
// ingress (reading the identities the notebook-ingress fixture assigned), and
// the provider CA Secret in the gateway namespace so kube-auth-proxy verifies
// Dex's self-signed certificate instead of skipping verification. The
// bootstrap is idempotent: a second call refreshes the client list and
// restarts Dex. Dex is left running after the test (like the XKS bootstrap);
// the namespace is plain and harmless on a qualification cluster.
func (tc *TestContext) ensureDexForOCP(t *testing.T, defaultHostname string, ingresses []serviceApi.AdditionalIngress) {
	t.Helper()

	clients := tc.ocpDexClients(t, defaultHostname, ingresses)

	dexDeploy := &appsv1.Deployment{}
	err := tc.Client().Get(tc.Context(), types.NamespacedName{
		Name:      xksDexName,
		Namespace: ocpDexNamespace,
	}, dexDeploy)
	if err == nil {
		t.Logf("Dex deployment already exists in %s, ensuring config is current", ocpDexNamespace)
		tc.ensureOCPDexConfigSecret(t, clients)
		// Clusters bootstrapped before the config moved to a Secret still
		// mount the ConfigMap: swap the volume (which also restarts the pod);
		// otherwise restart explicitly so Dex re-reads the refreshed config.
		if !tc.ensureOCPDexConfigVolumeIsSecret(t) {
			tc.restartXKSDexDeployment(t)
		}
		// The restart triggers a rolling update, and waitForDexDeploymentReady
		// only checks readyReplicas == 1 + Available — both already true while
		// the OLD pod (with the stale client list) still serves. Gate on full
		// rollout completion instead so callers never proceed against a Dex
		// missing the new shard clients (or mid-termination, losing its
		// in-memory auth-request state).
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.Deployment, types.NamespacedName{Name: xksDexName, Namespace: ocpDexNamespace}),
			WithCondition(And(
				jq.Match(`.status.observedGeneration >= .metadata.generation`),
				jq.Match(`.status.replicas == 1 and .status.updatedReplicas == 1 and .status.readyReplicas == 1`),
				jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`),
			)),
			WithEventuallyTimeout(tc.TestTimeouts.componentReadinessTimeout),
			WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
			WithCustomErrorMsg("Dex deployment %s/%s should complete its restart rollout", ocpDexNamespace, xksDexName),
		)
		tc.ensureOCPDexProviderCASecret(t)
		// Only after the new pod is up: drop the retired ConfigMap so its
		// credentials do not persist (CWE-312).
		tc.deleteStaleOCPDexConfigMap(t)
		return
	}
	if !k8serr.IsNotFound(err) {
		t.Fatalf("failed to check for existing Dex deployment: %v", err)
	}

	t.Logf("Bootstrapping Dex OIDC provider for OCP (issuer=%s)", ocpDexIssuerURL)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateNamespaceWithLabels(ocpDexNamespace, nil)),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)

	certPEM, keyPEM := generateDexTLSAssets(t)
	dexTLS := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      xksDexTLSName,
			Namespace: ocpDexNamespace,
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

	tc.ensureOCPDexConfigSecret(t, clients)
	// No Deployment exists in this branch, so nothing mounts the retired
	// ConfigMap: drop it right away if a previous bootstrap left one.
	tc.deleteStaleOCPDexConfigMap(t)

	dexDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      xksDexName,
			Namespace: ocpDexNamespace,
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
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "config", MountPath: "/etc/dex", ReadOnly: true},
								{Name: "tls", MountPath: "/etc/dex/tls", ReadOnly: true},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt32(xksDexPort),
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
								Secret: &corev1.SecretVolumeSource{
									SecretName: xksDexConfigName,
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
			Namespace: ocpDexNamespace,
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
	tc.ensureOCPDexProviderCASecret(t)
	t.Log("Dex OIDC provider bootstrap for OCP completed")
}

// configureGatewayConfigForOIDC points the cluster's GatewayConfig at the test
// Dex issuer: spec.oidc for the default gateway plus the provider CA Secret.
// The per-ingress auth.oidc identities were assigned by the fixture before
// admission, so additionalIngresses is deliberately not touched here. The
// registered cleanup restores the original spec fields and then waits for the
// default auth proxy to re-render against the original issuer, so the cluster
// is never left mid-flip and the Dex-owned Secrets (deleted by a cleanup
// registered earlier, hence running later) are unreferenced by then.
func (tc *GatewayTestCtx) configureGatewayConfigForOIDC(t *testing.T, fixture *notebookIngressFixture) {
	t.Helper()
	g := NewWithT(t)
	ctx := tc.Context()

	t.Cleanup(func() {
		cleanupCtx := tc.Context()
		g.Eventually(func() error {
			return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				current := &serviceApi.GatewayConfig{}
				if err := tc.Client().Get(cleanupCtx, types.NamespacedName{Name: gatewayConfigName}, current); err != nil {
					return err
				}
				current.Spec.OIDC = fixture.original.Spec.OIDC
				current.Spec.ProviderCASecretName = fixture.original.Spec.ProviderCASecretName
				current.Spec.VerifyProviderCertificate = fixture.original.Spec.VerifyProviderCertificate
				return tc.Client().Update(cleanupCtx, current)
			})
		}).Should(Succeed())

		if fixture.original.Spec.OIDC != nil && fixture.original.Spec.OIDC.IssuerURL != "" {
			originalIssuer := fixture.original.Spec.OIDC.IssuerURL
			g.Eventually(func(g Gomega) {
				deployment := &appsv1.Deployment{}
				g.Expect(tc.Client().Get(cleanupCtx, types.NamespacedName{
					Name: kubeAuthProxyName, Namespace: tc.gatewayNamespace(),
				}, deployment)).To(Succeed())
				g.Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--oidc-issuer-url="+originalIssuer),
					"default auth proxy should re-render against the original issuer %s", originalIssuer)
			}, tc.TestTimeouts.componentReadinessTimeout, 5*time.Second).Should(Succeed())
		}
	})

	g.Eventually(func() error {
		return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			current := &serviceApi.GatewayConfig{}
			if err := tc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, current); err != nil {
				return err
			}
			current.Spec.OIDC = &serviceApi.OIDCConfig{
				IssuerURL: ocpDexIssuerURL,
				ClientID:  ocpDexDefaultClientID,
				ClientSecretRef: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: ocpDexDefaultSecretName},
					Key:                  fixtureOIDCClientSecretKey,
				},
				SecretNamespace: tc.gatewayNamespace(),
			}
			current.Spec.ProviderCASecretName = ocpDexProviderCASecretName
			return tc.Client().Update(ctx, current)
		})
	}).Should(Succeed())
}

// requireOIDCProxyPodsSettled asserts that the named auth proxy's pods are
// running with at least the HPA's two-replica floor and zero container
// restarts. kube-auth-proxy runs with --skip-oidc-discovery=false, so it
// discovers the issuer at startup; an unreachable or untrusted issuer exits
// the process, which surfaces as restarts rather than a settled deployment.
func (tc *GatewayTestCtx) requireOIDCProxyPodsSettled(t *testing.T, proxyName string) {
	t.Helper()
	g := NewWithT(t)
	ctx := tc.Context()

	pods := &corev1.PodList{}
	g.Eventually(func(g Gomega) {
		g.Expect(tc.Client().List(ctx, pods,
			client.InNamespace(tc.gatewayNamespace()),
			client.MatchingLabels{"app": proxyName},
		)).To(Succeed())
		running := 0
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil {
				continue
			}
			g.Expect(pod.Status.ContainerStatuses).NotTo(BeEmpty())
			for _, status := range pod.Status.ContainerStatuses {
				g.Expect(status.RestartCount).To(BeZero(),
					"proxy %s pod %s restarted; OIDC discovery likely failed", proxyName, pod.Name)
			}
			if pod.Status.Phase == corev1.PodRunning {
				running++
			}
		}
		g.Expect(running).To(BeNumerically(">=", 2),
			"proxy %s should keep at least two running replicas", proxyName)
	}, tc.TestTimeouts.authGatewayTimeout, 5*time.Second).Should(Succeed())
}

// requireIngressAuthenticationReady asserts the named additional ingress
// reports AuthenticationReady=True in the GatewayConfig status, describing the
// CURRENT generation. Without the ObservedGeneration gate a True condition
// left over from a previous generation (e.g. the fixture admits shards before
// configureGatewayConfigForOIDC updates spec.oidc) satisfies the check before
// the operator reconciles the new issuer — the operator's own status logic
// treats an observedGeneration mismatch as not-ready; this mirrors it.
func (tc *GatewayTestCtx) requireIngressAuthenticationReady(t *testing.T, ingressName string) {
	t.Helper()
	g := NewWithT(t)
	ctx := tc.Context()

	g.Eventually(func(g Gomega) {
		gatewayConfig := &serviceApi.GatewayConfig{}
		g.Expect(tc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, gatewayConfig)).To(Succeed())
		var status *serviceApi.AdditionalIngressStatus
		for i := range gatewayConfig.Status.AdditionalIngresses {
			if gatewayConfig.Status.AdditionalIngresses[i].Name == ingressName {
				status = &gatewayConfig.Status.AdditionalIngresses[i]
			}
		}
		g.Expect(status).NotTo(BeNil(), "ingress %s missing from GatewayConfig status", ingressName)
		found := false
		for _, condition := range status.Conditions {
			if condition.Type == serviceApi.AdditionalIngressAuthenticationReadyConditionType {
				found = true
				g.Expect(condition.ObservedGeneration).To(Equal(gatewayConfig.Generation),
					"ingress %s AuthenticationReady must describe the current GatewayConfig generation", ingressName)
				g.Expect(condition.Status).To(Equal(metav1.ConditionTrue),
					"ingress %s should report AuthenticationReady", ingressName)
			}
		}
		g.Expect(found).To(BeTrue(), "ingress %s has no AuthenticationReady condition", ingressName)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
}

// ValidateAdditionalIngressOIDCAuthReadiness verifies the OCP Dex bootstrap
// end to end (RHOAIENG-96913) on BYOIDC clusters: with the GatewayConfig
// pointed at the in-cluster Dex issuer, every shard's kube-auth-proxy renders
// its own OIDC identity against the shared issuer with the provider CA
// mounted, and startup discovery succeeds (settled pods, zero restarts). The
// original issuer configuration is verified restored when the test finishes.
func (tc *GatewayTestCtx) ValidateAdditionalIngressOIDCAuthReadiness(t *testing.T) {
	t.Helper()

	tc.SkipIfXKSCluster(t)
	skipUnless(t, Tier2)
	// The OIDC proxy stack only renders on BYOIDC clusters (cluster
	// Authentication type OIDC); on IntegratedOAuth clusters spec.oidc is
	// silently ignored (finding on RHOAIENG-96913).
	tc.SkipUnlessBYOIDC(t)
	if !tc.isOcpRouteMode(t) {
		t.Skip("Skipping test: requires OcpRoute ingress mode")
	}

	ctx := tc.Context()
	g := NewWithT(t)

	fixture := newNotebookIngressFixture(t, tc)
	defaultHostname := tc.getExpectedGatewayHostname(t)
	tc.ensureDexForOCP(t, defaultHostname, fixture.ingresses)
	tc.configureGatewayConfigForOIDC(t, fixture)

	// Authentication identity: every shard renders its credentials Secret with
	// its own OIDC client ID, distinct from the default gateway's.
	// The client-ID Equal is safe to print (it is public); the client/cookie
	// secret keys are asserted via ok/length only, because a HaveKey failure
	// would format the whole Data map — the raw secret values — into the
	// publicly readable CI build log (CWE-532).
	for _, ingress := range fixture.ingresses {
		credsName := perIngressCredsName(ingress.Name)
		expectedClientID := fixtureOIDCClientID(ingress.Name)
		creds := &corev1.Secret{}
		g.Eventually(func(g Gomega) {
			g.Expect(tc.Client().Get(ctx, types.NamespacedName{
				Name: credsName, Namespace: tc.gatewayNamespace(),
			}, creds)).To(Succeed())
			g.Expect(string(creds.Data[gateway.EnvClientID])).To(Equal(expectedClientID))
			for _, key := range []string{gateway.EnvClientSecret, gateway.EnvCookieSecret} {
				value, ok := creds.Data[key]
				g.Expect(ok).To(BeTrue(),
					"credentials secret %s should carry %s", credsName, key)
				g.Expect(value).NotTo(BeEmpty(),
					"credentials secret %s should carry a non-empty %s", credsName, key)
			}
		}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed(),
			"credentials secret %s should carry the shard-specific OIDC identity", credsName)
	}
	defaultCreds := &corev1.Secret{}
	g.Eventually(func(g Gomega) {
		g.Expect(tc.Client().Get(ctx, types.NamespacedName{
			Name: kubeAuthProxyCredsName, Namespace: tc.gatewayNamespace(),
		}, defaultCreds)).To(Succeed())
		g.Expect(string(defaultCreds.Data[gateway.EnvClientID])).To(Equal(ocpDexDefaultClientID))
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed(),
		"default gateway should use the default Dex client")

	// Proxy wiring: each shard's deployment renders for OIDC mode with the
	// Dex issuer, the provider CA mounted, and its own redirect URL.
	for _, ingress := range fixture.ingresses {
		proxyName := perIngressProxyName(ingress.Name)
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.Deployment, types.NamespacedName{
				Name:      proxyName,
				Namespace: tc.gatewayNamespace(),
			}),
			WithCondition(And(
				jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`),
				jq.Match(`.spec.template.spec.containers[0].args | any(. == "--provider=oidc")`),
				jq.Match(`.spec.template.spec.containers[0].args | any(. == "--oidc-issuer-url=%s")`, ocpDexIssuerURL),
				jq.Match(`.spec.template.spec.containers[0].args | any(. == "--skip-oidc-discovery=false")`),
				jq.Match(`.spec.template.spec.containers[0].args | any(. == "--provider-ca-file=/etc/provider-ca/ca.crt")`),
				jq.Match(`.spec.template.spec.containers[0].args | any(. == "%s")`, makeRedirectURL(ingress.Hostname)),
				jq.Match(`.spec.template.spec.containers[0].args | any(. == "%s")`, makeCookieDomain(ingress.Hostname)),
				jq.Match(`.spec.template.spec.volumes[] | select(.name == "provider-ca-cert") | .secret.secretName == "%s"`, ocpDexProviderCASecretName),
				jq.Match(`.spec.template.spec.containers[0].env[] | select(.name == "%s") | .valueFrom.secretKeyRef.name == "%s"`, gateway.EnvClientID, perIngressCredsName(ingress.Name)),
			)),
			WithEventuallyTimeout(tc.TestTimeouts.authGatewayTimeout),
			WithCustomErrorMsg("auth proxy %s should render for OIDC mode with the Dex issuer and CA", proxyName),
		)
		tc.requireOIDCProxyPodsSettled(t, proxyName)
		tc.requireIngressAuthenticationReady(t, ingress.Name)
	}

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: gatewayConfigName}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`)),
		WithEventuallyTimeout(tc.TestTimeouts.authGatewayTimeout),
		WithCustomErrorMsg("GatewayConfig should report Ready in OIDC mode"),
	)
}

package e2e_test

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
)

const (
	xksGatewayOIDCSecretName   = "oidc-client-secret"
	xksGatewayOIDCClientSecret = "e2e-test-dummy-oidc-client-secret"
	xksGatewayOIDCClientID     = "odh-gateway"
	xksGatewayDomain           = "kind.local"
)

// EnsureGatewayConfigForXKS bootstraps the minimum gateway prerequisites on vanilla
// Kubernetes when GatewayConfig is absent. On cloud XKS, Helm/AKE normally creates
// this CR; on KinD e2e there is no AKE, so tests create it here (same pattern as
// EnsurePlatformCR).
func (tc *TestContext) EnsureGatewayConfigForXKS(t *testing.T) {
	t.Helper()

	tc.ensureDexForXKS(t)

	gatewayConfig := &serviceApi.GatewayConfig{}
	err := tc.Client().Get(tc.Context(), types.NamespacedName{Name: serviceApi.GatewayConfigName}, gatewayConfig)
	if err == nil {
		if tc.updateXKSGatewayConfigForE2E(t, gatewayConfig) {
			tc.waitForXKSGatewayConfigReady(t)
		} else {
			t.Logf("GatewayConfig %q already exists with expected KinD e2e OIDC settings", serviceApi.GatewayConfigName)
		}
		return
	}
	if !k8serr.IsNotFound(err) {
		t.Fatalf("failed to check for existing GatewayConfig: %v", err)
	}

	gatewayNS := gateway.GetGatewayNamespace()
	t.Logf("Bootstrapping GatewayConfig for xKS (namespace=%s, domain=%s, issuer=%s)",
		gatewayNS, xksGatewayDomain, xksGatewayOIDCIssuerURL)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateNamespaceWithLabels(gatewayNS, nil)),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)

	oidcSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      xksGatewayOIDCSecretName,
			Namespace: gatewayNS,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"clientSecret": xksGatewayOIDCClientSecret,
		},
	}
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(oidcSecret),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)

	defaultGateway := newXKSGatewayConfig()
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(defaultGateway),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)

	tc.waitForXKSGatewayConfigReady(t)
	t.Log("GatewayConfig bootstrap completed")
}

func newXKSGatewayConfig() *serviceApi.GatewayConfig {
	gatewayNS := gateway.GetGatewayNamespace()
	return &serviceApi.GatewayConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: serviceApi.GroupVersion.String(),
			Kind:       serviceApi.GatewayConfigKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName,
		},
		Spec: serviceApi.GatewayConfigSpec{
			IngressMode: serviceApi.IngressModeLoadBalancer,
			Domain:      xksGatewayDomain,
			Certificate: &infrav1.CertificateSpec{
				Type:       infrav1.SelfSigned,
				SecretName: gateway.DefaultGatewayTLSSecretName,
			},
			Cookie: serviceApi.CookieConfig{
				Expire:  metav1.Duration{Duration: 24 * time.Hour},
				Refresh: metav1.Duration{Duration: 1 * time.Hour},
			},
			AuthProxyTimeout:          metav1.Duration{Duration: 5 * time.Second},
			VerifyProviderCertificate: new(false),
			OIDC: &serviceApi.OIDCConfig{
				IssuerURL: xksGatewayOIDCIssuerURL,
				ClientID:  xksGatewayOIDCClientID,
				ClientSecretRef: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: xksGatewayOIDCSecretName},
					Key:                  "clientSecret",
				},
				SecretNamespace: gatewayNS,
			},
		},
	}
}

// updateXKSGatewayConfigForE2E patches legacy KinD bootstrap values (e.g. dex.kind.local)
// so kube-auth-proxy can reach the in-cluster Dex issuer.
func (tc *TestContext) updateXKSGatewayConfigForE2E(t *testing.T, gatewayConfig *serviceApi.GatewayConfig) bool {
	t.Helper()

	expected := newXKSGatewayConfig().Spec
	needsUpdate := gatewayConfig.Spec.OIDC == nil ||
		gatewayConfig.Spec.OIDC.IssuerURL != expected.OIDC.IssuerURL ||
		gatewayConfig.Spec.VerifyProviderCertificate == nil ||
		*gatewayConfig.Spec.VerifyProviderCertificate != false

	if !needsUpdate {
		return false
	}

	t.Logf("Updating GatewayConfig OIDC settings for KinD e2e (issuer=%s)", xksGatewayOIDCIssuerURL)
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: serviceApi.GatewayConfigName}),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.verifyProviderCertificate = false`),
			testf.Transform(`.spec.oidc.issuerURL = "%s"`, xksGatewayOIDCIssuerURL),
			testf.Transform(`.spec.oidc.clientID = "%s"`, xksGatewayOIDCClientID),
			testf.Transform(`.spec.oidc.clientSecretRef.name = "%s"`, xksGatewayOIDCSecretName),
			testf.Transform(`.spec.oidc.clientSecretRef.key = "clientSecret"`),
			testf.Transform(`.spec.oidc.secretNamespace = "%s"`, gateway.GetGatewayNamespace()),
		)),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)

	return true
}

func (tc *TestContext) waitForXKSGatewayConfigReady(t *testing.T) {
	t.Helper()

	readyCondition := jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionTrue)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: serviceApi.GatewayConfigName}),
		WithCondition(readyCondition),
		WithEventuallyTimeout(tc.TestTimeouts.authGatewayTimeout),
		WithCustomErrorMsg("bootstrapped GatewayConfig should become Ready"),
	)
}

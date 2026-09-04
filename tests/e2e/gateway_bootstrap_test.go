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
)

const (
	xksGatewayOIDCSecretName   = "oidc-client-secret"
	xksGatewayOIDCClientSecret = "e2e-test-dummy-oidc-client-secret"
	xksGatewayOIDCIssuerURL    = "https://dex.kind.local"
	xksGatewayOIDCClientID     = "odh-gateway"
	xksGatewayDomain           = "kind.local"
)

// EnsureGatewayConfigForXKS bootstraps the minimum gateway prerequisites on vanilla
// Kubernetes when GatewayConfig is absent. On cloud XKS, Helm/AKE normally creates
// this CR; on KinD e2e there is no AKE, so tests create it here (same pattern as
// EnsurePlatformCR).
func (tc *TestContext) EnsureGatewayConfigForXKS(t *testing.T) {
	t.Helper()

	gatewayConfig := &serviceApi.GatewayConfig{}
	err := tc.Client().Get(tc.Context(), types.NamespacedName{Name: serviceApi.GatewayConfigName}, gatewayConfig)
	if err == nil {
		t.Logf("GatewayConfig %q already exists, skipping bootstrap", serviceApi.GatewayConfigName)
		return
	}
	if !k8serr.IsNotFound(err) {
		t.Fatalf("failed to check for existing GatewayConfig: %v", err)
	}

	gatewayNS := gateway.GetGatewayNamespace()
	t.Logf("Bootstrapping GatewayConfig for xKS (namespace=%s, domain=%s)", gatewayNS, xksGatewayDomain)

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

	defaultGateway := &serviceApi.GatewayConfig{
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
			AuthProxyTimeout: metav1.Duration{Duration: 5 * time.Second},
			OIDC: &serviceApi.OIDCConfig{
				IssuerURL: xksGatewayOIDCIssuerURL,
				ClientID:  xksGatewayOIDCClientID,
				ClientSecretRef: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: xksGatewayOIDCSecretName},
					Key:                  "clientSecret",
				},
			},
		},
	}
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(defaultGateway),
		WithEventuallyTimeout(tc.TestTimeouts.crCreationTimeout),
	)

	readyCondition := jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionTrue)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: serviceApi.GatewayConfigName}),
		WithCondition(readyCondition),
		WithEventuallyTimeout(tc.TestTimeouts.authGatewayTimeout),
		WithCustomErrorMsg("bootstrapped GatewayConfig should become Ready"),
	)

	t.Log("GatewayConfig bootstrap completed")
}

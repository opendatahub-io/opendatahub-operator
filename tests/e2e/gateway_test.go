package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

const (
	gatewayName          = "odh-gateway"
	gatewayClassName     = "odh-gateway-class"
	gatewayNamespace     = "openshift-ingress"
	gatewayServiceName   = "default-gateway"
	gatewayTLSSecretName = "default-gateway-tls"
)

type GatewayTestCtx struct {
	*TestContext
}

func gatewayTestSuite(t *testing.T) { //nolint:unused
	t.Helper()

	ctx, err := NewTestContext(t)
	require.NoError(t, err)

	componentCtx := GatewayTestCtx{
		TestContext: ctx,
	}

	testCases := []TestCase{
		{"Validate Gateway infrastructure creation", componentCtx.ValidateGatewayInfrastructure},
		{"Validate kube-auth-proxy deployment with secret hash annotation", componentCtx.ValidateKubeAuthProxySecretHash},
	}

	RunTestCases(t, testCases)
}

func (tc *GatewayTestCtx) ValidateGatewayInfrastructure(t *testing.T) {
	t.Helper()

	t.Log("Validating Gateway service and API resources creation")

	// First ensure GatewayConfig exists and has proper configuration
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: gatewayServiceName}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue)),
		WithCustomErrorMsg("GatewayConfig should have ProvisioningSucceeded condition with status True"),
	)

	// Validate GatewayClass
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayClass, types.NamespacedName{Name: gatewayClassName}),
	)

	// Validate certificate secret
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Secret, types.NamespacedName{
			Name:      gatewayTLSSecretName,
			Namespace: gatewayNamespace,
		}),
	)

	// Validate Gateway API resource with configuration
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KubernetesGateway, types.NamespacedName{
			Name:      gatewayName,
			Namespace: gatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.gatewayClassName == "%s"`, gatewayClassName),
			jq.Match(`.spec.listeners | length > 0`),
			jq.Match(`.spec.listeners[] | select(.name == "https") | .protocol == "%s"`, string(gwapiv1.HTTPSProtocolType)),
			jq.Match(`.spec.listeners[] | select(.name == "https") | .port == 443`),
			jq.Match(`.spec.listeners[] | select(.name == "https") | .tls.certificateRefs[0].name == "%s"`, gatewayTLSSecretName),
		)),
		WithCustomErrorMsg("Gateway should be created with correct HTTPS configuration"),
	)

	t.Log("Gateway API resources validation completed successfully")
}

func (tc *GatewayTestCtx) ValidateKubeAuthProxySecretHash(t *testing.T) {
	t.Helper()
	// Validate kube-auth-proxy deployment exists and has the secret hash annotation
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      "kube-auth-proxy",
			Namespace: gatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.template.metadata.annotations["opendatahub.io/secret-hash"] != null`),
			jq.Match(`.spec.template.metadata.annotations["opendatahub.io/secret-hash"] | test("^[0-9a-f]{64}$|^$")`),
		)),
		WithCustomErrorMsg("kube-auth-proxy deployment should have 'opendatahub.io/secret-hash' annotation that is either empty or a 64-char hex SHA-256 hash"),
	)

	t.Log("kube-auth-proxy deployment secret hash annotation validation completed successfully")
}

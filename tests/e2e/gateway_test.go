package e2e_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/types"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

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

func gatewayTestSuite(t *testing.T) {
	t.Helper()
	ctx, err := NewTestContext(t)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Gateway infrastructure validation", func(t *testing.T) {
		validateGatewayCreation(t, ctx)
	})
}

func validateGatewayCreation(t *testing.T, tc *TestContext) {
	t.Helper()

	t.Log("Validating Gateway service and API resources creation")

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: gatewayServiceName}),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayClass, types.NamespacedName{Name: gatewayClassName}),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Secret, types.NamespacedName{
			Name:      gatewayTLSSecretName,
			Namespace: gatewayNamespace,
		}),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayAPI, types.NamespacedName{
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

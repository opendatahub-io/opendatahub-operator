package e2e_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

//nolint:unused
const (
	gatewayName          = "odh-gateway"
	gatewayClassName     = "odh-gateway-class"
	gatewayNamespace     = "openshift-ingress"
	gatewayServiceName   = "default-gateway"
	gatewayTLSSecretName = "default-gateway-tls"
)

var (
	GatewayServiceGVK = schema.GroupVersionKind{
		Group:   "services.platform.opendatahub.io",
		Version: "v1alpha1",
		Kind:    "GatewayConfig",
	}

	GatewayClassGVK = schema.GroupVersionKind{
		Group:   "gateway.networking.k8s.io",
		Version: "v1",
		Kind:    "GatewayClass",
	}

	GatewayGVK = schema.GroupVersionKind{
		Group:   "gateway.networking.k8s.io",
		Version: "v1",
		Kind:    "Gateway",
	}

	SecretGVK = schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Secret",
	}
)

func gatewayTestSuite(t *testing.T) { //nolint:unused
	t.Helper()
	ctx, err := NewTestContext(t)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Gateway infrastructure validation", func(t *testing.T) {
		validateGatewayCreation(t, ctx)
	})
}

func validateGatewayCreation(t *testing.T, ctx *TestContext) { //nolint:unused
	t.Helper()

	t.Log("Validating Gateway service and API resources creation")

	t.Run("GatewayConfig service CR should be created", func(t *testing.T) {
		ctx.EnsureResourceExists(
			WithMinimalObject(GatewayServiceGVK, types.NamespacedName{Name: gatewayServiceName}),
		)
	})

	t.Run("GatewayClass should be created", func(t *testing.T) {
		ctx.EnsureResourceExists(
			WithMinimalObject(GatewayClassGVK, types.NamespacedName{Name: gatewayClassName}),
		)
	})

	t.Run("Certificate secret should be created", func(t *testing.T) {
		ctx.EnsureResourceExists(
			WithMinimalObject(SecretGVK, types.NamespacedName{
				Name:      gatewayTLSSecretName,
				Namespace: gatewayNamespace,
			}),
		)
	})

	// Validate Gateway API resource with configuration
	ctx.EnsureResourceExists(
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

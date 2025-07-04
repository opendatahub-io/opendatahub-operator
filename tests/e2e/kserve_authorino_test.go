package e2e_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

type KserveAuthorinoTestCtx struct {
	*TestContext
}

// authRelatedResources defines the authorization-related resources that should NOT be created
// when Authorino is not installed.
var authRelatedResources = []struct {
	gvk schema.GroupVersionKind
	nn  types.NamespacedName
}{
	{gvk.EnvoyFilter, types.NamespacedName{Namespace: "istio-system", Name: "activator-host-header"}},
	{gvk.EnvoyFilter, types.NamespacedName{Namespace: "istio-system", Name: "envoy-oauth-temp-fix-after"}},
	{gvk.EnvoyFilter, types.NamespacedName{Namespace: "istio-system", Name: "envoy-oauth-temp-fix-before"}},
	{gvk.EnvoyFilter, types.NamespacedName{Namespace: "istio-system", Name: "kserve-inferencegraph-host-header"}},
	{gvk.AuthorizationPolicy, types.NamespacedName{Namespace: "istio-system", Name: "kserve-inferencegraph"}},
	{gvk.AuthorizationPolicy, types.NamespacedName{Namespace: "istio-system", Name: "kserve-predictor"}},
}

// TestKserveAuthorinoRegression tests the regression scenario where auth-related resources
// were created even when Authorino was not installed (RHOAI 2.19.0 issue).
func TestKserveAuthorinoRegression(t *testing.T) {
	t.Helper()

	ctx, err := NewTestContext(t)
	require.NoError(t, err)

	testCtx := KserveAuthorinoTestCtx{
		TestContext: ctx,
	}

	testCases := []TestCase{
		{"Verify Authorino is not installed", testCtx.VerifyAuthorinoNotInstalled},
		{"Validate auth resources are not created", testCtx.ValidateAuthResourcesNotCreated},
	}

	RunTestCases(t, testCases)
}

// VerifyAuthorinoNotInstalled ensures that Authorino is not installed in the cluster.
func (tc *KserveAuthorinoTestCtx) VerifyAuthorinoNotInstalled(t *testing.T) {
	t.Helper()

	// Check that Authorino subscription does not exist
	tc.EnsureResourceDoesNotExist(
		WithMinimalObject(gvk.Subscription, types.NamespacedName{
			Namespace: "openshift-operators",
			Name:      "authorino-operator",
		}),
		WithCustomErrorMsg("Authorino subscription should not exist for this test"),
	)

	tc.EnsureResourceDoesNotExist(
		WithMinimalObject(gvk.Subscription, types.NamespacedName{
			Namespace: tc.OperatorNamespace,
			Name:      "authorino-operator",
		}),
		WithCustomErrorMsg("Authorino subscription should not exist in operator namespace for this test"),
	)
}

// ValidateAuthResourcesNotCreated verifies that auth-related resources are not created
// when Authorino is not installed.
func (tc *KserveAuthorinoTestCtx) ValidateAuthResourcesNotCreated(t *testing.T) {
	t.Helper()

	// Check that EnvoyFilters and AuthorizationPolicies are not created
	for _, resource := range authRelatedResources {
		tc.EnsureResourceDoesNotExist(
			WithMinimalObject(resource.gvk, resource.nn),
			WithCustomErrorMsg(
				"Auth resource %s/%s should not exist when Authorino is not installed (RHOAI 2.19.0 regression)",
				resource.gvk.Kind,
				resource.nn.Name,
			),
		)
	}

	time.Sleep(10 * time.Second)
	for _, resource := range authRelatedResources {
		tc.EnsureResourceDoesNotExist(
			WithMinimalObject(resource.gvk, resource.nn),
			WithCustomErrorMsg(
				"Auth resource %s/%s should consistently not exist when Authorino is not installed",
				resource.gvk.Kind,
				resource.nn.Name,
			),
		)
	}

	t.Logf("SUCCESS: No auth-related resources were created when Authorino is not installed")
}

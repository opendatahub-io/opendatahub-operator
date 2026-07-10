package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const aiGatewayControllerDeployment = "ai-gateway-operator"

func aiGatewayTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	moduleGVK := schema.GroupVersionKind{
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.AIGatewayKind,
	}
	moduleCRNN := types.NamespacedName{Name: componentApi.AIGatewayInstanceName}
	controllerNN := types.NamespacedName{
		Namespace: tc.AppsNamespace,
		Name:      aiGatewayControllerDeployment,
	}

	testCases := []TestCase{
		{"Validate component enabled", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Smoke, Tier1)

			if !tc.IsXKS() {
				tc.EventuallyResourcePatched(
					WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
					WithMutateFunc(testf.Transform(`.spec.components.aigateway.managementState = "Removed"`)),
					WithCondition(jq.Match(`.spec.components.aigateway.managementState == "Removed"`)),
				)
				tc.EnsureResourceGone(WithMinimalObject(moduleGVK, moduleCRNN))
			}

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.aigateway.managementState = "Managed"`)),
				WithCondition(jq.Match(`.spec.components.aigateway.managementState == "Managed"`)),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(moduleGVK, moduleCRNN),
				WithCondition(And(
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
				)),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Deployment, controllerNN),
				WithCondition(jq.Match(`.status.readyReplicas >= 1`)),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeModulesReady, metav1.ConditionTrue)),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithCondition(jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.AIGatewayKind, metav1.ConditionTrue)),
				WithCustomErrorMsg("DataScienceCluster should have %sReady condition set to True", componentApi.AIGatewayKind),
			)
		}},
		{"Validate component disabled", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Smoke, Tier1)

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.aigateway.managementState = "Removed"`)),
				WithCondition(jq.Match(`.spec.components.aigateway.managementState == "Removed"`)),
			)

			tc.EnsureResourceGone(WithMinimalObject(moduleGVK, moduleCRNN))
			tc.EnsureResourceGone(WithMinimalObject(gvk.Deployment, controllerNN))

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithCondition(And(
					jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.AIGatewayKind, metav1.ConditionFalse),
					jq.Match(`.status.conditions[] | select(.type == "%sReady") | .reason == "%s"`, componentApi.AIGatewayKind, status.RemovedReason),
				)),
				WithCustomErrorMsg("DataScienceCluster should have %sReady condition set to False/Removed", componentApi.AIGatewayKind),
			)
		}},
	}

	RunTestCases(t, testCases)
}

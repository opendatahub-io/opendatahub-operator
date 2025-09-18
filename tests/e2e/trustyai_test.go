package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

type TrustyAITestCtx struct {
	*ComponentTestCtx
}

func trustyAITestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.TrustyAI{})
	require.NoError(t, err)

	componentCtx := TrustyAITestCtx{
		ComponentTestCtx: ct,
	}

	// TrustyAI requires some CRDs that are shipped by Kserve
	t.Run("Enable Kserve", componentCtx.EnableKserve)

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate pre check", componentCtx.ValidateTrustyAIPreCheck},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateTrustyAIPreCheck validates the dependency checking logic of TrustyAI.
// Test Scenario: TrustyAI should fail when KServe dependencies are missing and recover when restored.
// Steps:
//  1. Disable KServe → TrustyAI should detect missing dependency
//  2. Delete InferenceServices CRD → Trigger validation error
//  3. Validate Error State → TrustyAI conditions should be False
//  4. Enable KServe → Restore dependency
//  5. Validate Recovery → TrustyAI conditions should be True
//  6. Cleanup → Disable KServe for next test
func (tc *TrustyAITestCtx) ValidateTrustyAIPreCheck(t *testing.T) {
	t.Helper()

	// Define test cases.
	testCases := []TestCase{
		// Force TrustyAI into error state
		{"Disable Kserve for Error Test", tc.DisableKserve},
		// Remove required CRD to trigger validation error
		{"Delete InferenceServices", tc.DeleteInferenceServices},
		// Verify TrustyAI detects missing dependency
		{"Validate Error", func(t *testing.T) {
			t.Helper()
			tc.ValidateTrustyAICondition(metav1.ConditionFalse)
		}},
		// Restore Kserve to fix dependency
		{"Enable Kserve", tc.EnableKserve},
		// Verify TrustyAI recovers automatically
		{"Validate Recovery", func(t *testing.T) {
			t.Helper()
			tc.ValidateTrustyAICondition(metav1.ConditionTrue)
		}},
		// Clean up for next test
		{"Disable Kserve for Cleanup", tc.DisableKserve},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// EnableKserve enables the Kserve component for the TrustyAI test context.
func (tc *TrustyAITestCtx) EnableKserve(t *testing.T) {
	t.Helper()
	tc.SetKserveState(operatorv1.Managed, true)
}

// DisableKserve disables the Kserve component for the TrustyAI test context.
func (tc *TrustyAITestCtx) DisableKserve(t *testing.T) {
	t.Helper()
	tc.SetKserveState(operatorv1.Removed, false)
}

// SetKserveState updates the Kserve component state and verifies its existence.
func (tc *TrustyAITestCtx) SetKserveState(state operatorv1.ManagementState, shouldExist bool) {
	// Temporarily change timeout for this test since it takes lots of time because of FeatureGates
	// TODO: remove it once we understood why it's taking lots of time for kserve to become Ready/NotReady
	reset := tc.OverrideEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout, tc.TestTimeouts.defaultEventuallyPollInterval)
	defer reset() // Ensure reset happens after test completes

	nn := types.NamespacedName{Name: componentApi.KserveInstanceName}

	// Update the Kserve component state in DataScienceCluster.
	tc.UpdateComponentStateInDataScienceClusterWithKind(state, gvk.Kserve.Kind)

	// Verify if Kserve should exist or be removed.
	if shouldExist {
		// KServe can take longer to become ready, especially in CI environments
		tc.ValidateComponentCondition(
			gvk.Kserve,
			componentApi.KserveInstanceName,
			status.ConditionTypeReady,
		)
	} else {
		tc.DeleteResource(
			WithMinimalObject(gvk.Kserve, nn),
			WithIgnoreNotFound(true),
			WithRemoveFinalizersOnDelete(true),
		)
	}
}

// DeleteInferenceServices deletes the InferenceServices CustomResourceDefinition.
func (tc *TrustyAITestCtx) DeleteInferenceServices(t *testing.T) {
	t.Helper()

	propagationPolicy := metav1.DeletePropagationForeground
	tc.DeleteResource(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: "inferenceservices.serving.kserve.io"}),
		WithClientDeleteOptions(
			&client.DeleteOptions{
				PropagationPolicy: &propagationPolicy,
			}),
		WithWaitForDeletion(true),
		WithIgnoreNotFound(true),
	)
}

// ValidateTrustyAICondition validates the readiness condition of TrustyAI and DataScienceCluster.
func (tc *TrustyAITestCtx) ValidateTrustyAICondition(expectedStatus metav1.ConditionStatus) {
	// Validate TrustyAI readiness.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.TrustyAI, types.NamespacedName{Name: componentApi.TrustyAIInstanceName}),
		WithCondition(
			And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, expectedStatus),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, expectedStatus),
			),
		),
		WithCustomErrorMsg("TrustyAI should have Ready and %s conditions set to %s", status.ConditionTypeProvisioningSucceeded, expectedStatus),
	)

	// Validate DataScienceCluster readiness.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, expectedStatus)),
		WithCustomErrorMsg("DataScienceCluster should have %sReady condition set to %s", tc.GVK.Kind, expectedStatus),
	)
}

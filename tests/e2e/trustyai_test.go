package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

// TrustyAITestCtx extends ComponentTestCtx with TrustyAI-specific test functionality.
// TrustyAI has unique dependency requirements (KServe CRDs) that need special handling.
type TrustyAITestCtx struct {
	*ComponentTestCtx
}

// trustyAITestSuite runs the complete TrustyAI component test suite.
// This includes dependency validation tests specific to TrustyAI's KServe requirements.
func trustyAITestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.TrustyAI{})
	require.NoError(t, err)

	componentCtx := TrustyAITestCtx{
		ComponentTestCtx: ct,
	}

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

// ValidateComponentEnabled validates TrustyAI component with required KServe dependency.
// Unlike other components, TrustyAI requires KServe CRDs to be present before it can start.
// This method ensures the dependency is satisfied before running standard component validation.
func (tc *TrustyAITestCtx) ValidateComponentEnabled(t *testing.T) {
	t.Helper()

	// TrustyAI requires Kserve CRDs, so enable Kserve first
	tc.setKserveState(operatorv1.Managed, true)

	// Call the parent component validation
	tc.ComponentTestCtx.ValidateComponentEnabled(t)
}

// ValidateComponentDisabled validates TrustyAI component removal and cleans up dependencies.
// Unlike other components, TrustyAI requires explicit KServe cleanup to prevent interference
// with other test suites that might not expect KServe to be enabled.
func (tc *TrustyAITestCtx) ValidateComponentDisabled(t *testing.T) {
	t.Helper()

	// Then clean up KServe dependency to avoid interference with other tests
	tc.setKserveState(operatorv1.Removed, false)

	// First disable TrustyAI using standard component validation
	tc.ComponentTestCtx.ValidateComponentDisabled(t)
}

// ValidateTrustyAIPreCheck validates TrustyAI's dependency validation and recovery mechanisms.
// This test verifies that TrustyAI properly detects missing KServe dependencies and automatically
// recovers when dependencies are restored. The test simulates real-world scenarios where
// dependencies might be removed or unavailable during cluster operations.
func (tc *TrustyAITestCtx) ValidateTrustyAIPreCheck(t *testing.T) {
	t.Helper()

	// Step 1: Disable KServe → TrustyAI should detect missing dependency
	tc.setKserveState(operatorv1.Removed, false)

	// Step 2: Delete InferenceServices CRD → Trigger validation error
	tc.deleteInferenceServicesCRD()

	// Step 3: Validate Error State → TrustyAI conditions should be False
	tc.validateTrustyAICondition(metav1.ConditionFalse)

	// Step 4: Enable KServe → Restore dependency
	tc.setKserveState(operatorv1.Managed, true)

	// Step 5: Validate Recovery → TrustyAI conditions should be True
	tc.validateTrustyAICondition(metav1.ConditionTrue)
}

// setKserveState manages KServe component lifecycle for TrustyAI dependency testing.
// Uses extended timeouts because KServe initialization involves complex CRD installation
// and feature gate configuration that can be slow in CI environments.
func (tc *TrustyAITestCtx) setKserveState(state operatorv1.ManagementState, shouldExist bool) {
	// TODO: remove timeout override once we understand why Kserve takes so long in CI
	reset := tc.OverrideEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout, tc.TestTimeouts.defaultEventuallyPollInterval)
	defer reset()

	tc.UpdateComponentStateInDataScienceClusterWithKind(state, gvk.Kserve.Kind)

	if shouldExist {
		tc.ValidateComponentCondition(
			gvk.Kserve,
			componentApi.KserveInstanceName,
			status.ConditionTypeReady,
		)
	} else {
		tc.DeleteResource(
			WithMinimalObject(gvk.Kserve, types.NamespacedName{Name: componentApi.KserveInstanceName}),
			WithIgnoreNotFound(true),
			WithRemoveFinalizersOnDelete(true),
			WithWaitForDeletion(true),
		)
	}
}

// deleteInferenceServicesCRD removes the InferenceServices CRD to simulate dependency failure.
// Uses foreground deletion to ensure all InferenceService instances are cleaned up
// before the CRD is removed, preventing orphaned resources.
func (tc *TrustyAITestCtx) deleteInferenceServicesCRD() {
	tc.DeleteResource(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: "inferenceservices.serving.kserve.io"}),
		WithForegroundDeletion(),
		WithWaitForDeletion(true),
		WithIgnoreNotFound(true),
	)
}

// ValidateTrustyAICondition verifies TrustyAI condition status in both component and cluster resources.
// Checks both TrustyAI component conditions and the corresponding DataScienceCluster status
// to ensure consistency between component state and cluster-wide reporting.
func (tc *TrustyAITestCtx) validateTrustyAICondition(expectedStatus metav1.ConditionStatus) {
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

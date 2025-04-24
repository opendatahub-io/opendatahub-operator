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
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateTrustyAIPreCheck defines the test cases for TrustyAI pre-check validation.
func (tc *TrustyAITestCtx) ValidateTrustyAIPreCheck(t *testing.T) {
	t.Helper()

	// Define test cases.
	testCases := []TestCase{
		{"Disable Kserve", tc.DisableKserve},
		{"Delete InferenceServices", tc.DeleteInferenceServices},
		{"Validate Error", func(t *testing.T) {
			t.Helper()
			tc.ValidateTrustyAICondition(metav1.ConditionFalse)
		}},
		{"Enable Kserve", tc.EnableKserve},
		{"Validate Recovery", func(t *testing.T) {
			t.Helper()
			tc.ValidateTrustyAICondition(metav1.ConditionTrue)
		}},
		{"Disable Kserve", tc.DisableKserve},
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
	reset := tc.OverrideEventuallyTimeout(eventuallyTimeoutLong, defaultEventuallyPollInterval)
	defer reset() // Ensure reset happens after test completes

	nn := types.NamespacedName{Name: componentApi.KserveInstanceName}

	// Update the Kserve component state in DataScienceCluster.
	tc.UpdateComponentStateInDataScienceCluster(state, gvk.Kserve.Kind)

	// Verify if Kserve should exist or be removed.
	if shouldExist {
		tc.ValidateComponentCondition(
			gvk.Kserve,
			componentApi.KserveInstanceName,
			status.ConditionTypeReady,
		)
	} else {
		tc.EnsureResourceGone(WithMinimalObject(gvk.Kserve, nn))
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
	)

	// Validate DataScienceCluster readiness.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, expectedStatus)),
	)
}

package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	aipipelinesModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aipipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type DataSciencePipelinesTestCtx struct {
	*ComponentTestCtx
}

func aiPipelinesTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewModuleTestCtx(t, gvk.AIPipelines, componentApi.AIPipelinesInstanceName)
	require.NoError(t, err)

	componentCtx := DataSciencePipelinesTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate component conditions", componentCtx.ValidateConditions},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate platform release", componentCtx.ValidatePlatformRelease},
		{"Validate argoWorkflowsControllers options", componentCtx.ValidateArgoWorkflowsControllersOptions},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateUpdateDeploymentResources verifies that the AI Pipelines module
// controller Deployment accepts resource updates.
func (tc *DataSciencePipelinesTestCtx) ValidateUpdateDeploymentResources(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)

	deployment := tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Namespace: tc.AppsNamespace,
			Name:      aipipelinesModule.ControllerDeploymentName,
		}),
	)
	tc.validateUpdateDeploymentsResources(t, *deployment)
}

// ValidateConditions validates that the AIPipelines module is ready.
func (tc *DataSciencePipelinesTestCtx) ValidateConditions(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)

	tc.ValidateComponentCondition(
		gvk.AIPipelines,
		componentApi.AIPipelinesInstanceName,
		status.ConditionTypeReady,
	)
}

// ValidateOperandsOwnerReferences verifies that the module operator Deployment
// is owned by the Platform CR that renders it, rather than by the module CR.
func (tc *DataSciencePipelinesTestCtx) ValidateOperandsOwnerReferences(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Namespace: tc.AppsNamespace,
			Name:      aipipelinesModule.ControllerDeploymentName,
		}),
		WithCondition(jq.Match(`.metadata.ownerReferences[0].kind == "Platform"`)),
		WithCustomErrorMsg("AI Pipelines module operator Deployment should be owned by Platform"),
	)
}

// ValidateArgoWorkflowsControllersOptionsV2 ensures the DataSciencePipelines component is ready if the
// argoWorkflowsControllersSpec options are set to "Removed" when using v2 API (aipipelines field).
func (tc *DataSciencePipelinesTestCtx) ValidateArgoWorkflowsControllersOptions(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.aipipelines.argoWorkflowsControllers.managementState = "%s"`, operatorv1.Removed)),
		WithCondition(
			And(
				// Verify v2 condition type exists
				jq.Match(`.status.conditions[] | select(.type == "AIPipelinesReady") | .status == "True"`),
				// Verify v1 condition type does NOT exist
				jq.Match(`[.status.conditions[] | select(.type == "DataSciencePipelinesReady")] | length == 0`),
			),
		),
	)
}

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/components/modelcontroller"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

type ModelMeshServingTestCtx struct {
	*ComponentTestCtx
}

func modelMeshServingTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.ModelMeshServing{})
	require.NoError(t, err)

	componentCtx := ModelMeshServingTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate model controller", componentCtx.ValidateModelControllerInstance},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	componentCtx.RunTestCases(t, testCases)
}

// ValidateModelControllerInstance validates the existence and correct status of the ModelController and DataScienceCluster.
func (tc *ModelMeshServingTestCtx) ValidateModelControllerInstance(t *testing.T) {
	t.Helper()

	// Ensure ModelController resource exists with the expected owner references and status phase.
	tc.EnsureResourceExistsAndMatchesCondition(
		gvk.ModelController,
		types.NamespacedName{Name: componentApi.ModelControllerInstanceName},
		And(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
			jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady),
		),
	)

	// Ensure DataScienceCluster resource exists with the expected condition status.
	tc.EnsureResourceExistsAndMatchesCondition(
		gvk.DataScienceCluster,
		tc.DataScienceClusterNamespacedName,
		jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, modelcontroller.ReadyConditionType, metav1.ConditionTrue),
	)
}

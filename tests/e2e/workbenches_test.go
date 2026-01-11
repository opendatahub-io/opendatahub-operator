package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

type WorkbenchesTestCtx struct {
	*ComponentTestCtx
}

func workbenchesTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.Workbenches{})
	require.NoError(t, err)

	componentCtx := WorkbenchesTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate workbenches namespace configuration", componentCtx.ValidateWorkbenchesNamespaceConfiguration},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

func (tc *WorkbenchesTestCtx) ValidateWorkbenchesNamespaceConfiguration(t *testing.T) {
	t.Helper()

	// ensure the workbenches namespace exists and has the expected label
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: tc.WorkbenchesNamespace}),
		WithCondition(jq.Match(`.metadata.labels["%s"] == "true"`, labels.ODH.OwnedNamespace)),
	)

	// ensure the DataScienceCluster has the expected workbench namespace
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.spec.components.workbenches.workbenchNamespace == "%s"`, tc.WorkbenchesNamespace)),
	)

	// ensure the Workbenches CR instance has the expected workbench namespace in both spec and status
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Workbenches, types.NamespacedName{Name: componentApi.WorkbenchesInstanceName}),
		WithCondition(
			And(
				jq.Match(`.spec.workbenchNamespace == "%s"`, tc.WorkbenchesNamespace),
				jq.Match(`.status.workbenchNamespace == "%s"`, tc.WorkbenchesNamespace),
			),
		),
	)
}

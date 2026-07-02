package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

type MLflowOperatorTestCtx struct {
	*ComponentTestCtx
}

const (
	mlflowValidateEnabledName  = "Validate component enabled"
	mlflowValidateDisabledName = "Validate component disabled"
)

func mlflowOperatorTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.MLflowOperator{})
	require.NoError(t, err)

	componentCtx := MLflowOperatorTestCtx{
		ComponentTestCtx: ct,
	}

	testCases := []TestCase{
		{mlflowValidateEnabledName, componentCtx.ValidateModuleEnabled},
		{mlflowValidateDisabledName, componentCtx.ValidateModuleDisabled},
	}

	RunTestCases(t, testCases)
}

func (tc *MLflowOperatorTestCtx) ValidateModuleEnabled(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	if !tc.IsXKS() {
		tc.UpdateComponentStateInDataScienceCluster(operatorv1.Removed)
	}

	tc.UpdateComponentState(operatorv1.Managed)

	tc.EnsureResourcesExist(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithCondition(
			And(
				HaveLen(1),
				HaveEach(And(
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
					jq.Match(`.spec.gatewayName != ""`),
					jq.Match(`.spec.sectionTitle != ""`),
				)),
			),
		),
	)
}

func (tc *MLflowOperatorTestCtx) ValidateModuleDisabled(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	tc.EnsureResourcesExist(WithMinimalObject(tc.GVK, tc.NamespacedName))
	tc.UpdateComponentState(operatorv1.Removed)
	tc.EnsureResourcesGone(WithMinimalObject(tc.GVK, tc.NamespacedName))
}

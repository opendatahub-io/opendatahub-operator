package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

func trustyAITestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(&componentApi.TrustyAI{})
	require.NoError(t, err)

	componentCtx := TrustyAITestCtx{
		ComponentTestCtx: ct,
	}

	// TrustyAI requires some CRDs that are shipped by Kserve
	t.Run("Enable Kserve", componentCtx.enableKserve)

	t.Run("Validate component enabled", componentCtx.ValidateComponentEnabled)
	t.Run("Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences)
	t.Run("Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources)
	t.Run("Validate component disabled", componentCtx.ValidateComponentDisabled)
	t.Run("Disable Kserve", componentCtx.disableKserve)
}

type TrustyAITestCtx struct {
	*ComponentTestCtx
}

func (c *TrustyAITestCtx) enableKserve(t *testing.T) {
	g := c.NewWithT(t)

	g.Update(
		gvk.DataScienceCluster,
		c.DSCName,
		testf.Transform(`.spec.components.%s.managementState = "%s"`, componentApi.KserveComponentName, operatorv1.Managed),
	).Eventually().Should(
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentApi.KserveComponentName, operatorv1.Managed),
	)

	g.List(gvk.Kserve).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionTrue),
		)),
	))
}

func (c *TrustyAITestCtx) disableKserve(t *testing.T) {
	g := c.NewWithT(t)

	g.Update(
		gvk.DataScienceCluster,
		c.DSCName,
		testf.Transform(`.spec.components.%s.managementState = "%s"`, componentApi.KserveComponentName, operatorv1.Removed),
	).Eventually().Should(
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentApi.KserveComponentName, operatorv1.Removed),
	)

	g.List(gvk.Kserve).Eventually().Should(
		BeEmpty(),
	)
}

package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
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
	t.Run("Validate component releases", componentCtx.ValidateComponentReleases)
	t.Run("Validate pre check", componentCtx.validateTrustyAIPreCheck)
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
		Succeed(),
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
		Succeed(),
	)

	g.List(gvk.Kserve).Eventually().Should(
		BeEmpty(),
	)
}

func (c *TrustyAITestCtx) validateTrustyAIPreCheck(t *testing.T) {
	t.Run("Disable Kserve", c.disableKserve)
	t.Run("Delete InferenceServices", func(t *testing.T) {
		g := c.NewWithT(t)
		n := types.NamespacedName{Name: "inferenceservices.serving.kserve.io"}

		g.Delete(gvk.CustomResourceDefinition, n, client.PropagationPolicy(metav1.DeletePropagationForeground)).Eventually().Should(
			Succeed(),
		)
	})

	t.Run("Validate Error", func(t *testing.T) {
		g := c.NewWithT(t)

		g.List(gvk.TrustyAI).Eventually().Should(And(
			HaveLen(1),
			HaveEach(And(
				jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "ProvisioningSucceeded") | .status == "%s"`, metav1.ConditionFalse),
			)),
		))

		g.List(gvk.DataScienceCluster).Eventually().Should(And(
			HaveLen(1),
			HaveEach(
				jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, c.GVK.Kind, metav1.ConditionFalse),
			),
		))
	})

	t.Run("Enable Kserve", c.enableKserve)

	t.Run("Validate Recovery", func(t *testing.T) {
		g := c.NewWithT(t)

		g.List(gvk.TrustyAI).Eventually().Should(And(
			HaveLen(1),
			HaveEach(And(
				jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionTrue),
				jq.Match(`.status.conditions[] | select(.type == "ProvisioningSucceeded") | .status == "%s"`, metav1.ConditionTrue),
			)),
		))

		g.List(gvk.DataScienceCluster).Eventually().Should(And(
			HaveLen(1),
			HaveEach(
				jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, c.GVK.Kind, metav1.ConditionTrue),
			),
		))
	})
}

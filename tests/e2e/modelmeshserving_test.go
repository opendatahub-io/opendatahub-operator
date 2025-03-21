package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelcontroller"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func modelMeshServingTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(&componentApi.ModelMeshServing{})
	require.NoError(t, err)

	componentCtx := ModelMeshServingTestCtx{
		ComponentTestCtx: ct,
	}

	t.Run("Validate component enabled", componentCtx.ValidateComponentEnabled)
	t.Run("Validate model controller", componentCtx.validateModelControllerInstance)
	t.Run("Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences)
	t.Run("Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources)
	t.Run("Validate component disabled", componentCtx.ValidateComponentDisabled)
	// t.Run("Validate component releases", componentCtx.ValidateComponentReleases)
}

type ModelMeshServingTestCtx struct {
	*ComponentTestCtx
}

func (tc *ModelMeshServingTestCtx) validateModelControllerInstance(t *testing.T) {
	g := tc.NewWithT(t)

	g.List(gvk.ModelController).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
			jq.Match(`.status.phase == "%s"`, readyStatus),
		)),
	))

	g.List(gvk.DataScienceCluster).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, modelcontroller.ReadyConditionType, metav1.ConditionTrue),
		)),
	))
}

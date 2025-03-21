package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func dataSciencePipelinesTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(&componentApi.DataSciencePipelines{})
	require.NoError(t, err)

	componentCtx := DataSciencePipelinesTestCtx{
		ComponentTestCtx: ct,
	}

	t.Run("Validate component enabled", componentCtx.ValidateComponentEnabled)
	t.Run("Validate component conditions", componentCtx.validateConditions)
	t.Run("Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences)
	t.Run("Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources)
	t.Run("Validate component releases", componentCtx.ValidateComponentReleases)
	t.Run("Validate component disabled", componentCtx.ValidateComponentDisabled)
}

type DataSciencePipelinesTestCtx struct {
	*ComponentTestCtx
}

func (c *DataSciencePipelinesTestCtx) validateConditions(t *testing.T) {
	g := c.NewWithT(t)

	g.List(gvk.DataSciencePipelines).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionArgoWorkflowAvailable, metav1.ConditionTrue),
		)),
	))
}

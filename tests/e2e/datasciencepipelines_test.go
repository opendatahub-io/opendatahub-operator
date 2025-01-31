package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

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
	t.Run("Validate component managed pipelines", componentCtx.validateManagedPipelines)
	t.Run("Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences)
	t.Run("Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources)
	t.Run("Validate component disabled", componentCtx.ValidateComponentDisabled)
}

type DataSciencePipelinesTestCtx struct {
	*ComponentTestCtx
}

func (c *DataSciencePipelinesTestCtx) validateManagedPipelines(t *testing.T) {
	t.Run("instructLab", c.validateInstructLabPipelines)
}

func (c *DataSciencePipelinesTestCtx) validateInstructLabPipelines(t *testing.T) {
	pipelineType := "instructLab"

	states := []operatorv1.ManagementState{
		operatorv1.Managed,
		operatorv1.Removed,
	}

	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			g := c.NewWithT(t)

			g.Update(
				gvk.DataScienceCluster,
				c.DSCName,
				testf.TransformPipeline(
					testf.Transform(
						`.spec.components.%s.managedPipelines.%s.state = "%s"`,
						componentApi.DataSciencePipelinesComponentName,
						pipelineType,
						state),
				),
			).Eventually().Should(
				jq.Match(
					`.spec.components.%s.managedPipelines.%s.state == "%s"`,
					componentApi.DataSciencePipelinesComponentName,
					pipelineType,
					state),
			)

			g.List(gvk.DataSciencePipelines).Eventually().Should(And(
				HaveLen(1),
				HaveEach(And(
					jq.Match(
						`.spec.managedPipelines.%s.state == "%s"`,
						pipelineType,
						state),
				)),
			))
		})
	}
}

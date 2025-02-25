package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
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
	t.Run("Validate component spec", componentCtx.validateSpec)
	t.Run("Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences)
	t.Run("Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources)
	t.Run("Validate component disabled", componentCtx.ValidateComponentDisabled)
	t.Run("Validate component releases", componentCtx.ValidateComponentReleases)
}

type DataSciencePipelinesTestCtx struct {
	*ComponentTestCtx
}

func (d *DataSciencePipelinesTestCtx) validateSpec(t *testing.T) {
	g := d.NewWithT(t)

	dsc, err := d.GetDSC()
	g.Expect(err).NotTo(HaveOccurred())

	g.List(gvk.DataSciencePipelines).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.spec.modelRegistryNamespace == "%s"`, dsc.Spec.Components.ModelRegistry.RegistriesNamespace),
		)),
	))
}

package e2e_test

import (
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelcontroller"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

func modelControllerTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(&componentApi.ModelController{})
	require.NoError(t, err)

	componentCtx := ModelControllerTestCtx{
		ComponentTestCtx: ct,
	}

	t.Run("Validate component enabled", componentCtx.validateComponentEnabled)
	t.Run("Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences)
	t.Run("Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources)
	t.Run("Validate component disabled", componentCtx.validateComponentDisabled)
}

type ModelControllerTestCtx struct {
	*ComponentTestCtx
}

func (c *ModelControllerTestCtx) validateComponentEnabled(t *testing.T) {
	t.Run("ModelMeshServing enabled", func(t *testing.T) {
		c.validateComponentDeployed(t, operatorv1.Managed, operatorv1.Removed, operatorv1.Removed, metav1.ConditionTrue)
	})
	t.Run("Kserve enabled", func(t *testing.T) {
		c.validateComponentDeployed(t, operatorv1.Removed, operatorv1.Managed, operatorv1.Removed, metav1.ConditionTrue)
	})
	t.Run("Kserve and ModelMeshServing enabled", func(t *testing.T) {
		c.validateComponentDeployed(t, operatorv1.Managed, operatorv1.Managed, operatorv1.Removed, metav1.ConditionTrue)
	})
	t.Run("ModelRegistry enabled", func(t *testing.T) {
		c.validateComponentDeployed(t, operatorv1.Managed, operatorv1.Managed, operatorv1.Managed, metav1.ConditionTrue)
	})
}

func (c *ModelControllerTestCtx) validateComponentDisabled(t *testing.T) {
	t.Run("Kserve and ModelMeshServing disabled", func(t *testing.T) {
		c.validateComponentDeployed(t, operatorv1.Removed, operatorv1.Removed, operatorv1.Removed, metav1.ConditionFalse)
	})
}

func (c *ModelControllerTestCtx) validateComponentDeployed(
	t *testing.T,
	modelMeshState operatorv1.ManagementState,
	kserveState operatorv1.ManagementState,
	modelRegistryState operatorv1.ManagementState,
	status metav1.ConditionStatus,
) {
	t.Helper()

	g := c.NewWithT(t)

	g.Update(
		gvk.DataScienceCluster,
		c.DSCName,
		testf.TransformPipeline(
			testf.Transform(`.spec.components.%s.managementState = "%s"`, componentApi.ModelMeshServingComponentName, modelMeshState),
			testf.Transform(`.spec.components.%s.managementState = "%s"`, componentApi.KserveComponentName, kserveState),
			testf.Transform(`.spec.components.%s.managementState = "%s"`, componentApi.ModelRegistryComponentName, modelRegistryState),
		),
	).Eventually().WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(
		Succeed(),
	)

	if status == metav1.ConditionTrue {
		g.List(gvk.ModelController).Eventually().Should(And(
			HaveLen(1),
			HaveEach(And(
				jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
				jq.Match(`.status.phase == "%s"`, readyStatus),
			)),
		))

		g.List(
			gvk.Deployment,
			client.InNamespace(c.ApplicationNamespace),
			client.MatchingLabels{
				labels.PlatformPartOf: strings.ToLower(c.GVK.Kind),
			},
		).Eventually().ShouldNot(
			BeEmpty(),
		)
	} else {
		g.List(gvk.Kserve).Eventually().Should(
			BeEmpty(),
		)
		g.List(gvk.ModelMeshServing).Eventually().Should(
			BeEmpty(),
		)
		g.List(gvk.ModelController).Eventually().Should(
			BeEmpty(),
		)

		g.List(
			gvk.Deployment,
			client.InNamespace(c.ApplicationNamespace),
			client.MatchingLabels{
				labels.PlatformPartOf: strings.ToLower(gvk.ModelController.Kind),
			},
		).Eventually().Should(
			BeEmpty(),
		)
	}

	g.List(gvk.DataScienceCluster).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, modelcontroller.ReadyConditionType, status),
		)),
	))
}

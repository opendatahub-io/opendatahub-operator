package e2e_test

import (
	"context"
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	t.Run("Validate CRDs reinstated", componentCtx.validateCRDReinstated)
	t.Run("Validate component disabled", componentCtx.ValidateComponentDisabled)
	t.Run("Validate pre check", componentCtx.validateTrustyAIPreCheck)
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

func (c *TrustyAITestCtx) validateCRDReinstated(t *testing.T) {
	crds := []string{
		"inferenceservices.serving.kserve.io", // SR is not needed any more in 2.18.0 by TrustyAI
	}

	for _, crd := range crds {
		t.Run(crd, func(t *testing.T) {
			c.ValidateCRDReinstated(t, crd)
		})
	}
}

func (c *TrustyAITestCtx) validateTrustyAIPreCheck(t *testing.T) {
	// validate precheck on CRD version:
	// pre-req: skip trusty to removed (done by ValidateComponentDisabled)
	// step: delete isvc left from enabled Kserve, wait till it is gone
	// set trustyai to managed, result to error, enable mm, result to success

	g := c.NewWithT(t)
	g.Delete(gvk.CustomResourceDefinition,
		types.NamespacedName{Name: "inferenceservices.serving.kserve.io"},
		client.PropagationPolicy(metav1.DeletePropagationForeground),
	).Eventually().Should(
		Succeed(),
	)
	g.Eventually(func() bool {
		var crd apiextensionsv1.CustomResourceDefinition
		err := c.Client().Get(context.Background(), types.NamespacedName{Name: "inferenceservices.serving.kserve.io"}, &crd)
		return k8serr.IsNotFound(err)
	}).Should(BeTrue())

	g.Update(
		gvk.DataScienceCluster,
		c.DSCName,
		testf.Transform(`.spec.components.%s.managementState = "%s"`, strings.ToLower(c.GVK.Kind), operatorv1.Managed),
	).Eventually().Should(
		jq.Match(`.spec.components.%s.managementState == "%s"`, strings.ToLower(c.GVK.Kind), operatorv1.Managed),
	)

	g.List(gvk.DataScienceCluster).Eventually().Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, c.GVK.Kind, metav1.ConditionFalse),
		),
	))

	g.Update(
		gvk.DataScienceCluster,
		c.DSCName,
		testf.Transform(`.spec.components.%s.managementState = "%s"`, strings.ToLower(componentApi.ModelMeshServingComponentName), operatorv1.Managed),
	).Eventually().Should(
		jq.Match(`.spec.components.%s.managementState == "%s"`, strings.ToLower(componentApi.ModelMeshServingComponentName), operatorv1.Managed),
	)

	g.List(gvk.DataScienceCluster).Eventually().Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, c.GVK.Kind, metav1.ConditionTrue),
		),
	))
}

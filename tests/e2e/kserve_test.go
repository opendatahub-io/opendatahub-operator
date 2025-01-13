package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/components/modelcontroller"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/serverless"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func kserveTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(&componentApi.Kserve{})
	require.NoError(t, err)

	componentCtx := KserveTestCtx{
		ComponentTestCtx: ct,
	}

	t.Run("Validate component enabled", componentCtx.ValidateComponentEnabled)
	t.Run("Validate component spec", componentCtx.validateSpec)
	t.Run("Validate model controller", componentCtx.validateModelControllerInstance)
	t.Run("Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences)
	t.Run("Validate default certs", componentCtx.validateDefaultCertsAvailable)
	t.Run("Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources)
	t.Run("Validate component disabled", componentCtx.ValidateComponentDisabled)
}

type KserveTestCtx struct {
	*ComponentTestCtx
}

func (c *KserveTestCtx) validateSpec(t *testing.T) {
	g := c.NewWithT(t)

	dsc, err := c.GetDSC()
	g.Expect(err).NotTo(HaveOccurred())

	g.List(gvk.Kserve).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.spec.defaultDeploymentMode == "%s"`, dsc.Spec.Components.Kserve.DefaultDeploymentMode),
			jq.Match(`.spec.nim.managementState == "%s"`, dsc.Spec.Components.Kserve.NIM.ManagementState),
			jq.Match(`.spec.serving.managementState == "%s"`, dsc.Spec.Components.Kserve.Serving.ManagementState),
			jq.Match(`.spec.serving.name == "%s"`, dsc.Spec.Components.Kserve.Serving.Name),
			jq.Match(`.spec.serving.ingressGateway.certificate.type == "%s"`, dsc.Spec.Components.Kserve.Serving.IngressGateway.Certificate.Type),
		)),
	))
}

func (c *KserveTestCtx) validateModelControllerInstance(t *testing.T) {
	g := c.NewWithT(t)

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

func (c *KserveTestCtx) validateDefaultCertsAvailable(t *testing.T) {
	g := c.NewWithT(t)

	defaultIngressSecret, err := cluster.FindDefaultIngressSecret(g.Context(), g.Client())
	g.Expect(err).ToNot(HaveOccurred())

	dsc, err := c.GetDSC()
	g.Expect(err).ToNot(HaveOccurred())

	dsci, err := c.GetDSCI()
	g.Expect(err).ToNot(HaveOccurred())

	defaultSecretName := dsc.Spec.Components.Kserve.Serving.IngressGateway.Certificate.SecretName
	if defaultSecretName == "" {
		defaultSecretName = serverless.DefaultCertificateSecretName
	}

	ctrlPlaneSecret, err := cluster.GetSecret(g.Context(), g.Client(), dsci.Spec.ServiceMesh.ControlPlane.Namespace, defaultSecretName)
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(ctrlPlaneSecret.Type).Should(Equal(defaultIngressSecret.Type))
	g.Expect(defaultIngressSecret.Data).Should(Equal(ctrlPlaneSecret.Data))
}

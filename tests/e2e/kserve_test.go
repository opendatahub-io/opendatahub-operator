package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

	t.Run("Validate environment", componentCtx.validateEnv)
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

// validateEnv remove leftovers eventually present in the cluster. For some reason, the
// KnativeServing may be left on the cluster, which causes the KServe tests to hang till
// the test suite timeout (25m).
func (c *KserveTestCtx) validateEnv(t *testing.T) {
	g := c.NewWithT(t)
	ns := "knative-serving"

	kss, err := g.List(gvk.KnativeServing, client.InNamespace(ns)).Get()
	g.Expect(err).NotTo(HaveOccurred())

	if len(kss) != 0 {
		t.Logf("Detected %d Knative Serving objects in namespace %s", len(kss), ns)
	}

	for _, ks := range kss {
		t.Logf("Deleting Knative Serving %s in namespace %s", ks.GetName(), ks.GetNamespace())

		g.Delete(gvk.KnativeServing, client.ObjectKeyFromObject(&ks)).Eventually().Should(Or(
			MatchError(k8serr.IsNotFound, "IsNotFound"),
			Not(HaveOccurred()),
		))
	}
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

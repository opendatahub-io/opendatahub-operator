package e2e_test

import (
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

type ModelRegistryTestCtx struct {
	*testContext
}

func modelRegistryTestSuite(t *testing.T) {
	tc, err := NewTestContext()
	require.NoError(t, err)

	componentCtx := ModelRegistryTestCtx{
		testContext: tc,
	}

	t.Run(componentCtx.testDsc.Name, func(t *testing.T) {
		t.Run("Validate ModelRegistry instance", componentCtx.validateModelRegistryInstance)
		t.Run("Validate ModelRegistry operands OwnerReferences", componentCtx.validateOperandsOwnerReferences)
		t.Run("Validate Update ModelRegistry operands resources", componentCtx.validateUpdateModelRegistryOperandsResources)
		t.Run("Validate ModelRegistry Cert", componentCtx.validateModelRegistryCert)
		t.Run("Validate ModelRegistry ServiceMeshMember", componentCtx.validateModelRegistryServiceMeshMember)
		// must be the latest one
		t.Run("Validate Disabling ModelRegistry Component", componentCtx.validateModelRegistryDisabled)
	})
}

func (tc *ModelRegistryTestCtx) validateModelRegistryInstance(t *testing.T) {
	g := NewWithT(t)
	g.SetDefaultEventuallyTimeout(generalWaitTimeout)
	g.SetDefaultEventuallyPollingInterval(1 * time.Second)

	g.Eventually(
		tc.List(gvk.ModelRegistry),
	).Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
			jq.Match(`.spec.registriesNamespace == "%s"`, tc.testDsc.Spec.Components.ModelRegistry.RegistriesNamespace),
			jq.Match(`.status.phase == "%s"`, readyStatus),
		)),
	))
}

func (tc *ModelRegistryTestCtx) validateOperandsOwnerReferences(t *testing.T) {
	g := NewWithT(t)
	g.SetDefaultEventuallyTimeout(generalWaitTimeout)
	g.SetDefaultEventuallyPollingInterval(generalRetryInterval)

	g.Eventually(
		tc.List(
			gvk.Deployment,
			client.InNamespace(tc.applicationsNamespace),
			client.MatchingLabels{labels.ComponentManagedBy: componentsv1.ModelRegistryInstanceName},
		),
	).Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, componentsv1.ModelRegistryKind),
		),
	))
}

func (tc *ModelRegistryTestCtx) validateUpdateModelRegistryOperandsResources(t *testing.T) {
	g := NewWithT(t)
	g.SetDefaultEventuallyTimeout(generalWaitTimeout)
	g.SetDefaultEventuallyPollingInterval(generalRetryInterval)

	appDeployments, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(
		tc.ctx,
		metav1.ListOptions{
			LabelSelector: labels.ComponentManagedBy + "=" + componentsv1.ModelRegistryInstanceName,
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(appDeployments.Items).To(HaveLen(1))

	const expectedReplica int32 = 2 // from 1 to 2

	testDeployment := appDeployments.Items[0]
	patchedReplica := &autoscalingv1.Scale{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDeployment.Name,
			Namespace: testDeployment.Namespace,
		},
		Spec: autoscalingv1.ScaleSpec{
			Replicas: expectedReplica,
		},
		Status: autoscalingv1.ScaleStatus{},
	}

	updatedDep, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).UpdateScale(
		tc.ctx,
		testDeployment.Name,
		patchedReplica,
		metav1.UpdateOptions{},
	)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(updatedDep.Spec.Replicas).Should(Equal(patchedReplica.Spec.Replicas))

	g.Eventually(
		tc.List(
			gvk.Deployment,
			client.InNamespace(tc.applicationsNamespace),
			client.MatchingLabels{labels.ComponentManagedBy: componentsv1.ModelRegistryInstanceName},
		),
	).Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.spec.replicas == %d`, expectedReplica),
		),
	))

	g.Consistently(
		tc.List(
			gvk.Deployment,
			client.InNamespace(tc.applicationsNamespace),
			client.MatchingLabels{labels.ComponentManagedBy: componentsv1.ModelRegistryInstanceName},
		),
	).WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.spec.replicas == %d`, expectedReplica),
		),
	))
}

func (tc *ModelRegistryTestCtx) validateModelRegistryCert(t *testing.T) {
	g := NewWithT(t)
	g.SetDefaultEventuallyTimeout(generalWaitTimeout)
	g.SetDefaultEventuallyPollingInterval(generalRetryInterval)

	is, err := cluster.FindDefaultIngressSecret(tc.ctx, tc.customClient)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Eventually(
		tc.Get(
			gvk.Secret,
			tc.testDSCI.Spec.ServiceMesh.ControlPlane.Namespace,
			modelregistry.DefaultModelRegistryCert,
		),
	).Should(And(
		jq.Match(`.type == "%s"`, is.Type),
		jq.Match(`(.data."tls.crt" | @base64d) == "%s"`, is.Data["tls.crt"]),
		jq.Match(`(.data."tls.key" | @base64d) == "%s"`, is.Data["tls.key"]),
	))
}

func (tc *ModelRegistryTestCtx) validateModelRegistryServiceMeshMember(t *testing.T) {
	g := NewWithT(t)
	g.SetDefaultEventuallyTimeout(generalWaitTimeout)
	g.SetDefaultEventuallyPollingInterval(generalRetryInterval)

	g.Eventually(
		tc.Get(gvk.ServiceMeshMember, modelregistry.DefaultModelRegistriesNamespace, "default"),
	).Should(
		jq.Match(`.spec | has("controlPlaneRef")`),
	)
}

func (tc *ModelRegistryTestCtx) validateModelRegistryDisabled(t *testing.T) {
	g := NewWithT(t)
	g.SetDefaultEventuallyTimeout(generalWaitTimeout)
	g.SetDefaultEventuallyPollingInterval(generalRetryInterval)

	g.Eventually(
		tc.List(
			gvk.Deployment,
			client.InNamespace(tc.applicationsNamespace),
			client.MatchingLabels{labels.ComponentManagedBy: componentsv1.ModelRegistryInstanceName},
		),
	).Should(
		HaveLen(1),
	)

	g.Eventually(
		tc.updateComponent(func(c *dscv1.Components) {
			c.ModelRegistry.ManagementState = operatorv1.Removed
		}),
	).ShouldNot(
		HaveOccurred(),
	)

	g.Eventually(
		tc.List(
			gvk.Deployment,
			client.InNamespace(tc.applicationsNamespace),
			client.MatchingLabels{labels.ComponentManagedBy: componentsv1.ModelRegistryInstanceName},
		),
	).Should(
		BeEmpty(),
	)

	g.Eventually(
		tc.List(gvk.ModelRegistry),
	).Should(
		BeEmpty(),
	)
}

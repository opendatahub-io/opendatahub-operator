package e2e_test

import (
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

type ModelRegistryTestCtx struct {
	*testContext
}

func (mr *ModelRegistryTestCtx) WithT(t *testing.T) *WithT {
	t.Helper()

	g := NewWithT(t)
	g.SetDefaultEventuallyTimeout(generalWaitTimeout)
	g.SetDefaultEventuallyPollingInterval(1 * time.Second)

	return g
}

func (mr *ModelRegistryTestCtx) List(
	gvk schema.GroupVersionKind,
	option ...client.ListOption,
) func() ([]unstructured.Unstructured, error) {
	return func() ([]unstructured.Unstructured, error) {
		items := unstructured.UnstructuredList{}
		items.SetGroupVersionKind(gvk)

		err := mr.customClient.List(mr.ctx, &items, option...)
		if err != nil {
			return nil, err
		}

		return items.Items, nil
	}
}

func (mr *ModelRegistryTestCtx) Get(
	gvk schema.GroupVersionKind,
	ns string,
	name string,
	option ...client.GetOption,
) func() (*unstructured.Unstructured, error) {
	return func() (*unstructured.Unstructured, error) {
		u := unstructured.Unstructured{}
		u.SetGroupVersionKind(gvk)

		err := mr.customClient.Get(mr.ctx, client.ObjectKey{Namespace: ns, Name: name}, &u, option...)
		if err != nil {
			return nil, err
		}

		return &u, nil
	}
}
func (mr *ModelRegistryTestCtx) MergePatch(
	obj client.Object,
	patch []byte,
) func() (*unstructured.Unstructured, error) {
	return func() (*unstructured.Unstructured, error) {
		u, err := resources.ToUnstructured(obj)
		if err != nil {
			return nil, err
		}

		err = mr.customClient.Patch(mr.ctx, u, client.RawPatch(types.MergePatchType, patch))
		if err != nil {
			return nil, err
		}

		return u, nil
	}
}

func (mr *ModelRegistryTestCtx) updateComponent(fn func(dsc *dscv1.Components)) func() error {
	return func() error {
		err := mr.customClient.Get(mr.ctx, types.NamespacedName{Name: mr.testDsc.Name}, mr.testDsc)
		if err != nil {
			return err
		}

		fn(&mr.testDsc.Spec.Components)

		err = mr.customClient.Update(mr.ctx, mr.testDsc)
		if err != nil {
			return err
		}

		return nil
	}
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

func (mr *ModelRegistryTestCtx) validateModelRegistryInstance(t *testing.T) {
	g := mr.WithT(t)

	g.Eventually(
		mr.List(gvk.ModelRegistry),
	).Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
			jq.Match(`.spec.registriesNamespace == "%s"`, mr.testDsc.Spec.Components.ModelRegistry.RegistriesNamespace),
			jq.Match(`.status.phase == "%s"`, readyStatus),
		)),
	))
}

func (mr *ModelRegistryTestCtx) validateOperandsOwnerReferences(t *testing.T) {
	g := mr.WithT(t)

	g.Eventually(
		mr.List(
			gvk.Deployment,
			client.InNamespace(mr.applicationsNamespace),
			client.MatchingLabels{labels.ComponentPartOf: componentsv1.ModelRegistryInstanceName},
		),
	).Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, componentsv1.ModelRegistryKind),
		),
	))
}

func (mr *ModelRegistryTestCtx) validateUpdateModelRegistryOperandsResources(t *testing.T) {
	g := mr.WithT(t)

	appDeployments, err := mr.kubeClient.AppsV1().Deployments(mr.applicationsNamespace).List(
		mr.ctx,
		metav1.ListOptions{
			LabelSelector: labels.ComponentPartOf + "=" + componentsv1.ModelRegistryInstanceName,
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

	updatedDep, err := mr.kubeClient.AppsV1().Deployments(mr.applicationsNamespace).UpdateScale(
		mr.ctx,
		testDeployment.Name,
		patchedReplica,
		metav1.UpdateOptions{},
	)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(updatedDep.Spec.Replicas).Should(Equal(patchedReplica.Spec.Replicas))

	g.Eventually(
		mr.List(
			gvk.Deployment,
			client.InNamespace(mr.applicationsNamespace),
			client.MatchingLabels{labels.ComponentPartOf: componentsv1.ModelRegistryInstanceName},
		),
	).Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.spec.replicas == %d`, expectedReplica),
		),
	))

	g.Consistently(
		mr.List(
			gvk.Deployment,
			client.InNamespace(mr.applicationsNamespace),
			client.MatchingLabels{labels.ComponentPartOf: componentsv1.ModelRegistryInstanceName},
		),
	).WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.spec.replicas == %d`, expectedReplica),
		),
	))
}

func (mr *ModelRegistryTestCtx) validateModelRegistryCert(t *testing.T) {
	g := mr.WithT(t)

	is, err := cluster.FindDefaultIngressSecret(mr.ctx, mr.customClient)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Eventually(
		mr.Get(
			gvk.Secret,
			mr.testDSCI.Spec.ServiceMesh.ControlPlane.Namespace,
			modelregistry.DefaultModelRegistryCert,
		),
	).Should(And(
		jq.Match(`.type == "%s"`, is.Type),
		jq.Match(`(.data."tls.crt" | @base64d) == "%s"`, is.Data["tls.crt"]),
		jq.Match(`(.data."tls.key" | @base64d) == "%s"`, is.Data["tls.key"]),
	))
}

func (mr *ModelRegistryTestCtx) validateModelRegistryServiceMeshMember(t *testing.T) {
	g := mr.WithT(t)

	g.Eventually(
		mr.Get(gvk.ServiceMeshMember, modelregistry.DefaultModelRegistriesNamespace, "default"),
	).Should(
		jq.Match(`.spec | has("controlPlaneRef")`),
	)
}

func (mr *ModelRegistryTestCtx) validateModelRegistryDisabled(t *testing.T) {
	g := mr.WithT(t)

	g.Eventually(
		mr.List(
			gvk.Deployment,
			client.InNamespace(mr.applicationsNamespace),
			client.MatchingLabels{labels.ComponentPartOf: componentsv1.ModelRegistryInstanceName},
		),
	).Should(
		HaveLen(1),
	)

	g.Eventually(
		mr.updateComponent(func(c *dscv1.Components) {
			c.ModelRegistry.ManagementState = operatorv1.Removed
		}),
	).ShouldNot(
		HaveOccurred(),
	)

	g.Eventually(
		mr.List(
			gvk.Deployment,
			client.InNamespace(mr.applicationsNamespace),
			client.MatchingLabels{labels.ComponentPartOf: componentsv1.ModelRegistryInstanceName},
		),
	).Should(
		BeEmpty(),
	)

	g.Eventually(
		mr.List(gvk.ModelRegistry),
	).Should(
		BeEmpty(),
	)
}

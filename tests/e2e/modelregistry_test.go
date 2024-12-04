package e2e_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
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

func (mr *ModelRegistryTestCtx) Update(
	obj client.Object,
	fn func(obj *unstructured.Unstructured) error,
	option ...client.GetOption,
) func() (*unstructured.Unstructured, error) {
	return func() (*unstructured.Unstructured, error) {
		if err := mr.customClient.Get(mr.ctx, client.ObjectKeyFromObject(obj), obj, option...); err != nil {
			return nil, fmt.Errorf("failed to fetch resource: %w", err)
		}

		in, err := resources.ToUnstructured(obj)
		if err != nil {
			return nil, fmt.Errorf("failed to convert to unstructured: %w", err)
		}

		if err := fn(in); err != nil {
			return nil, fmt.Errorf("failed to apply function: %w", err)
		}

		if err := mr.customClient.Update(mr.ctx, in); err != nil {
			return nil, fmt.Errorf("failed to update resource: %w", err)
		}

		return in, nil
	}
}

func (mr *ModelRegistryTestCtx) Delete(
	gvk schema.GroupVersionKind,
	ns string,
	name string,
	option ...client.DeleteOption,
) func() error {
	return func() error {
		u := resources.GvkToUnstructured(gvk)
		u.SetName(name)
		u.SetNamespace(ns)

		err := mr.customClient.Delete(mr.ctx, u, option...)
		if err != nil {
			return err
		}

		return nil
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

func (mr *ModelRegistryTestCtx) getInstance() (*componentApi.ModelRegistry, error) {
	mri := componentApi.ModelRegistry{}
	nn := types.NamespacedName{Name: componentApi.ModelRegistryInstanceName}

	err := mr.customClient.Get(mr.ctx, nn, &mri)
	if err != nil {
		return nil, err
	}

	return &mri, nil
}

func modelRegistryTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext()
	require.NoError(t, err)

	componentCtx := ModelRegistryTestCtx{
		testContext: tc,
	}

	t.Run(componentCtx.testDsc.Name, func(t *testing.T) {
		t.Run("Validate ModelRegistry instance", componentCtx.validateModelRegistryInstance)
		t.Run("Validate ModelRegistry operands OwnerReferences", componentCtx.validateOperandsOwnerReferences)
		t.Run("Validate ModelRegistry operands Watched Resources", componentCtx.validateOperandsWatchedResources)
		t.Run("Validate ModelRegistry operands Dynamically Watched Resources", componentCtx.validateOperandsDynamicallyWatchedResources)
		t.Run("Validate Update ModelRegistry operands resources", componentCtx.validateUpdateModelRegistryOperandsResources)
		t.Run("Validate ModelRegistry Cert", componentCtx.validateModelRegistryCert)
		t.Run("Validate ModelRegistry ServiceMeshMember", componentCtx.validateModelRegistryServiceMeshMember)
		t.Run("Validate ModelRegistry CRDs reinstated", componentCtx.validateCRDReinstated)
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
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentApi.ModelRegistryKind)},
		),
	).Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, componentApi.ModelRegistryKind),
		),
	))
}

func (mr *ModelRegistryTestCtx) validateOperandsWatchedResources(t *testing.T) {
	g := mr.WithT(t)

	g.Eventually(
		mr.List(
			gvk.ServiceMeshMember,
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentApi.ModelRegistryKind)},
		),
	).Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata | has("ownerReferences") | not`),
		)),
	))
}

func (mr *ModelRegistryTestCtx) validateOperandsDynamicallyWatchedResources(t *testing.T) {
	g := mr.WithT(t)

	mri, err := mr.getInstance()
	g.Expect(err).ShouldNot(HaveOccurred())

	newPt := xid.New().String()
	oldPt := ""

	smm := unstructured.Unstructured{}
	smm.SetGroupVersionKind(gvk.ServiceMeshMember)
	smm.SetName("default")
	smm.SetNamespace(mri.Spec.RegistriesNamespace)

	g.Eventually(
		mr.Update(&smm, func(obj *unstructured.Unstructured) error {
			oldPt = resources.SetAnnotation(obj, annotations.PlatformType, newPt)
			return nil
		}),
	).Should(
		jq.Match(`.metadata.annotations."%s" == "%s"`, annotations.PlatformType, newPt),
	)

	g.Eventually(
		mr.List(
			gvk.ServiceMeshMember,
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentApi.ModelRegistryKind)},
		),
	).Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata.annotations."%s" == "%s"`, annotations.PlatformType, oldPt),
		)),
	))
}

func (mr *ModelRegistryTestCtx) validateUpdateModelRegistryOperandsResources(t *testing.T) {
	g := mr.WithT(t)

	appDeployments, err := mr.kubeClient.AppsV1().Deployments(mr.applicationsNamespace).List(
		mr.ctx,
		metav1.ListOptions{
			LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(componentApi.ModelRegistryKind),
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
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentApi.ModelRegistryKind)},
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
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentApi.ModelRegistryKind)},
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

func (mr *ModelRegistryTestCtx) validateCRDReinstated(t *testing.T) {
	g := mr.WithT(t)
	crdName := "modelregistries.modelregistry.opendatahub.io"
	crdSel := client.MatchingFields{"metadata.name": crdName}

	g.Eventually(
		mr.updateComponent(func(c *dscv1.Components) { c.Dashboard.ManagementState = operatorv1.Removed }),
	).ShouldNot(
		HaveOccurred(),
	)

	g.Eventually(
		mr.List(gvk.Dashboard),
	).Should(
		BeEmpty(),
	)

	g.Eventually(
		mr.List(gvk.CustomResourceDefinition, crdSel),
	).Should(
		HaveLen(1),
	)

	g.Eventually(
		mr.Delete(
			gvk.CustomResourceDefinition,
			"", crdName,
			client.PropagationPolicy(metav1.DeletePropagationForeground),
		),
	).ShouldNot(
		HaveOccurred(),
	)

	g.Eventually(
		mr.List(gvk.CustomResourceDefinition, crdSel),
	).Should(
		BeEmpty(),
	)

	g.Eventually(
		mr.updateComponent(func(c *dscv1.Components) { c.Dashboard.ManagementState = operatorv1.Managed }),
	).ShouldNot(
		HaveOccurred(),
	)

	g.Eventually(
		mr.List(gvk.Dashboard),
	).Should(
		HaveLen(1),
	)

	g.Eventually(
		mr.List(gvk.CustomResourceDefinition, crdSel),
	).Should(
		HaveLen(1),
	)
}

func (mr *ModelRegistryTestCtx) validateModelRegistryDisabled(t *testing.T) {
	g := mr.WithT(t)

	g.Eventually(
		mr.List(
			gvk.Deployment,
			client.InNamespace(mr.applicationsNamespace),
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentApi.ModelRegistryKind)},
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
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentApi.ModelRegistryKind)},
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

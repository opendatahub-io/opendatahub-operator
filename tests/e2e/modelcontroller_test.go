package e2e_test

import (
	"fmt"
	"strings"
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

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/components/modelcontroller"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func modelControllerTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext()
	require.NoError(t, err)

	componentCtx := ModelControllerTestCtx{
		testContext: tc,
	}

	t.Run(componentCtx.testDsc.Name, func(t *testing.T) {
		t.Run("Validate ModelController instance", componentCtx.validateModelControllerInstance)
		t.Run("Validate ModelController operands OwnerReferences", componentCtx.validateOperandsOwnerReferences)
		t.Run("Validate Update ModelController operands resources", componentCtx.validateUpdateModelControllerOperandsResources)
		// must be the latest one
		t.Run("Validate Disabling ModelMesh and KServer Component then ModelController is removed", componentCtx.validateModelControllerDisabled)
	})
}

type ModelControllerTestCtx struct {
	*testContext
}

func (tc *ModelControllerTestCtx) WithT(t *testing.T) *WithT {
	t.Helper()

	g := NewWithT(t)
	g.SetDefaultEventuallyTimeout(generalWaitTimeout)
	g.SetDefaultEventuallyPollingInterval(1 * time.Second)

	return g
}

func (tc *ModelControllerTestCtx) List(
	gvk schema.GroupVersionKind,
	option ...client.ListOption,
) func() ([]unstructured.Unstructured, error) {
	return func() ([]unstructured.Unstructured, error) {
		items := unstructured.UnstructuredList{}
		items.SetGroupVersionKind(gvk)

		err := tc.customClient.List(tc.ctx, &items, option...)
		if err != nil {
			return nil, err
		}

		return items.Items, nil
	}
}

func (tc *ModelControllerTestCtx) Get(
	gvk schema.GroupVersionKind,
	ns string,
	name string,
	option ...client.GetOption,
) func() (*unstructured.Unstructured, error) {
	return func() (*unstructured.Unstructured, error) {
		u := unstructured.Unstructured{}
		u.SetGroupVersionKind(gvk)

		err := tc.customClient.Get(tc.ctx, client.ObjectKey{Namespace: ns, Name: name}, &u, option...)
		if err != nil {
			return nil, err
		}

		return &u, nil
	}
}

func (tc *ModelControllerTestCtx) Update(
	obj client.Object,
	fn func(obj *unstructured.Unstructured) error,
	option ...client.GetOption,
) func() (*unstructured.Unstructured, error) {
	return func() (*unstructured.Unstructured, error) {
		if err := tc.customClient.Get(tc.ctx, client.ObjectKeyFromObject(obj), obj, option...); err != nil {
			return nil, fmt.Errorf("failed to fetch resource: %w", err)
		}

		in, err := resources.ToUnstructured(obj)
		if err != nil {
			return nil, fmt.Errorf("failed to convert to unstructured: %w", err)
		}

		if err := fn(in); err != nil {
			return nil, fmt.Errorf("failed to apply function: %w", err)
		}

		if err := tc.customClient.Update(tc.ctx, in); err != nil {
			return nil, fmt.Errorf("failed to update resource: %w", err)
		}

		return in, nil
	}
}

func (tc *ModelControllerTestCtx) Delete(
	gvk schema.GroupVersionKind,
	ns string,
	name string,
	option ...client.DeleteOption,
) func() error {
	return func() error {
		u := resources.GvkToUnstructured(gvk)
		u.SetName(name)
		u.SetNamespace(ns)

		err := tc.customClient.Delete(tc.ctx, u, option...)
		if err != nil {
			return err
		}

		return nil
	}
}

func (tc *ModelControllerTestCtx) MergePatch(
	obj client.Object,
	patch []byte,
) func() (*unstructured.Unstructured, error) {
	return func() (*unstructured.Unstructured, error) {
		u, err := resources.ToUnstructured(obj)
		if err != nil {
			return nil, err
		}

		err = tc.customClient.Patch(tc.ctx, u, client.RawPatch(types.MergePatchType, patch))
		if err != nil {
			return nil, err
		}

		return u, nil
	}
}

func (tc *ModelControllerTestCtx) updateComponent(fn func(dsc *dscv1.Components)) func() error {
	return func() error {
		err := tc.customClient.Get(tc.ctx, types.NamespacedName{Name: tc.testDsc.Name}, tc.testDsc)
		if err != nil {
			return err
		}

		fn(&tc.testDsc.Spec.Components)

		err = tc.customClient.Update(tc.ctx, tc.testDsc)
		if err != nil {
			return err
		}

		return nil
	}
}

func (tc *ModelControllerTestCtx) validateModelControllerInstance(t *testing.T) {
	g := tc.WithT(t)

	g.Eventually(
		tc.updateComponent(func(c *dscv1.Components) {
			c.ModelMeshServing.ManagementState = operatorv1.Managed
		}),
	).ShouldNot(
		HaveOccurred(),
	)

	g.Eventually(
		tc.List(gvk.ModelController),
	).Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
			jq.Match(`.status.phase == "%s"`, readyStatus),
		)),
	))

	g.Eventually(
		tc.List(gvk.DataScienceCluster),
	).Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, modelcontroller.ReadyConditionType, metav1.ConditionTrue),
		)),
	))
}

func (tc *ModelControllerTestCtx) validateOperandsOwnerReferences(t *testing.T) {
	g := tc.WithT(t)

	g.Eventually(
		tc.List(
			gvk.Deployment,
			client.InNamespace(tc.applicationsNamespace),
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentApi.ModelControllerKind)},
		),
	).Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, componentApi.ModelControllerKind),
		),
	))
}

func (tc *ModelControllerTestCtx) validateUpdateModelControllerOperandsResources(t *testing.T) {
	g := tc.WithT(t)

	appDeployments, err := tc.kubeClient.AppsV1().Deployments(tc.applicationsNamespace).List(
		tc.ctx,
		metav1.ListOptions{
			LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(componentApi.ModelControllerKind),
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
		tc.Get(
			gvk.Deployment,
			appDeployments.Items[0].Namespace,
			appDeployments.Items[0].Name,
		),
	).Should(
		jq.Match(`.spec.replicas == %d`, expectedReplica),
	)

	g.Consistently(
		tc.Get(
			gvk.Deployment,
			appDeployments.Items[0].Namespace,
			appDeployments.Items[0].Name,
		),
	).WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(
		jq.Match(`.spec.replicas == %d`, expectedReplica),
	)
}

func (tc *ModelControllerTestCtx) validateModelControllerDisabled(t *testing.T) {
	g := tc.WithT(t)

	g.Eventually(
		tc.List(
			gvk.Deployment,
			client.InNamespace(tc.applicationsNamespace),
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentApi.ModelControllerKind)},
		),
	).Should(
		HaveLen(1),
	)

	g.Eventually(
		tc.updateComponent(func(c *dscv1.Components) {
			c.ModelMeshServing.ManagementState = operatorv1.Removed
			c.Kserve.ManagementState = operatorv1.Removed
		}),
	).ShouldNot(
		HaveOccurred(),
	)

	g.Eventually(
		tc.List(
			gvk.Deployment,
			client.InNamespace(tc.applicationsNamespace),
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentApi.ModelControllerKind)},
		),
	).Should(
		BeEmpty(),
	)

	g.Eventually(
		tc.List(gvk.ModelController),
	).Should(
		BeEmpty(),
	)

	g.Eventually(
		tc.List(gvk.DataScienceCluster),
	).Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, modelcontroller.ReadyConditionType, metav1.ConditionFalse),
		),
	))
}

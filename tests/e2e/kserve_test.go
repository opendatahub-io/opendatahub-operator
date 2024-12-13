package e2e_test

import (
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

type KserveTestCtx struct {
	*testContext
}

func kserveTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext()
	require.NoError(t, err)

	componentCtx := KserveTestCtx{
		testContext: tc,
	}

	t.Run(componentCtx.testDsc.Name, func(t *testing.T) {
		t.Run("Validate Kserve instance", componentCtx.validateKserveInstance)
		t.Run("Validate default certs available", componentCtx.validateDefaultCertsAvailable)
		t.Run("Validate Kserve operands OwnerReferences", componentCtx.validateOperandsOwnerReferences)
		t.Run("Validate Update Kserve operands resources", componentCtx.validateUpdateKserveOperandsResources)
		// must be the latest one
		t.Run("Validate Disabling Kserve Component", componentCtx.validateKserveDisabled)
	})
}

func (k *KserveTestCtx) validateKserveInstance(t *testing.T) {
	g := k.WithT(t)

	g.Eventually(
		k.updateComponent(func(c *dscv1.Components) {
			c.Kserve.ManagementState = operatorv1.Managed
		}),
	).ShouldNot(
		HaveOccurred(),
	)

	g.Eventually(
		k.List(gvk.Kserve),
	).Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
			jq.Match(`.spec.serving.name == "%s"`, k.testDsc.Spec.Components.Kserve.Serving.Name),
			jq.Match(`.spec.serving.managementState == "%s"`, k.testDsc.Spec.Components.Kserve.Serving.ManagementState),
			jq.Match(`.spec.serving.ingressGateway.certificate.type == "%s"`,
				k.testDsc.Spec.Components.Kserve.Serving.IngressGateway.Certificate.Type),

			jq.Match(`.status.phase == "%s"`, readyStatus),
		)),
	))

	g.Eventually(
		k.List(gvk.DataScienceCluster),
	).Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.KserveComponentName, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.ModelControllerComponentName, metav1.ConditionTrue),
			jq.Match(`.status.installedComponents."%s" == true`, kserve.LegacyComponentName),
			jq.Match(`.status.components.%s.managementState == "%s"`, componentApi.KserveComponentName, operatorv1.Managed),
		)),
	))

	g.Eventually(
		k.List(gvk.ModelController),
	).Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
			jq.Match(`.spec.kserve.managementState == "%s"`, operatorv1.Managed),
		)),
	))
}

func (k *KserveTestCtx) validateDefaultCertsAvailable(t *testing.T) {
	err := k.testDefaultCertsAvailable()
	require.NoError(t, err, "error getting default cert secrets for Kserve")
}

func (k *KserveTestCtx) validateOperandsOwnerReferences(t *testing.T) {
	g := k.WithT(t)

	g.Eventually(
		k.updateComponent(func(c *dscv1.Components) {
			c.Kserve.ManagementState = operatorv1.Managed
		}),
	).ShouldNot(
		HaveOccurred(),
	)

	g.Eventually(
		k.List(
			gvk.Deployment,
			client.InNamespace(k.applicationsNamespace),
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentApi.KserveKind)},
		),
	).Should(And(
		HaveLen(1), // only kserve-controller-manager
		HaveEach(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, componentApi.KserveKind),
		),
	))

	g.Eventually(
		k.List(gvk.DataScienceCluster),
	).Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.KserveComponentName, metav1.ConditionTrue),
		),
	))
}

func (k *KserveTestCtx) validateUpdateKserveOperandsResources(t *testing.T) {
	g := k.WithT(t)

	matchLabels := map[string]string{
		"control-plane":       "kserve-controller-manager",
		labels.PlatformPartOf: strings.ToLower(componentApi.KserveKind),
	}

	listOpts := []client.ListOption{
		client.MatchingLabels(matchLabels),
		client.InNamespace(k.applicationsNamespace),
	}

	appDeployments, err := k.kubeClient.AppsV1().Deployments(k.applicationsNamespace).List(
		k.ctx,
		metav1.ListOptions{
			LabelSelector: k8slabels.SelectorFromSet(matchLabels).String(),
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

	updatedDep, err := k.kubeClient.AppsV1().Deployments(k.applicationsNamespace).UpdateScale(
		k.ctx,
		testDeployment.Name,
		patchedReplica,
		metav1.UpdateOptions{},
	)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(updatedDep.Spec.Replicas).Should(Equal(patchedReplica.Spec.Replicas))

	g.Eventually(
		k.List(
			gvk.Deployment,
			listOpts...,
		),
	).Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.spec.replicas == %d`, expectedReplica),
		),
	))

	g.Consistently(
		k.List(
			gvk.Deployment,
			listOpts...,
		),
	).WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.spec.replicas == %d`, expectedReplica),
		),
	))
}

func (k *KserveTestCtx) validateKserveDisabled(t *testing.T) {
	g := k.WithT(t)

	g.Eventually(
		k.List(
			gvk.Deployment,
			client.InNamespace(k.applicationsNamespace),
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentApi.KserveKind)},
		),
	).Should(
		HaveLen(1),
	)

	g.Eventually(
		k.updateComponent(func(c *dscv1.Components) {
			c.Kserve.ManagementState = operatorv1.Removed
		}),
	).ShouldNot(
		HaveOccurred(),
	)

	g.Eventually(
		k.List(
			gvk.Deployment,
			client.InNamespace(k.applicationsNamespace),
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentApi.KserveKind)},
		),
	).Should(
		BeEmpty(),
	)

	g.Eventually(
		k.List(gvk.Kserve),
	).Should(
		BeEmpty(),
	)

	g.Eventually(
		k.List(gvk.DataScienceCluster),
	).Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.KserveComponentName, metav1.ConditionFalse),
		),
	))
}

func (k *KserveTestCtx) WithT(t *testing.T) *WithT {
	t.Helper()

	g := NewWithT(t)
	g.SetDefaultEventuallyTimeout(generalWaitTimeout)
	g.SetDefaultEventuallyPollingInterval(1 * time.Second)

	return g
}

func (k *KserveTestCtx) List(
	gvk schema.GroupVersionKind,
	option ...client.ListOption,
) func() ([]unstructured.Unstructured, error) {
	return func() ([]unstructured.Unstructured, error) {
		items := unstructured.UnstructuredList{}
		items.SetGroupVersionKind(gvk)

		err := k.customClient.List(k.ctx, &items, option...)
		if err != nil {
			return nil, err
		}

		return items.Items, nil
	}
}

func (k *KserveTestCtx) Get(
	gvk schema.GroupVersionKind,
	ns string,
	name string,
	option ...client.GetOption,
) func() (*unstructured.Unstructured, error) {
	return func() (*unstructured.Unstructured, error) {
		u := unstructured.Unstructured{}
		u.SetGroupVersionKind(gvk)

		err := k.customClient.Get(k.ctx, client.ObjectKey{Namespace: ns, Name: name}, &u, option...)
		if err != nil {
			return nil, err
		}

		return &u, nil
	}
}
func (k *KserveTestCtx) MergePatch(
	obj client.Object,
	patch []byte,
) func() (*unstructured.Unstructured, error) {
	return func() (*unstructured.Unstructured, error) {
		u, err := resources.ToUnstructured(obj)
		if err != nil {
			return nil, err
		}

		err = k.customClient.Patch(k.ctx, u, client.RawPatch(types.MergePatchType, patch))
		if err != nil {
			return nil, err
		}

		return u, nil
	}
}

func (k *KserveTestCtx) updateComponent(fn func(dsc *dscv1.Components)) func() error {
	return func() error {
		err := k.customClient.Get(k.ctx, types.NamespacedName{Name: k.testDsc.Name}, k.testDsc)
		if err != nil {
			return err
		}

		fn(&k.testDsc.Spec.Components)

		err = k.customClient.Update(k.ctx, k.testDsc)
		if err != nil {
			return err
		}

		return nil
	}
}

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

	componentsv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

type DashboardTestCtx struct {
	*testContext
}

func (d *DashboardTestCtx) WithT(t *testing.T) *WithT {
	t.Helper()

	g := NewWithT(t)
	g.SetDefaultEventuallyTimeout(generalWaitTimeout)
	g.SetDefaultEventuallyPollingInterval(1 * time.Second)

	return g
}

func (d *DashboardTestCtx) List(
	gvk schema.GroupVersionKind,
	option ...client.ListOption,
) func() ([]unstructured.Unstructured, error) {
	return func() ([]unstructured.Unstructured, error) {
		items := unstructured.UnstructuredList{}
		items.SetGroupVersionKind(gvk)

		err := d.customClient.List(d.ctx, &items, option...)
		if err != nil {
			return nil, err
		}

		return items.Items, nil
	}
}

func (d *DashboardTestCtx) Get(
	gvk schema.GroupVersionKind,
	ns string,
	name string,
	option ...client.GetOption,
) func() (*unstructured.Unstructured, error) {
	return func() (*unstructured.Unstructured, error) {
		u := unstructured.Unstructured{}
		u.SetGroupVersionKind(gvk)

		err := d.customClient.Get(d.ctx, client.ObjectKey{Namespace: ns, Name: name}, &u, option...)
		if err != nil {
			return nil, err
		}

		return &u, nil
	}
}

func (d *DashboardTestCtx) Update(
	obj client.Object,
	fn func(obj *unstructured.Unstructured) error,
	option ...client.GetOption,
) func() (*unstructured.Unstructured, error) {
	return func() (*unstructured.Unstructured, error) {
		if err := d.customClient.Get(d.ctx, client.ObjectKeyFromObject(obj), obj, option...); err != nil {
			return nil, fmt.Errorf("failed to fetch resource: %w", err)
		}

		in, err := resources.ToUnstructured(obj)
		if err != nil {
			return nil, fmt.Errorf("failed to convert to unstructured: %w", err)
		}

		if err := fn(in); err != nil {
			return nil, fmt.Errorf("failed to apply function: %w", err)
		}

		if err := d.customClient.Update(d.ctx, in); err != nil {
			return nil, fmt.Errorf("failed to update resource: %w", err)
		}

		return in, nil
	}
}

func (d *DashboardTestCtx) MergePatch(
	obj client.Object,
	patch []byte,
) func() (*unstructured.Unstructured, error) {
	return func() (*unstructured.Unstructured, error) {
		u, err := resources.ToUnstructured(obj)
		if err != nil {
			return nil, err
		}

		err = d.customClient.Patch(d.ctx, u, client.RawPatch(types.MergePatchType, patch))
		if err != nil {
			return nil, err
		}

		return u, nil
	}
}

func (d *DashboardTestCtx) updateComponent(fn func(dsc *dscv1.Components)) func() error {
	return func() error {
		err := d.customClient.Get(d.ctx, types.NamespacedName{Name: d.testDsc.Name}, d.testDsc)
		if err != nil {
			return err
		}

		fn(&d.testDsc.Spec.Components)

		err = d.customClient.Update(d.ctx, d.testDsc)
		if err != nil {
			return err
		}

		return nil
	}
}

func (d *DashboardTestCtx) getInstance() (*componentsv1alpha1.Dashboard, error) {
	mri := componentsv1alpha1.Dashboard{}
	nn := types.NamespacedName{Name: componentsv1alpha1.DashboardInstanceName}

	err := d.customClient.Get(d.ctx, nn, &mri)
	if err != nil {
		return nil, err
	}

	return &mri, nil
}

func dashboardTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext()
	require.NoError(t, err)

	componentCtx := DashboardTestCtx{
		testContext: tc,
	}

	t.Run(componentCtx.testDsc.Name, func(t *testing.T) {
		t.Run("Validate Dashboard instance", componentCtx.validateDashboardInstance)
		t.Run("Validate Dashboard operands have OwnerReferences", componentCtx.validateOperandsOwnerReferences)
		t.Run("Validate Dashboard dynamically watches operands", componentCtx.validateOperandsDynamicallyWatchedResources)
		t.Run("Validate Dashboard update operand resources", componentCtx.validateUpdateOperandsResources)
		// must be the latest one
		t.Run("Validate Disabling Dashboard Component", componentCtx.validateDashboardDisabled)
	})
}

func (d *DashboardTestCtx) validateDashboardInstance(t *testing.T) {
	g := d.WithT(t)

	g.Eventually(
		d.List(gvk.Dashboard),
	).Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
			jq.Match(`.status.phase == "%s"`, readyStatus),
		)),
	))
}

func (d *DashboardTestCtx) validateOperandsOwnerReferences(t *testing.T) {
	g := d.WithT(t)

	g.Eventually(
		d.List(
			gvk.Deployment,
			client.InNamespace(d.applicationsNamespace),
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentsv1alpha1.DashboardKind)},
		),
	).Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, componentsv1alpha1.DashboardKind),
		),
	))
}

func (d *DashboardTestCtx) validateOperandsDynamicallyWatchedResources(t *testing.T) {
	g := d.WithT(t)

	_, err := d.getInstance()
	g.Expect(err).ShouldNot(HaveOccurred())

	newPt := xid.New().String()
	oldPt := ""

	odhapp := unstructured.Unstructured{}
	odhapp.SetGroupVersionKind(gvk.OdhApplication)
	odhapp.SetName("jupyter")
	odhapp.SetNamespace(d.applicationsNamespace)

	g.Eventually(
		d.Update(&odhapp, func(obj *unstructured.Unstructured) error {
			oldPt = resources.SetAnnotation(obj, annotations.PlatformType, newPt)
			return nil
		}),
	).Should(
		jq.Match(`.metadata.annotations."%s" == "%s"`, annotations.PlatformType, newPt),
	)

	g.Eventually(
		d.List(
			gvk.OdhApplication,
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentsv1alpha1.DashboardKind)},
		),
	).Should(And(
		HaveEach(
			jq.Match(`.metadata.annotations."%s" == "%s"`, annotations.PlatformType, oldPt),
		),
	))
}

func (d *DashboardTestCtx) validateUpdateOperandsResources(t *testing.T) {
	g := d.WithT(t)

	appDeployments, err := d.kubeClient.AppsV1().Deployments(d.applicationsNamespace).List(
		d.ctx,
		metav1.ListOptions{
			LabelSelector: labels.PlatformPartOf + "=" + strings.ToLower(componentsv1alpha1.DashboardKind),
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(appDeployments.Items).To(HaveLen(1))

	const expectedReplica int32 = 1 // from 2 to 1

	patchedReplica := &autoscalingv1.Scale{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appDeployments.Items[0].Name,
			Namespace: appDeployments.Items[0].Namespace,
		},
		Spec: autoscalingv1.ScaleSpec{
			Replicas: expectedReplica,
		},
		Status: autoscalingv1.ScaleStatus{},
	}

	updatedDep, err := d.kubeClient.AppsV1().Deployments(d.applicationsNamespace).UpdateScale(
		d.ctx,
		appDeployments.Items[0].Name,
		patchedReplica,
		metav1.UpdateOptions{},
	)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(updatedDep.Spec.Replicas).Should(Equal(patchedReplica.Spec.Replicas))

	g.Eventually(
		d.List(
			gvk.Deployment,
			client.InNamespace(d.applicationsNamespace),
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentsv1alpha1.DashboardKind)},
		),
	).Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.spec.replicas == %d`, expectedReplica),
		),
	))

	g.Consistently(
		d.List(
			gvk.Deployment,
			client.InNamespace(d.applicationsNamespace),
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentsv1alpha1.DashboardKind)},
		),
	).WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(And(
		HaveLen(1),
		HaveEach(
			jq.Match(`.spec.replicas == %d`, expectedReplica),
		),
	))
}

func (d *DashboardTestCtx) validateDashboardDisabled(t *testing.T) {
	g := d.WithT(t)

	g.Eventually(
		d.List(
			gvk.Deployment,
			client.InNamespace(d.applicationsNamespace),
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentsv1alpha1.DashboardKind)},
		),
	).Should(
		HaveLen(1),
	)

	g.Eventually(
		d.updateComponent(func(c *dscv1.Components) {
			c.Dashboard.ManagementState = operatorv1.Removed
		}),
	).ShouldNot(
		HaveOccurred(),
	)

	g.Eventually(
		d.List(
			gvk.Deployment,
			client.InNamespace(d.applicationsNamespace),
			client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(componentsv1alpha1.DashboardKind)},
		),
	).Should(
		BeEmpty(),
	)

	g.Eventually(
		d.List(gvk.Dashboard),
	).Should(
		BeEmpty(),
	)
}

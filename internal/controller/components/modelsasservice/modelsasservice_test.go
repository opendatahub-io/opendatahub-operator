//nolint:testpackage
package modelsasservice

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/onsi/gomega/types"
	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	pkgtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

const testApplicationsNamespace = "tenant-test-ns"

func testDSCI(appNS string) *dsciv2.DSCInitialization {
	return &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: appNS,
		},
	}
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}

	name := handler.GetName()
	g.Expect(name).Should(Equal(componentApi.ModelsAsServiceComponentName))
}

func TestNewCRObject_ReturnsNil(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}
	dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)

	cr, err := handler.NewCRObject(context.Background(), nil, dsc)
	g.Expect(err).To(Succeed())
	g.Expect(cr).Should(BeNil(), "maas-controller owns Tenant creation, ODH NewCRObject must return nil")
}

func TestIsEnabled(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}

	testCases := []struct {
		name            string
		kserveState     operatorv1.ManagementState
		maasState       operatorv1.ManagementState
		expectedEnabled func() types.GomegaMatcher
	}{
		{"should be enabled when both KServe and MaaS are managed", operatorv1.Managed, operatorv1.Managed, BeTrue},
		{"should be disabled when KServe not managed", operatorv1.Removed, operatorv1.Managed, BeFalse},
		{"should be disabled when KServe managed but MaaS is not enabled", operatorv1.Managed, operatorv1.Removed, BeFalse},
		{"should be disabled when KServe is unmanaged", operatorv1.Unmanaged, operatorv1.Managed, BeFalse},
		{"should be disabled when both KServe and MaaS are unmanaged", operatorv1.Unmanaged, operatorv1.Unmanaged, BeFalse},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dsc := createDSCWithKServeAndMaaS(tc.kserveState, tc.maasState)
			g.Expect(handler.IsEnabled(dsc)).Should(tc.expectedEnabled())
		})
	}
}

func TestUpdateDSCStatus(t *testing.T) {
	handler := &componentHandler{}

	t.Run("should return ConditionFalse when component CR has deletionTimestamp", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
		cr := createTenantCR(true)
		now := metav1.Now()
		cr.SetDeletionTimestamp(&now)
		cr.SetFinalizers([]string{"test-finalizer"})

		cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(testApplicationsNamespace), dsc, cr))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionFalse))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.DeletingReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, ReadyConditionType, status.DeletingMessage),
		)))
	})

	t.Run("should handle enabled component with ready Tenant CR", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
		cr := createTenantCR(true)

		cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(testApplicationsNamespace), dsc, cr))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionTrue))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.ReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Component is ready"`, ReadyConditionType),
		)))
	})

	t.Run("should show MaaS management state when disabled via MaaS Removed", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Removed)

		cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(testApplicationsNamespace), dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, string(operatorv1.Removed)),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .severity == "%s"`, ReadyConditionType, common.ConditionSeverityInfo),
		)))
	})

	t.Run("should show MaaS management state when disabled via KServe not managed", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Removed, operatorv1.Managed)

		cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(testApplicationsNamespace), dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, string(operatorv1.Managed)),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .severity == "%s"`, ReadyConditionType, common.ConditionSeverityInfo),
		)))
	})

	t.Run("should return NotReady when Tenant CR does not exist", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)

		cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(testApplicationsNamespace), dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionFalse))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.NotReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Tenant CR not available yet"`, ReadyConditionType),
		)))
	})

	t.Run("should handle enabled component with not-ready Tenant CR", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
		cr := createTenantCR(false)

		cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(testApplicationsNamespace), dsc, cr))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionFalse))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.NotReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Component is not ready"`, ReadyConditionType),
		)))
	})

	t.Run("should handle Tenant CR with no status conditions", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
		cr := &maasv1alpha1.Tenant{}
		cr.SetName(maasv1alpha1.TenantInstanceName)
		cr.SetNamespace(MaaSSubscriptionNamespace)
		cr.APIVersion = maasv1alpha1.GroupVersion.String()
		cr.Kind = maasv1alpha1.TenantKind

		cli, err := fakeclient.New(fakeclient.WithObjects(testDSCI(testApplicationsNamespace), dsc, cr))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &pkgtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionFalse))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.NotReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Tenant CR exists but has no ready condition yet"`, ReadyConditionType),
		)))
	})
}

func createDSCWithKServeAndMaaS(kserveState, maasState operatorv1.ManagementState) *dscv2.DataScienceCluster {
	dsc := dscv2.DataScienceCluster{}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)
	dsc.SetName("test-dsc")

	dsc.Spec.Components.Kserve.ManagementState = kserveState
	dsc.Spec.Components.Kserve.ModelsAsService.ManagementState = maasState

	return &dsc
}

func createTenantCR(ready bool) *maasv1alpha1.Tenant {
	c := &maasv1alpha1.Tenant{}
	c.SetName(maasv1alpha1.TenantInstanceName)
	c.SetNamespace(MaaSSubscriptionNamespace)
	c.APIVersion = maasv1alpha1.GroupVersion.String()
	c.Kind = maasv1alpha1.TenantKind
	now := metav1.Now()
	if ready {
		c.Status.Conditions = []metav1.Condition{{
			Type:               status.ConditionTypeReady,
			Status:             metav1.ConditionTrue,
			Reason:             status.ReadyReason,
			Message:            "Component is ready",
			LastTransitionTime: now,
		}}
	} else {
		c.Status.Conditions = []metav1.Condition{{
			Type:               status.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             status.NotReadyReason,
			Message:            "Component is not ready",
			LastTransitionTime: now,
		}}
	}

	return c
}

//nolint:testpackage
package datasciencecluster

import (
	"context"
	"encoding/json"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

// mockHandler is a minimal ComponentHandler for testing computeComponentsStatus.
type mockHandler struct {
	name    string
	enabled bool
	status  metav1.ConditionStatus
	err     error
}

func (m *mockHandler) Init(_ common.Platform, _ operatorconfig.OperatorSettings) error { return nil }
func (m *mockHandler) GetName() string                                                 { return m.name }
func (m *mockHandler) NewCRObject(_ context.Context, _ client.Client, _ *dscv2.DataScienceCluster) (common.PlatformObject, error) {
	return nil, nil
}
func (m *mockHandler) NewComponentReconciler(_ context.Context, _ ctrl.Manager) error {
	return nil
}
func (m *mockHandler) IsEnabled(_ *dscv2.DataScienceCluster) bool { return m.enabled }
func (m *mockHandler) UpdateDSCStatus(_ context.Context, _ *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	return m.status, m.err
}

func newRegistry(handlers ...cr.ComponentHandler) *cr.Registry {
	reg := &cr.Registry{}
	for _, h := range handlers {
		reg.Add(h)
	}
	return reg
}

func newDSC() *dscv2.DataScienceCluster {
	dsc := &dscv2.DataScienceCluster{}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)
	dsc.SetName("test-dsc")
	return dsc
}

func TestComputeComponentsStatus(t *testing.T) {
	t.Run("all managed components ready should set ComponentsReady=True", func(t *testing.T) {
		g := NewWithT(t)
		dsc := newDSC()
		reg := newRegistry(
			&mockHandler{name: "comp-a", enabled: true, status: metav1.ConditionTrue},
			&mockHandler{name: "comp-b", enabled: true, status: metav1.ConditionTrue},
		)

		rr := &types.ReconciliationRequest{
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, status.ConditionTypeComponentsReady),
		}

		err := computeComponentsStatus(t.Context(), rr, reg)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(dsc).Should(WithTransform(json.Marshal,
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeComponentsReady, metav1.ConditionTrue),
		))
	})

	t.Run("ConditionUnknown from enabled component should set ComponentsReady=False", func(t *testing.T) {
		g := NewWithT(t)
		dsc := newDSC()
		reg := newRegistry(
			&mockHandler{name: "comp-a", enabled: true, status: metav1.ConditionTrue},
			&mockHandler{name: "comp-b", enabled: true, status: metav1.ConditionUnknown},
		)

		rr := &types.ReconciliationRequest{
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, status.ConditionTypeComponentsReady),
		}

		err := computeComponentsStatus(t.Context(), rr, reg)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeComponentsReady, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("comp-b")`,
				status.ConditionTypeComponentsReady),
		)))
	})

	t.Run("ConditionFalse from enabled component should set ComponentsReady=False", func(t *testing.T) {
		g := NewWithT(t)
		dsc := newDSC()
		reg := newRegistry(
			&mockHandler{name: "comp-a", enabled: true, status: metav1.ConditionTrue},
			&mockHandler{name: "comp-b", enabled: true, status: metav1.ConditionFalse},
		)

		rr := &types.ReconciliationRequest{
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, status.ConditionTypeComponentsReady),
		}

		err := computeComponentsStatus(t.Context(), rr, reg)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeComponentsReady, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("comp-b")`,
				status.ConditionTypeComponentsReady),
		)))
	})

	t.Run("disabled component returning ConditionFalse should count as not ready", func(t *testing.T) {
		g := NewWithT(t)
		dsc := newDSC()
		reg := newRegistry(
			&mockHandler{name: "comp-a", enabled: true, status: metav1.ConditionTrue},
			&mockHandler{name: "comp-stuck", enabled: false, status: metav1.ConditionFalse},
		)

		rr := &types.ReconciliationRequest{
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, status.ConditionTypeComponentsReady),
		}

		err := computeComponentsStatus(t.Context(), rr, reg)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeComponentsReady, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("comp-stuck")`,
				status.ConditionTypeComponentsReady),
		)))
	})

	t.Run("disabled component returning ConditionUnknown should be skipped", func(t *testing.T) {
		g := NewWithT(t)
		dsc := newDSC()
		reg := newRegistry(
			&mockHandler{name: "comp-a", enabled: true, status: metav1.ConditionTrue},
			&mockHandler{name: "comp-disabled", enabled: false, status: metav1.ConditionUnknown},
		)

		rr := &types.ReconciliationRequest{
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, status.ConditionTypeComponentsReady),
		}

		err := computeComponentsStatus(t.Context(), rr, reg)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(dsc).Should(WithTransform(json.Marshal,
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeComponentsReady, metav1.ConditionTrue),
		))
	})

	t.Run("no managed components should set ComponentsReady=True with info severity", func(t *testing.T) {
		g := NewWithT(t)
		dsc := newDSC()
		reg := newRegistry(
			&mockHandler{name: "comp-a", enabled: false, status: metav1.ConditionUnknown},
		)

		rr := &types.ReconciliationRequest{
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, status.ConditionTypeComponentsReady),
		}

		err := computeComponentsStatus(t.Context(), rr, reg)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeComponentsReady, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`,
				status.ConditionTypeComponentsReady, status.NoManagedComponentsReason),
		)))
	})
}

func TestDeleteMaaSDeploymentIfDisabled(t *testing.T) {
	const appNs = "redhat-ods-applications"
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec:       dsciv2.DSCInitializationSpec{ApplicationsNamespace: appNs},
	}

	newDSCWithMaaS := func(state operatorv1.ManagementState) *dscv2.DataScienceCluster {
		dsc := &dscv2.DataScienceCluster{}
		dsc.SetGroupVersionKind(gvk.DataScienceCluster)
		dsc.SetName("test-dsc")
		dsc.Spec.Components.Kserve.ManagementState = operatorv1.Managed
		dsc.Spec.Components.Kserve.ModelsAsService.ManagementState = state
		return dsc
	}

	t.Run("requests Deployment deletion and returns nil when Deployment exists", func(t *testing.T) {
		g := NewWithT(t)
		dsc := newDSCWithMaaS(operatorv1.Removed)

		cli, err := fakeclient.New(fakeclient.WithObjects(
			dsci, dsc,
			&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "maas-controller", Namespace: appNs}},
		))
		g.Expect(err).ShouldNot(HaveOccurred())

		reg := newRegistry(&mockHandler{name: componentApi.ModelsAsServiceComponentName, enabled: false})
		err = deleteMaaSDeploymentIfDisabled(t.Context(), &types.ReconciliationRequest{Client: cli, Instance: dsc}, dsc, reg)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Deployment deletion requested; LifecycleReconciler handles RBAC/CRD cleanup via its finalizer.
		depList := &appsv1.DeploymentList{}
		g.Expect(cli.List(t.Context(), depList, client.InNamespace(appNs))).To(Succeed())
		g.Expect(depList.Items).To(BeEmpty())
	})

	t.Run("no-op when MaaS is enabled", func(t *testing.T) {
		g := NewWithT(t)
		dsc := newDSCWithMaaS(operatorv1.Managed)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsci, dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		reg := newRegistry(&mockHandler{name: componentApi.ModelsAsServiceComponentName, enabled: true})
		err = deleteMaaSDeploymentIfDisabled(t.Context(), &types.ReconciliationRequest{Client: cli, Instance: dsc}, dsc, reg)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("no-op when Deployment is already gone", func(t *testing.T) {
		g := NewWithT(t)
		dsc := newDSCWithMaaS(operatorv1.Removed)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsci, dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		reg := newRegistry(&mockHandler{name: componentApi.ModelsAsServiceComponentName, enabled: false})
		err = deleteMaaSDeploymentIfDisabled(t.Context(), &types.ReconciliationRequest{Client: cli, Instance: dsc}, dsc, reg)
		g.Expect(err).ShouldNot(HaveOccurred())
	})
}

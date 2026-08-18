//nolint:testpackage
package modules

import (
	"context"
	"errors"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

type statusTestHandler struct {
	BaseHandler

	enabled      bool
	moduleStatus *ModuleStatus
	statusErr    error
	crState      CRState
}

func (m *statusTestHandler) IsEnabled(_ *configv1alpha1.PlatformModules) bool {
	return m.enabled
}

func (m *statusTestHandler) BuildModuleCR(_ context.Context, _ client.Client, _ *DSCContext, _ *ModuleCRConfig) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (m *statusTestHandler) GetModuleStatus(_ context.Context, _ client.Client) (*ModuleStatus, error) {
	if m.statusErr != nil {
		return nil, m.statusErr
	}
	return m.moduleStatus, nil
}

func (m *statusTestHandler) GetModuleCRState(_ context.Context, _ client.Client) (CRState, error) {
	return m.crState, nil
}

var _ ModuleHandler = (*statusTestHandler)(nil)

func newStatusTestHandler(name, kind string, enabled bool, ms *ModuleStatus) *statusTestHandler {
	return &statusTestHandler{
		BaseHandler: BaseHandler{
			Config: ModuleConfig{
				Name:   name,
				CRName: "default",
				GVK: schema.GroupVersionKind{
					Group:   "components.platform.opendatahub.io",
					Version: "v1alpha1",
					Kind:    kind,
				},
			},
		},
		enabled:      enabled,
		moduleStatus: ms,
	}
}

// setupStatusTest creates a test reconciliation request with per-module
// conditions declared as dependents (simulates DSC controller).
// Uses DataScienceCluster as instance since ComputeModulesStatusDetailed
// requires it.
func setupStatusTest(t *testing.T, handlers ...*statusTestHandler) (*odhtype.ReconciliationRequest, func()) {
	t.Helper()
	g := NewWithT(t)

	condTypes := make([]string, 0, len(handlers)+1)
	for _, h := range handlers {
		condTypes = append(condTypes, h.GetGVK().Kind+status.ReadySuffix)
		for _, sm := range h.GetSubmoduleConditions() {
			condTypes = append(condTypes, sm.DSCConditionType)
		}
	}
	condTypes = append(condTypes, status.ConditionTypeModulesReady)

	oldR := r
	r = &Registry{}
	for _, h := range handlers {
		r.Add(h)
	}

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsc"},
	}

	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: "test-ns",
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
	g.Expect(err).ShouldNot(HaveOccurred())

	cm := conditions.NewManager(dsc, status.ConditionTypeReady, condTypes...)

	rr := &odhtype.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		Conditions: cm,
		Release:    common.Release{Name: cluster.OpenDataHub},
	}

	return rr, func() { r = oldR }
}

// setupStatusTestAggregateOnly creates a test reconciliation request
// with only aggregate ModulesReady declared (simulates Platform controller).
// Uses Platform as instance since computeModulesStatusAggregate requires it.
func setupStatusTestAggregateOnly(t *testing.T, handlers ...*statusTestHandler) (*odhtype.ReconciliationRequest, func()) {
	t.Helper()
	g := NewWithT(t)

	oldR := r
	r = &Registry{}
	for _, h := range handlers {
		r.Add(h)
	}

	platform := &configv1alpha1.Platform{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
	}

	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: "test-ns",
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
	g.Expect(err).ShouldNot(HaveOccurred())

	cm := conditions.NewManager(platform, status.ConditionTypeReady, status.ConditionTypeModulesReady)

	rr := &odhtype.ReconciliationRequest{
		Client:     cli,
		Instance:   platform,
		Conditions: cm,
		Release:    common.Release{Name: cluster.OpenDataHub},
	}

	return rr, func() { r = oldR }
}

func TestComputeModulesStatusDetailed_AllReady(t *testing.T) {
	g := NewWithT(t)

	h1 := newStatusTestHandler("gw", "AIGateway", true, &ModuleStatus{
		Conditions:         []common.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready"}},
		ObservedGeneration: 1,
		Generation:         1,
	})
	h2 := newStatusTestHandler("mon", "Monitoring", true, &ModuleStatus{
		Conditions:         []common.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready"}},
		ObservedGeneration: 1,
		Generation:         1,
	})

	rr, cleanup := setupStatusTest(t, h1, h2)
	defer cleanup()

	err := ComputeModulesStatusDetailed(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(rr.Conditions.GetCondition("AIGatewayReady")).ShouldNot(BeNil())
	g.Expect(rr.Conditions.GetCondition("AIGatewayReady").Status).Should(Equal(metav1.ConditionTrue))

	g.Expect(rr.Conditions.GetCondition("MonitoringReady")).ShouldNot(BeNil())
	g.Expect(rr.Conditions.GetCondition("MonitoringReady").Status).Should(Equal(metav1.ConditionTrue))

	g.Expect(rr.Conditions.GetCondition(status.ConditionTypeModulesReady)).ShouldNot(BeNil())
	g.Expect(rr.Conditions.GetCondition(status.ConditionTypeModulesReady).Status).Should(Equal(metav1.ConditionTrue))
}

func TestComputeModulesStatusDetailed_SomeNotReady(t *testing.T) {
	g := NewWithT(t)

	h1 := newStatusTestHandler("gw", "AIGateway", true, &ModuleStatus{
		Conditions:         []common.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready"}},
		ObservedGeneration: 1,
		Generation:         1,
	})
	h2 := newStatusTestHandler("mon", "Monitoring", true, &ModuleStatus{
		Conditions:         []common.Condition{{Type: "Ready", Status: metav1.ConditionFalse, Reason: "NotReady"}},
		ObservedGeneration: 1,
		Generation:         1,
	})

	rr, cleanup := setupStatusTest(t, h1, h2)
	defer cleanup()

	err := ComputeModulesStatusDetailed(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(rr.Conditions.GetCondition("AIGatewayReady").Status).Should(Equal(metav1.ConditionTrue))
	g.Expect(rr.Conditions.GetCondition("MonitoringReady").Status).Should(Equal(metav1.ConditionFalse))

	modulesReady := rr.Conditions.GetCondition(status.ConditionTypeModulesReady)
	g.Expect(modulesReady.Status).Should(Equal(metav1.ConditionFalse))
	g.Expect(modulesReady.Message).Should(ContainSubstring("mon"))
}

func TestComputeModulesStatusDetailed_DisabledModule(t *testing.T) {
	g := NewWithT(t)

	h1 := newStatusTestHandler("gw", "AIGateway", false, nil)

	rr, cleanup := setupStatusTest(t, h1)
	defer cleanup()

	err := ComputeModulesStatusDetailed(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	cond := rr.Conditions.GetCondition("AIGatewayReady")
	g.Expect(cond).ShouldNot(BeNil())
	g.Expect(cond.Status).Should(Equal(metav1.ConditionFalse))
	g.Expect(cond.Severity).Should(Equal(common.ConditionSeverityInfo))

	modulesReady := rr.Conditions.GetCondition(status.ConditionTypeModulesReady)
	g.Expect(modulesReady.Status).Should(Equal(metav1.ConditionTrue))
	g.Expect(modulesReady.Reason).Should(Equal(status.NoManagedModulesReason))
}

func TestComputeModulesStatusAggregate_DisabledPendingDeletion(t *testing.T) {
	g := NewWithT(t)

	h1 := newStatusTestHandler("gw", "AIGateway", false, nil)
	h1.crState = CRStateAlive

	rr, cleanup := setupStatusTestAggregateOnly(t, h1)
	defer cleanup()

	err := computeModulesStatusAggregate(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	modulesReady := rr.Conditions.GetCondition(status.ConditionTypeModulesReady)
	g.Expect(modulesReady.Status).Should(Equal(metav1.ConditionTrue))
	g.Expect(modulesReady.Severity).Should(Equal(common.ConditionSeverityInfo))
	g.Expect(modulesReady.Reason).Should(Equal(status.RemovedReason))
	g.Expect(modulesReady.Message).Should(ContainSubstring("pending deletion: gw"))
}

func TestComputeModulesStatusDetailed_StatusError(t *testing.T) {
	g := NewWithT(t)

	h1 := newStatusTestHandler("gw", "AIGateway", true, nil)
	h1.statusErr = errors.New("CR not found")

	rr, cleanup := setupStatusTest(t, h1)
	defer cleanup()

	err := ComputeModulesStatusDetailed(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	cond := rr.Conditions.GetCondition("AIGatewayReady")
	g.Expect(cond.Status).Should(Equal(metav1.ConditionFalse))
	g.Expect(cond.Message).Should(ContainSubstring("CR not found"))

	g.Expect(rr.Conditions.GetCondition(status.ConditionTypeModulesReady).Status).Should(Equal(metav1.ConditionFalse))
}

func TestComputeModulesStatusAggregate_AllReady_NoPerModuleConditions(t *testing.T) {
	g := NewWithT(t)

	h1 := newStatusTestHandler("gw", "AIGateway", true, &ModuleStatus{
		Conditions:         []common.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready"}},
		ObservedGeneration: 1,
		Generation:         1,
	})
	h2 := newStatusTestHandler("mon", "Monitoring", true, &ModuleStatus{
		Conditions:         []common.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready"}},
		ObservedGeneration: 1,
		Generation:         1,
	})

	rr, cleanup := setupStatusTestAggregateOnly(t, h1, h2)
	defer cleanup()

	err := computeModulesStatusAggregate(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(rr.Conditions.GetCondition(status.ConditionTypeModulesReady)).ShouldNot(BeNil())
	g.Expect(rr.Conditions.GetCondition(status.ConditionTypeModulesReady).Status).Should(Equal(metav1.ConditionTrue))

	g.Expect(rr.Conditions.GetCondition("AIGatewayReady")).Should(BeNil(),
		"aggregate should not set per-module conditions")
	g.Expect(rr.Conditions.GetCondition("MonitoringReady")).Should(BeNil(),
		"aggregate should not set per-module conditions")
}

func TestComputeModulesStatusAggregate_SomeNotReady(t *testing.T) {
	g := NewWithT(t)

	h1 := newStatusTestHandler("gw", "AIGateway", true, &ModuleStatus{
		Conditions:         []common.Condition{{Type: "Ready", Status: metav1.ConditionFalse, Reason: "NotReady"}},
		ObservedGeneration: 1,
		Generation:         1,
	})

	rr, cleanup := setupStatusTestAggregateOnly(t, h1)
	defer cleanup()

	err := computeModulesStatusAggregate(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	modulesReady := rr.Conditions.GetCondition(status.ConditionTypeModulesReady)
	g.Expect(modulesReady.Status).Should(Equal(metav1.ConditionFalse))
	g.Expect(modulesReady.Message).Should(ContainSubstring("gw"))

	g.Expect(rr.Conditions.GetCondition("AIGatewayReady")).Should(BeNil(),
		"aggregate should not set per-module conditions")
}

func TestComputeModulesStatusAggregate_Degraded(t *testing.T) {
	g := NewWithT(t)

	h1 := newStatusTestHandler("gw", "AIGateway", true, &ModuleStatus{
		Conditions: []common.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready"},
			{Type: "Degraded", Status: metav1.ConditionTrue, Reason: "Degraded"},
		},
		ObservedGeneration: 1,
		Generation:         1,
	})

	rr, cleanup := setupStatusTestAggregateOnly(t, h1)
	defer cleanup()

	err := computeModulesStatusAggregate(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	modulesReady := rr.Conditions.GetCondition(status.ConditionTypeModulesReady)
	g.Expect(modulesReady.Status).Should(Equal(metav1.ConditionFalse))
	g.Expect(modulesReady.Reason).Should(Equal(status.ConditionTypeDegraded))
}

func TestComputeModulesStatusDetailed_StatusError_SubmodulesNotMarkedRemoved(t *testing.T) {
	g := NewWithT(t)

	h1 := newStatusTestHandler("gw", "AIGateway", true, nil)
	h1.statusErr = errors.New("CR not found")
	h1.Config.SubmoduleConditions = []SubmoduleCondition{
		{
			SourceConditionType: "BatchGatewayReady",
			DSCConditionType:    "BatchGatewayReady",
			StatusFieldName:     "BatchGateway",
			IsEnabled:           func(_ *DSCContext) bool { return true },
		},
		{
			SourceConditionType: "ModelsAsAServiceReady",
			DSCConditionType:    "ModelsAsAServiceReady",
			StatusFieldName:     "ModelsAsAService",
			IsEnabled:           func(_ *DSCContext) bool { return false },
		},
	}

	rr, cleanup := setupStatusTest(t, h1)
	defer cleanup()

	err := ComputeModulesStatusDetailed(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	dsc, ok := rr.Instance.(*dscv2.DataScienceCluster)
	g.Expect(ok).Should(BeTrue())

	batchCond := rr.Conditions.GetCondition("BatchGatewayReady")
	g.Expect(batchCond).ShouldNot(BeNil())
	g.Expect(batchCond.Reason).Should(Equal(status.NotReadyReason),
		"enabled submodule should inherit parent's NotReady reason, not Removed")
	g.Expect(dsc.Status.Components.BatchGateway.ManagementState).Should(Equal(operatorv1.Managed))

	maasCond := rr.Conditions.GetCondition("ModelsAsAServiceReady")
	g.Expect(maasCond).ShouldNot(BeNil())
	g.Expect(maasCond.Reason).Should(Equal(status.RemovedReason),
		"disabled submodule should be Removed regardless of parent status")
	g.Expect(dsc.Status.Components.ModelsAsAService.ManagementState).Should(Equal(operatorv1.Removed))
}

func TestComputeModulesStatusDetailed_StaleStatus_SubmodulesNotMarkedRemoved(t *testing.T) {
	g := NewWithT(t)

	h1 := newStatusTestHandler("gw", "AIGateway", true, &ModuleStatus{
		Conditions:         []common.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready"}},
		ObservedGeneration: 1,
		Generation:         2,
	})
	h1.Config.SubmoduleConditions = []SubmoduleCondition{
		{
			SourceConditionType: "BatchGatewayReady",
			DSCConditionType:    "BatchGatewayReady",
			StatusFieldName:     "BatchGateway",
			IsEnabled:           func(_ *DSCContext) bool { return true },
		},
	}

	rr, cleanup := setupStatusTest(t, h1)
	defer cleanup()

	err := ComputeModulesStatusDetailed(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	dsc, ok := rr.Instance.(*dscv2.DataScienceCluster)
	g.Expect(ok).Should(BeTrue())

	batchCond := rr.Conditions.GetCondition("BatchGatewayReady")
	g.Expect(batchCond).ShouldNot(BeNil())
	g.Expect(batchCond.Reason).Should(Equal(status.NotReadyReason),
		"enabled submodule should inherit parent's stale reason, not Removed")
	g.Expect(dsc.Status.Components.BatchGateway.ManagementState).Should(Equal(operatorv1.Managed))
}

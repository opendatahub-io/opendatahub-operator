package modules_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

type testConditionsAccessor struct {
	conds []common.Condition
}

func (a *testConditionsAccessor) GetConditions() []common.Condition {
	return a.conds
}

func (a *testConditionsAccessor) SetConditions(c []common.Condition) {
	a.conds = c
}

func newTestRR() (*odhtype.ReconciliationRequest, *conditions.Manager) {
	accessor := &testConditionsAccessor{}
	mgr := conditions.NewManager(accessor, status.ConditionTypeModulesReady)

	return &odhtype.ReconciliationRequest{
		Conditions: mgr,
	}, mgr
}

func newTestPlatformCtx() *modules.PlatformContext {
	return &modules.PlatformContext{
		DSC: &dscv2.DataScienceCluster{},
	}
}

func TestGetSubmoduleConditions_Empty(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	handler := &modules.BaseHandler{
		Config: modules.ModuleConfig{
			Name:   "test",
			CRName: "default",
			GVK:    schema.GroupVersionKind{Kind: "Test"},
		},
	}

	g.Expect(handler.GetSubmoduleConditions()).Should(BeEmpty())
}

func TestGetSubmoduleConditions_Declared(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	handler := &modules.BaseHandler{
		Config: modules.ModuleConfig{
			Name:   "aigateway",
			CRName: "default",
			GVK:    schema.GroupVersionKind{Kind: "AIGateway"},
			SubmoduleConditions: []modules.SubmoduleCondition{
				{
					SourceConditionType: "ModelsAsServiceReady",
					DSCConditionType:    "ModelsAsServiceReady",
				},
				{
					SourceConditionType: "BatchGatewayReady",
					DSCConditionType:    "BatchGatewayReady",
				},
			},
		},
	}

	subs := handler.GetSubmoduleConditions()
	g.Expect(subs).Should(HaveLen(2))
	g.Expect(subs[0].SourceConditionType).Should(Equal("ModelsAsServiceReady"))
	g.Expect(subs[0].DSCConditionType).Should(Equal("ModelsAsServiceReady"))
	g.Expect(subs[1].SourceConditionType).Should(Equal("BatchGatewayReady"))
	g.Expect(subs[1].DSCConditionType).Should(Equal("BatchGatewayReady"))
}

func TestSubmoduleConditionsFor_NoSubmodules(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	mock := newStatusMock("basic", &modules.ModuleStatus{})
	mock.Config.SubmoduleConditions = nil

	result := modules.SubmoduleConditionsFor(mock)
	g.Expect(result).Should(BeEmpty())
}

func TestSubmoduleConditionsFor_WithSubmodules(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	mock := newStatusMock("aigateway", &modules.ModuleStatus{})
	mock.Config.SubmoduleConditions = []modules.SubmoduleCondition{
		{SourceConditionType: "FooReady", DSCConditionType: "FooReady"},
	}

	result := modules.SubmoduleConditionsFor(mock)
	g.Expect(result).Should(HaveLen(1))
	g.Expect(result[0].DSCConditionType).Should(Equal("FooReady"))
}

func TestMirrorSubmoduleConditions_ConditionFound_True(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	rr, mgr := newTestRR()

	moduleStatus := &modules.ModuleStatus{
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood"},
			{Type: "ModelsAsServiceReady", Status: metav1.ConditionTrue, Reason: "Deployed", Message: "MaaS is healthy"},
		},
	}

	submodules := []modules.SubmoduleCondition{
		{SourceConditionType: "ModelsAsServiceReady", DSCConditionType: "ModelsAsServiceReady"},
	}

	var notReady []string
	modules.MirrorSubmoduleConditions(rr, newTestPlatformCtx(), moduleStatus, submodules, &notReady)

	cond := mgr.GetCondition("ModelsAsServiceReady")
	g.Expect(cond).ShouldNot(BeNil())
	g.Expect(cond.Status).Should(Equal(metav1.ConditionTrue))
	g.Expect(cond.Reason).Should(Equal("Deployed"))
	g.Expect(cond.Message).Should(Equal("MaaS is healthy"))
	g.Expect(notReady).Should(BeEmpty())
}

func TestMirrorSubmoduleConditions_ConditionFound_False(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	rr, mgr := newTestRR()

	moduleStatus := &modules.ModuleStatus{
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood"},
			{Type: "BatchGatewayReady", Status: metav1.ConditionFalse, Reason: "Deploying", Message: "waiting for pods"},
		},
	}

	submodules := []modules.SubmoduleCondition{
		{SourceConditionType: "BatchGatewayReady", DSCConditionType: "BatchGatewayReady"},
	}

	var notReady []string
	modules.MirrorSubmoduleConditions(rr, newTestPlatformCtx(), moduleStatus, submodules, &notReady)

	cond := mgr.GetCondition("BatchGatewayReady")
	g.Expect(cond).ShouldNot(BeNil())
	g.Expect(cond.Status).Should(Equal(metav1.ConditionFalse))
	g.Expect(notReady).Should(ConsistOf("BatchGatewayReady"))
}

func TestMirrorSubmoduleConditions_ConditionAbsent(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	rr, mgr := newTestRR()

	moduleStatus := &modules.ModuleStatus{
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood"},
		},
	}

	submodules := []modules.SubmoduleCondition{
		{SourceConditionType: "ModelsAsServiceReady", DSCConditionType: "ModelsAsServiceReady"},
	}

	var notReady []string
	modules.MirrorSubmoduleConditions(rr, newTestPlatformCtx(), moduleStatus, submodules, &notReady)

	cond := mgr.GetCondition("ModelsAsServiceReady")
	g.Expect(cond).ShouldNot(BeNil())
	g.Expect(cond.Status).Should(Equal(metav1.ConditionFalse))
	g.Expect(cond.Reason).Should(Equal(status.AwaitingReadinessReason))
	g.Expect(cond.Message).Should(ContainSubstring("enabled (Managed)"))
	g.Expect(notReady).Should(ConsistOf("ModelsAsServiceReady"))
}

func TestMirrorSubmoduleConditions_MultipleSubmodules(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	rr, mgr := newTestRR()

	moduleStatus := &modules.ModuleStatus{
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood"},
			{Type: "ModelsAsServiceReady", Status: metav1.ConditionTrue, Reason: "Ready"},
			{Type: "BatchGatewayReady", Status: metav1.ConditionFalse, Reason: "Pending", Message: "waiting"},
		},
	}

	submodules := []modules.SubmoduleCondition{
		{SourceConditionType: "ModelsAsServiceReady", DSCConditionType: "ModelsAsServiceReady"},
		{SourceConditionType: "BatchGatewayReady", DSCConditionType: "BatchGatewayReady"},
	}

	var notReady []string
	modules.MirrorSubmoduleConditions(rr, newTestPlatformCtx(), moduleStatus, submodules, &notReady)

	maasCond := mgr.GetCondition("ModelsAsServiceReady")
	g.Expect(maasCond).ShouldNot(BeNil())
	g.Expect(maasCond.Status).Should(Equal(metav1.ConditionTrue))

	batchCond := mgr.GetCondition("BatchGatewayReady")
	g.Expect(batchCond).ShouldNot(BeNil())
	g.Expect(batchCond.Status).Should(Equal(metav1.ConditionFalse))

	g.Expect(notReady).Should(ConsistOf("BatchGatewayReady"))
}

func TestMirrorSubmoduleConditions_EmptySubmodules(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	rr, mgr := newTestRR()

	moduleStatus := &modules.ModuleStatus{
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue},
		},
	}

	var notReady []string
	modules.MirrorSubmoduleConditions(rr, newTestPlatformCtx(), moduleStatus, nil, &notReady)

	g.Expect(mgr.GetCondition("ModelsAsServiceReady")).Should(BeNil())
	g.Expect(notReady).Should(BeEmpty())
}

func TestMirrorSubmoduleConditions_DifferentSourceAndDSCType(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	rr, mgr := newTestRR()

	moduleStatus := &modules.ModuleStatus{
		Conditions: []metav1.Condition{
			{Type: "InternalMaaSStatus", Status: metav1.ConditionTrue, Reason: "OK", Message: "all good"},
		},
	}

	submodules := []modules.SubmoduleCondition{
		{SourceConditionType: "InternalMaaSStatus", DSCConditionType: "ModelsAsServiceReady"},
	}

	var notReady []string
	modules.MirrorSubmoduleConditions(rr, newTestPlatformCtx(), moduleStatus, submodules, &notReady)

	cond := mgr.GetCondition("ModelsAsServiceReady")
	g.Expect(cond).ShouldNot(BeNil())
	g.Expect(cond.Status).Should(Equal(metav1.ConditionTrue))
	g.Expect(cond.Reason).Should(Equal("OK"))

	g.Expect(mgr.GetCondition("InternalMaaSStatus")).Should(BeNil(),
		"internal type should not appear on DSC")
}

func TestMirrorSubmoduleConditions_DisabledSubmodule_ShowsRemoved(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	rr, mgr := newTestRR()

	moduleStatus := &modules.ModuleStatus{
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood"},
			{Type: "ModelsAsServiceReady", Status: metav1.ConditionTrue, Reason: "Ready"},
		},
	}

	submodules := []modules.SubmoduleCondition{
		{
			SourceConditionType: "ModelsAsServiceReady",
			DSCConditionType:    "ModelsAsServiceReady",
			StatusFieldName:     "ModelsAsAService",
			IsEnabled:           func(_ *modules.PlatformContext) bool { return false },
		},
	}

	var notReady []string
	modules.MirrorSubmoduleConditions(rr, newTestPlatformCtx(), moduleStatus, submodules, &notReady)

	cond := mgr.GetCondition("ModelsAsServiceReady")
	g.Expect(cond).ShouldNot(BeNil())
	g.Expect(cond.Status).Should(Equal(metav1.ConditionFalse))
	g.Expect(cond.Reason).Should(Equal(status.RemovedReason))
	g.Expect(cond.Message).Should(ContainSubstring("Removed"))
	g.Expect(notReady).Should(BeEmpty(), "disabled submodule should not count as not-ready")
}

func TestMirrorSubmoduleConditions_NilIsEnabled_AssumedEnabled(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	rr, mgr := newTestRR()

	moduleStatus := &modules.ModuleStatus{
		Conditions: []metav1.Condition{
			{Type: "FooReady", Status: metav1.ConditionTrue, Reason: "OK"},
		},
	}

	submodules := []modules.SubmoduleCondition{
		{
			SourceConditionType: "FooReady",
			DSCConditionType:    "FooReady",
			IsEnabled:           nil,
		},
	}

	var notReady []string
	modules.MirrorSubmoduleConditions(rr, newTestPlatformCtx(), moduleStatus, submodules, &notReady)

	cond := mgr.GetCondition("FooReady")
	g.Expect(cond).ShouldNot(BeNil())
	g.Expect(cond.Status).Should(Equal(metav1.ConditionTrue))
	g.Expect(notReady).Should(BeEmpty())
}

func TestWriteSubmoduleComponentStatus_SetsManaged(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	pCtx := newTestPlatformCtx()

	sm := modules.SubmoduleCondition{
		SourceConditionType: "ModelsAsServiceReady",
		DSCConditionType:    "ModelsAsServiceReady",
		StatusFieldName:     "ModelsAsAService",
	}

	modules.WriteSubmoduleComponentStatus(pCtx, sm, true)
	g.Expect(pCtx.DSC.Status.Components.ModelsAsAService.ManagementState).Should(
		Equal(operatorv1.Managed))
}

func TestWriteSubmoduleComponentStatus_SetsRemoved(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	pCtx := newTestPlatformCtx()

	sm := modules.SubmoduleCondition{
		SourceConditionType: "BatchGatewayReady",
		DSCConditionType:    "BatchGatewayReady",
		StatusFieldName:     "BatchGateway",
	}

	modules.WriteSubmoduleComponentStatus(pCtx, sm, false)
	g.Expect(pCtx.DSC.Status.Components.BatchGateway.ManagementState).Should(
		Equal(operatorv1.Removed))
}

func TestWriteSubmoduleComponentStatus_EmptyFieldName_NoOp(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	pCtx := newTestPlatformCtx()

	sm := modules.SubmoduleCondition{
		SourceConditionType: "FooReady",
		DSCConditionType:    "FooReady",
		StatusFieldName:     "",
	}

	modules.WriteSubmoduleComponentStatus(pCtx, sm, true)
	g.Expect(pCtx.DSC.Status.Components.ModelsAsAService.ManagementState).Should(
		Equal(operatorv1.ManagementState("")))
}

func TestWriteSubmoduleComponentStatus_NilDSC_NoOp(t *testing.T) {
	t.Parallel()

	pCtx := &modules.PlatformContext{DSC: nil}
	sm := modules.SubmoduleCondition{
		StatusFieldName: "ModelsAsAService",
	}

	// should not panic
	modules.WriteSubmoduleComponentStatus(pCtx, sm, true)
}

//nolint:testpackage
package modules

import (
	"context"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
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
func setupStatusTest(t *testing.T, handlers ...*statusTestHandler) (*odhtype.ReconciliationRequest, func()) {
	t.Helper()

	var condTypes []string
	for _, h := range handlers {
		condTypes = append(condTypes, h.GetGVK().Kind+status.ReadySuffix)
	}
	condTypes = append(condTypes, status.ConditionTypeModulesReady)

	return setupStatusTestWithCondTypes(t, condTypes, handlers...)
}

// setupStatusTestAggregateOnly creates a test reconciliation request
// with only aggregate ModulesReady declared (simulates Platform controller).
func setupStatusTestAggregateOnly(t *testing.T, handlers ...*statusTestHandler) (*odhtype.ReconciliationRequest, func()) {
	t.Helper()
	return setupStatusTestWithCondTypes(t, []string{status.ConditionTypeModulesReady}, handlers...)
}

func setupStatusTestWithCondTypes(t *testing.T, condTypes []string, handlers ...*statusTestHandler) (*odhtype.ReconciliationRequest, func()) {
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

	cm := conditions.NewManager(platform, status.ConditionTypeReady, condTypes...)

	rr := &odhtype.ReconciliationRequest{
		Client:     cli,
		Instance:   platform,
		Conditions: cm,
		Release:    common.Release{Name: cluster.OpenDataHub},
	}

	cleanup := func() { r = oldR }

	return rr, cleanup
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
	h1.statusErr = fmt.Errorf("CR not found")

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

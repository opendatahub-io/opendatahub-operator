package conditions_test

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"

	. "github.com/onsi/gomega"
)

const (
	readyCondition       = "Ready"
	dependency1Condition = "Dependency1"
	dependency2Condition = "Dependency2"
)

type fakeAccessor struct {
	conditions []common.Condition
}

func (f *fakeAccessor) GetConditions() []common.Condition {
	return f.conditions
}

func (f *fakeAccessor) SetConditions(values []common.Condition) {
	f.conditions = values
}

func TestManager_InitializeConditions(t *testing.T) {
	g := NewWithT(t)

	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, dependency1Condition, dependency2Condition)

	g.Expect(accessor.GetConditions()).To(HaveLen(3))
	g.Expect(manager.GetCondition(readyCondition)).NotTo(BeNil())
	g.Expect(manager.GetCondition(readyCondition).Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(manager.GetCondition(dependency1Condition)).NotTo(BeNil())
	g.Expect(manager.GetCondition(dependency2Condition)).NotTo(BeNil())
}

func TestManager_IsHappy(t *testing.T) {
	g := NewWithT(t)

	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, dependency1Condition, dependency2Condition)

	g.Expect(manager.IsHappy()).To(BeFalse())

	manager.MarkFalse(dependency1Condition)
	manager.MarkFalse(dependency2Condition)

	g.Expect(manager.IsHappy()).To(BeFalse())

	manager.MarkTrue(dependency1Condition)
	g.Expect(manager.IsHappy()).To(BeFalse())

	manager.MarkTrue(dependency2Condition)
	g.Expect(manager.IsHappy()).To(BeTrue())
}

func TestManager_IsHappy_NoDependents(t *testing.T) {
	g := NewWithT(t)

	accessor := &fakeAccessor{}
	accessor.SetConditions([]common.Condition{
		{Type: dependency1Condition, Status: metav1.ConditionUnknown},
		{Type: dependency2Condition, Status: metav1.ConditionUnknown},
	})

	manager := conditions.NewManager(accessor, readyCondition)
	g.Expect(manager.IsHappy()).To(BeFalse())

	manager.MarkFalse(dependency1Condition)
	g.Expect(manager.IsHappy()).To(BeFalse())

	manager.MarkTrue(dependency1Condition)
	g.Expect(manager.IsHappy()).To(BeFalse())

	manager.MarkFalse(dependency2Condition)
	g.Expect(manager.IsHappy()).To(BeFalse())

	manager.MarkTrue(dependency2Condition)
	g.Expect(manager.IsHappy()).To(BeTrue())
}

func TestManager_SetAndClearCondition(t *testing.T) {
	g := NewWithT(t)

	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, dependency1Condition)

	manager.MarkTrue(dependency1Condition)
	g.Expect(manager.GetCondition(dependency1Condition)).NotTo(BeNil())
	g.Expect(manager.GetCondition(dependency1Condition).Status).To(Equal(metav1.ConditionTrue))

	err := manager.ClearCondition(dependency1Condition)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(manager.GetCondition(dependency1Condition)).To(BeNil())
}

func TestManager_RecomputeHappiness(t *testing.T) {
	g := NewWithT(t)

	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, dependency1Condition, dependency2Condition)

	manager.MarkTrue(dependency1Condition)
	manager.MarkFalse(dependency2Condition, conditions.WithSeverity(common.ConditionSeverityError))
	g.Expect(manager.IsHappy()).To(BeFalse())
	g.Expect(manager.GetTopLevelCondition().Status).To(Equal(metav1.ConditionFalse))

	manager.MarkTrue(dependency2Condition)
	g.Expect(manager.IsHappy()).To(BeTrue())
}

func TestManager_ResetPreservesConditions(t *testing.T) {
	g := NewWithT(t)

	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, dependency1Condition, dependency2Condition)

	manager.MarkTrue(dependency1Condition)
	manager.MarkTrue(dependency2Condition)
	g.Expect(accessor.GetConditions()).To(HaveLen(3))

	manager.Reset()

	g.Expect(accessor.GetConditions()).To(HaveLen(3))
	g.Expect(manager.GetCondition(readyCondition)).NotTo(BeNil())
	g.Expect(manager.GetCondition(dependency1Condition)).NotTo(BeNil())
	g.Expect(manager.GetCondition(dependency2Condition)).NotTo(BeNil())
}

func TestManager_CleanupStaleConditions(t *testing.T) {
	g := NewWithT(t)

	// Simulate a component that was previously enabled (dependency2 condition exists
	// in the accessor) but is now disabled (not in the dependents list). This mirrors
	// production where the Manager is recreated each reconcile cycle with only the
	// currently-enabled components as dependents.
	accessor := &fakeAccessor{}
	accessor.SetConditions([]common.Condition{
		{Type: dependency1Condition, Status: metav1.ConditionTrue},
		{Type: dependency2Condition, Status: metav1.ConditionTrue},
	})

	manager := conditions.NewManager(accessor, readyCondition, dependency1Condition)

	manager.Reset()

	manager.MarkTrue(dependency1Condition)

	manager.CleanupStaleConditions()

	g.Expect(manager.GetCondition(dependency1Condition)).NotTo(BeNil())
	g.Expect(manager.GetCondition(dependency2Condition)).To(BeNil())
	g.Expect(manager.GetCondition(readyCondition)).NotTo(BeNil())
}

func TestManager_CleanupStaleConditionsPreservesHappy(t *testing.T) {
	g := NewWithT(t)

	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, dependency1Condition)

	manager.MarkTrue(dependency1Condition)
	g.Expect(manager.IsHappy()).To(BeTrue())

	manager.Reset()

	manager.MarkTrue(dependency1Condition)

	manager.CleanupStaleConditions()

	g.Expect(manager.GetCondition(readyCondition)).NotTo(BeNil())
	g.Expect(manager.IsHappy()).To(BeTrue())
}

func TestManager_TimestampPreservedWhenConditionUnchanged(t *testing.T) {
	g := NewWithT(t)

	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, dependency1Condition)

	manager.MarkTrue(dependency1Condition, conditions.WithReason("TestReason"), conditions.WithMessage("test message"))

	originalCondition := manager.GetCondition(dependency1Condition)
	g.Expect(originalCondition).NotTo(BeNil())
	originalTime := originalCondition.LastTransitionTime

	time.Sleep(time.Millisecond) // ensure clock advances so any regression produces different timestamps

	manager.Reset()

	manager.MarkTrue(dependency1Condition, conditions.WithReason("TestReason"), conditions.WithMessage("test message"))

	updatedCondition := manager.GetCondition(dependency1Condition)
	g.Expect(updatedCondition).NotTo(BeNil())
	g.Expect(updatedCondition.LastTransitionTime).To(Equal(originalTime))
}

func TestManager_CleanupStaleConditionsRecomputesHappiness(t *testing.T) {
	g := NewWithT(t)

	// Simulate a component that was previously enabled and unhappy (dependency2 is
	// False with Error severity) but is now disabled (not in the dependents list).
	// After cleanup, the stale unhappy condition should be removed and happiness
	// should be recomputed to True.
	accessor := &fakeAccessor{}
	accessor.SetConditions([]common.Condition{
		{Type: dependency1Condition, Status: metav1.ConditionTrue},
		{
			Type:     dependency2Condition,
			Status:   metav1.ConditionFalse,
			Reason:   "Broken",
			Message:  "something failed",
			Severity: common.ConditionSeverityError,
		},
	})

	manager := conditions.NewManager(accessor, readyCondition, dependency1Condition)
	g.Expect(manager.IsHappy()).To(BeFalse())

	manager.Reset()

	manager.MarkTrue(dependency1Condition)

	manager.CleanupStaleConditions()

	g.Expect(manager.GetCondition(dependency2Condition)).To(BeNil())
	g.Expect(manager.GetCondition(readyCondition)).NotTo(BeNil())
	g.Expect(manager.IsHappy()).To(BeTrue())
}

func TestManager_CleanupStaleConditionsNoopWithoutReset(t *testing.T) {
	g := NewWithT(t)

	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, dependency1Condition, dependency2Condition)

	manager.MarkTrue(dependency1Condition)
	manager.MarkTrue(dependency2Condition)
	g.Expect(accessor.GetConditions()).To(HaveLen(3))

	manager.CleanupStaleConditions()

	g.Expect(accessor.GetConditions()).To(HaveLen(3))
}

func TestManager_PreserveTypeProtectsFromCleanup(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	externalCondition := "ModulesReady"

	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, dependency1Condition)

	manager.MarkTrue(dependency1Condition)
	manager.SetCondition(common.Condition{
		Type:   externalCondition,
		Status: metav1.ConditionTrue,
		Reason: "NoRegisteredModules",
	})
	g.Expect(manager.IsHappy()).To(BeTrue())

	manager.Reset()
	manager.MarkTrue(dependency1Condition)

	manager.PreserveType(externalCondition)
	manager.CleanupStaleConditions()

	g.Expect(manager.GetCondition(dependency1Condition)).NotTo(BeNil())
	g.Expect(manager.GetCondition(externalCondition)).NotTo(BeNil(), "preserved external condition should survive cleanup")
	g.Expect(manager.GetCondition(externalCondition).Reason).To(Equal("NoRegisteredModules"))
	g.Expect(manager.GetCondition(readyCondition)).NotTo(BeNil())
	g.Expect(manager.IsHappy()).To(BeTrue())
}

func TestManager_PreserveTypeNoopBeforeReset(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, dependency1Condition)

	manager.PreserveType("SomeCondition")

	g.Expect(manager.GetCondition("SomeCondition")).To(BeNil(), "PreserveType before Reset should be a no-op")
}

// TestManager_UnsetDependentsDoNotBlockHappiness reproduces the ModelController
// e2e scenario: 3 dependent conditions are registered (DeploymentsAvailable,
// DependenciesAvailable, LLMDWVADependencies), but only DeploymentsAvailable
// is ever set by an action. The other two are initialized as Unknown by
// NewManager and never touched. They must not block Ready=True.
func TestManager_UnsetDependentsDoNotBlockHappiness(t *testing.T) {
	g := NewWithT(t)

	deploymentsAvailable := "DeploymentsAvailable"
	dependenciesAvailable := "DependenciesAvailable"
	llmdWVA := "LLM-D-WVADependencies"

	// First reconcile cycle: fresh object, no pre-existing conditions.
	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, deploymentsAvailable, dependenciesAvailable, llmdWVA)

	manager.Reset()

	// Only the deployments action sets its condition.
	// No precondition or action sets DependenciesAvailable or LLM-D-WVADependencies.
	manager.MarkTrue(deploymentsAvailable)

	manager.CleanupStaleConditions()
	manager.RecomputeHappiness("")

	g.Expect(manager.IsHappy()).To(BeTrue(), "Ready should be True when only unset dependents remain")
	g.Expect(manager.GetCondition(dependenciesAvailable)).To(BeNil(), "unset dependent should be cleaned up")
	g.Expect(manager.GetCondition(llmdWVA)).To(BeNil(), "unset dependent should be cleaned up")

	// Second reconcile cycle: simulate re-creating the Manager with persisted status.
	// The object now has Ready=True, DeploymentsAvailable=True from last apply.
	manager2 := conditions.NewManager(accessor, readyCondition, deploymentsAvailable, dependenciesAvailable, llmdWVA)

	manager2.Reset()

	manager2.MarkTrue(deploymentsAvailable)

	manager2.CleanupStaleConditions()
	manager2.RecomputeHappiness("")

	g.Expect(manager2.IsHappy()).To(BeTrue(), "Ready should remain True on subsequent cycles")
	g.Expect(manager2.GetCondition(dependenciesAvailable)).To(BeNil())
	g.Expect(manager2.GetCondition(llmdWVA)).To(BeNil())
}

func TestManager_Sort(t *testing.T) {
	g := NewWithT(t)

	accessor := &fakeAccessor{conditions: make([]common.Condition, 0)}

	manager := conditions.NewManager(accessor, "Z", "A", "C")
	manager.MarkTrue("B")
	manager.MarkTrue("D")
	manager.MarkTrue("E")
	manager.Sort()

	result := make([]string, 0, len(accessor.conditions))
	for _, c := range accessor.conditions {
		result = append(result, c.Type)
	}

	g.Expect(result).To(HaveExactElements(
		"Z",
		"A",
		"C",
		"B",
		"D",
		"E",
	))
}

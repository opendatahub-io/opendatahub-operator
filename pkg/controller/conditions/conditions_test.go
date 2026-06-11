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
	readyCondition        = "Ready"
	dependency1Condition  = "Dependency1"
	dependency2Condition  = "Dependency2"
	deploymentsAvailable  = "DeploymentsAvailable"
	dependenciesAvailable = "DependenciesAvailable"
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

// TestManager_UnsetDependentsDoNotBlockHappiness reproduces the ModelController
// e2e scenario: 3 dependent conditions are registered (DeploymentsAvailable,
// DependenciesAvailable, LLMDWVADependencies), but only DeploymentsAvailable
// is ever set by an action. The other two are initialized as Unknown by
// NewManager and never touched. They must not block Ready=True.
func TestManager_UnsetDependentsBlockHappiness(t *testing.T) {
	g := NewWithT(t)

	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, deploymentsAvailable, dependenciesAvailable)

	manager.Reset()

	// Only DeploymentsAvailable is set. DependenciesAvailable is not.
	manager.MarkTrue(deploymentsAvailable)

	manager.CleanupStaleConditions()
	manager.RecomputeHappiness("")

	// Unset dependent should be marked False (not removed), blocking happiness.
	g.Expect(manager.IsHappy()).To(BeFalse(), "Ready must be False when a declared dependent was not set")

	cond := manager.GetCondition(dependenciesAvailable)
	g.Expect(cond).NotTo(BeNil(), "unset dependent must not be removed")
	g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(cond.Reason).To(Equal(conditions.ConditionReasonNotSet))
}

func TestManager_AllDependentsSetAllowsHappiness(t *testing.T) {
	g := NewWithT(t)

	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, deploymentsAvailable, dependenciesAvailable)

	manager.Reset()

	manager.MarkTrue(deploymentsAvailable)
	manager.MarkTrue(dependenciesAvailable)

	manager.CleanupStaleConditions()
	manager.RecomputeHappiness("")

	g.Expect(manager.IsHappy()).To(BeTrue(), "Ready should be True when all dependents are set")
}

func TestManager_NonDependentStaleConditionRemoved(t *testing.T) {
	g := NewWithT(t)

	accessor := &fakeAccessor{}
	accessor.SetConditions([]common.Condition{
		{Type: dependency1Condition, Status: metav1.ConditionTrue},
		{Type: "OrphanedCondition", Status: metav1.ConditionTrue},
	})

	manager := conditions.NewManager(accessor, readyCondition, dependency1Condition)

	manager.Reset()
	manager.MarkTrue(dependency1Condition)

	manager.CleanupStaleConditions()

	g.Expect(manager.GetCondition(dependency1Condition)).NotTo(BeNil())
	g.Expect(manager.GetCondition("OrphanedCondition")).To(BeNil(), "non-dependent stale condition should be removed")
	g.Expect(manager.IsHappy()).To(BeTrue())
}

func TestManager_UnsetDependentRecoversOnNextCycle(t *testing.T) {
	g := NewWithT(t)

	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, deploymentsAvailable)

	// First cycle: dependent not set
	manager.Reset()
	manager.CleanupStaleConditions()
	manager.RecomputeHappiness("")

	g.Expect(manager.IsHappy()).To(BeFalse())
	cond := manager.GetCondition(deploymentsAvailable)
	g.Expect(cond).NotTo(BeNil())
	g.Expect(cond.Reason).To(Equal(conditions.ConditionReasonNotSet))

	// Second cycle: dependent is set, should recover
	manager2 := conditions.NewManager(accessor, readyCondition, deploymentsAvailable)
	manager2.Reset()
	manager2.MarkTrue(deploymentsAvailable)

	manager2.CleanupStaleConditions()
	manager2.RecomputeHappiness("")

	g.Expect(manager2.IsHappy()).To(BeTrue(), "should recover when dependent is set on next cycle")
}

func TestManager_MultipleDependentsPartiallySet(t *testing.T) {
	g := NewWithT(t)

	condA := "CondA"
	condB := "CondB"
	condC := "CondC"

	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, condA, condB, condC)

	manager.Reset()

	manager.MarkTrue(condA)
	// condB not set
	manager.MarkTrue(condC)

	manager.CleanupStaleConditions()
	manager.RecomputeHappiness("")

	g.Expect(manager.IsHappy()).To(BeFalse(), "should be unhappy when any dependent is missing")

	g.Expect(manager.GetCondition(condA).Status).To(Equal(metav1.ConditionTrue))
	g.Expect(manager.GetCondition(condB).Status).To(Equal(metav1.ConditionFalse))
	g.Expect(manager.GetCondition(condB).Reason).To(Equal(conditions.ConditionReasonNotSet))
	g.Expect(manager.GetCondition(condC).Status).To(Equal(metav1.ConditionTrue))
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

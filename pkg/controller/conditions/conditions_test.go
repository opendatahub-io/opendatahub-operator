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

func TestManager_ResetPreservesLastTransitionTime(t *testing.T) {
	g := NewWithT(t)

	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, dependency1Condition, dependency2Condition)

	manager.MarkTrue(dependency1Condition)
	manager.MarkTrue(dependency2Condition)
	g.Expect(manager.IsHappy()).To(BeTrue())

	dep1Before := manager.GetCondition(dependency1Condition)
	dep2Before := manager.GetCondition(dependency2Condition)
	readyBefore := manager.GetCondition(readyCondition)
	g.Expect(dep1Before.LastTransitionTime.IsZero()).To(BeFalse())
	g.Expect(dep2Before.LastTransitionTime.IsZero()).To(BeFalse())
	g.Expect(readyBefore.LastTransitionTime.IsZero()).To(BeFalse())

	time.Sleep(10 * time.Millisecond)

	manager.Reset()
	g.Expect(accessor.GetConditions()).To(BeEmpty())

	manager.MarkTrue(dependency1Condition)
	manager.MarkTrue(dependency2Condition)

	dep1After := manager.GetCondition(dependency1Condition)
	dep2After := manager.GetCondition(dependency2Condition)
	readyAfter := manager.GetCondition(readyCondition)

	g.Expect(dep1After.LastTransitionTime).To(Equal(dep1Before.LastTransitionTime))
	g.Expect(dep2After.LastTransitionTime).To(Equal(dep2Before.LastTransitionTime))
	g.Expect(readyAfter.LastTransitionTime).To(Equal(readyBefore.LastTransitionTime))
}

func TestManager_ResetUpdatesLastTransitionTimeOnStatusChange(t *testing.T) {
	g := NewWithT(t)

	accessor := &fakeAccessor{}
	manager := conditions.NewManager(accessor, readyCondition, dependency1Condition, dependency2Condition)

	manager.MarkTrue(dependency1Condition)
	manager.MarkTrue(dependency2Condition)

	dep1Before := manager.GetCondition(dependency1Condition)
	dep2Before := manager.GetCondition(dependency2Condition)
	g.Expect(dep1Before.LastTransitionTime.IsZero()).To(BeFalse())
	g.Expect(dep2Before.LastTransitionTime.IsZero()).To(BeFalse())

	time.Sleep(10 * time.Millisecond)

	manager.Reset()

	manager.MarkFalse(dependency1Condition, conditions.WithSeverity(common.ConditionSeverityError))
	manager.MarkTrue(dependency2Condition)

	dep1After := manager.GetCondition(dependency1Condition)
	dep2After := manager.GetCondition(dependency2Condition)

	g.Expect(dep1After.LastTransitionTime).NotTo(Equal(dep1Before.LastTransitionTime))
	g.Expect(dep2After.LastTransitionTime).To(Equal(dep2Before.LastTransitionTime))
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

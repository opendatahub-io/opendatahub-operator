//nolint:testpackage
package precondition

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

func TestCustom_Defaults(t *testing.T) {
	g := NewWithT(t)

	pc := Custom(passingCheck)

	g.Expect(pc.check).NotTo(BeNil())
	g.Expect(pc.conditionType).To(Equal(status.ConditionDependenciesAvailable))
	g.Expect(pc.severity).To(Equal(common.ConditionSeverityError))
	g.Expect(pc.stopReconciliation).To(BeFalse())
}

func TestCustom_PassingCheck(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	shouldStop := RunAll(t.Context(), rr, []PreCondition{
		Custom(passingCheck),
	})

	g.Expect(shouldStop).To(BeFalse())

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionTrue))
}

func TestCustom_FailingCheck(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	shouldStop := RunAll(t.Context(), rr, []PreCondition{
		Custom(failingCheck("component not ready")),
	})

	g.Expect(shouldStop).To(BeFalse())

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(got.Message).To(ContainSubstring("component not ready"))
}

func TestCustom_ErrorCheck(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	shouldStop := RunAll(t.Context(), rr, []PreCondition{
		Custom(errorCheck),
	})

	g.Expect(shouldStop).To(BeFalse())

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionUnknown))
}

func TestCustom_WithStopReconciliation(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	shouldStop := RunAll(t.Context(), rr, []PreCondition{
		Custom(failingCheck("must stop"), WithStopReconciliation()),
	})

	g.Expect(shouldStop).To(BeTrue())

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
}

func TestCustom_AllOptions(t *testing.T) {
	g := NewWithT(t)

	customCondition := "CustomCheck"
	rr := newRR(customCondition)
	shouldStop := RunAll(t.Context(), rr, []PreCondition{
		Custom(
			failingCheck("original"),
			WithConditionType(customCondition),
			WithSeverity(common.ConditionSeverityInfo),
			WithStopReconciliation(),
			WithMessage("overridden message"),
		),
	})

	g.Expect(shouldStop).To(BeTrue())

	got := rr.Conditions.GetCondition(customCondition)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(got.Severity).To(Equal(common.ConditionSeverityInfo))
	g.Expect(got.Message).To(ContainSubstring("overridden message"))
	g.Expect(got.Message).NotTo(ContainSubstring("original"))
}

func TestCustom_ErrorWithStopReconciliation(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	shouldStop := RunAll(t.Context(), rr, []PreCondition{
		Custom(errorCheck, WithStopReconciliation()),
	})

	g.Expect(shouldStop).To(BeTrue())

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionUnknown))
}

func TestCustom_AccessesInstance(t *testing.T) {
	g := NewWithT(t)

	instanceCheck := func(_ context.Context, rr *types.ReconciliationRequest) (CheckResult, error) {
		if rr.Instance == nil {
			return CheckResult{}, errors.New("instance is nil")
		}
		if rr.Instance.GetName() == "" {
			return CheckResult{Pass: false, Message: "instance has no name"}, nil
		}
		return CheckResult{Pass: true}, nil
	}

	rr := newRR(status.ConditionDependenciesAvailable)
	shouldStop := RunAll(t.Context(), rr, []PreCondition{
		Custom(instanceCheck),
	})

	g.Expect(shouldStop).To(BeFalse())

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionTrue))
}

func TestCustom_NilCheck(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	shouldStop := RunAll(t.Context(), rr, []PreCondition{
		Custom(nil),
	})

	g.Expect(shouldStop).To(BeFalse())

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(got.Message).To(ContainSubstring("precondition check function is nil"))
}

func TestCustom_NilCheckWithStopReconciliation(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	shouldStop := RunAll(t.Context(), rr, []PreCondition{
		Custom(nil, WithStopReconciliation()),
	})

	g.Expect(shouldStop).To(BeTrue())

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionUnknown))
}

func TestCustom_SkippedOnClusterType(t *testing.T) {
	g := NewWithT(t)

	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

	rr := newRR(status.ConditionDependenciesAvailable)
	shouldStop := RunAll(t.Context(), rr, []PreCondition{
		Custom(failingCheck("should not run"), WithClusterTypes(cluster.ClusterTypeKubernetes)),
	})

	g.Expect(shouldStop).To(BeFalse())

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).NotTo(Equal(metav1.ConditionFalse))
}

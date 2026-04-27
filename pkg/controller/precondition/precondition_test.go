package precondition_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/xid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/precondition"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

var errTest = errors.New("test error")

func passingCheck(_ context.Context, _ *types.ReconciliationRequest) (precondition.CheckResult, error) {
	return precondition.CheckResult{Pass: true}, nil
}

func failingCheck(msg string) precondition.CheckFn {
	return func(_ context.Context, _ *types.ReconciliationRequest) (precondition.CheckResult, error) {
		return precondition.CheckResult{Pass: false, Message: msg}, nil
	}
}

func errorCheck(_ context.Context, _ *types.ReconciliationRequest) (precondition.CheckResult, error) {
	return precondition.CheckResult{}, errTest
}

func newRR(conditionTypes ...string) *types.ReconciliationRequest {
	instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}

	return &types.ReconciliationRequest{
		Instance:   instance,
		Conditions: cond.NewManager(instance, status.ConditionTypeReady, conditionTypes...),
	}
}

func TestNew_Defaults(t *testing.T) {
	g := NewWithT(t)

	pc := precondition.New(passingCheck)

	g.Expect(pc.Check).NotTo(BeNil())
	g.Expect(pc.ConditionType).To(Equal(status.ConditionDependenciesAvailable))
	g.Expect(pc.Severity).To(Equal(common.ConditionSeverityError))
	g.Expect(pc.StopReconciliation).To(BeFalse())
	g.Expect(pc.ClusterTypes).To(BeNil())
	g.Expect(pc.Message).To(BeEmpty())
}

func TestNew_WithConditionType(t *testing.T) {
	g := NewWithT(t)

	pc := precondition.New(passingCheck, precondition.WithConditionType("CustomCondition"))

	g.Expect(pc.ConditionType).To(Equal("CustomCondition"))
}

func TestNew_WithSeverity(t *testing.T) {
	g := NewWithT(t)

	pc := precondition.New(passingCheck, precondition.WithSeverity(common.ConditionSeverityInfo))

	g.Expect(pc.Severity).To(Equal(common.ConditionSeverityInfo))
}

func TestNew_WithStopReconciliation(t *testing.T) {
	g := NewWithT(t)

	pc := precondition.New(passingCheck, precondition.WithStopReconciliation())

	g.Expect(pc.StopReconciliation).To(BeTrue())
}

func TestNew_WithClusterTypes(t *testing.T) {
	g := NewWithT(t)

	pc := precondition.New(passingCheck, precondition.WithClusterTypes(cluster.ClusterTypeOpenShift, cluster.ClusterTypeKubernetes))

	g.Expect(pc.ClusterTypes).To(Equal([]string{cluster.ClusterTypeOpenShift, cluster.ClusterTypeKubernetes}))
}

func TestNew_WithMessage(t *testing.T) {
	g := NewWithT(t)

	pc := precondition.New(passingCheck, precondition.WithMessage("custom message"))

	g.Expect(pc.Message).To(Equal("custom message"))
}

func TestNew_MultipleOptions(t *testing.T) {
	g := NewWithT(t)

	pc := precondition.New(
		passingCheck,
		precondition.WithConditionType("Custom"),
		precondition.WithSeverity(common.ConditionSeverityInfo),
		precondition.WithStopReconciliation(),
		precondition.WithClusterTypes(cluster.ClusterTypeOpenShift),
		precondition.WithMessage("guidance"),
	)

	g.Expect(pc.ConditionType).To(Equal("Custom"))
	g.Expect(pc.Severity).To(Equal(common.ConditionSeverityInfo))
	g.Expect(pc.StopReconciliation).To(BeTrue())
	g.Expect(pc.ClusterTypes).To(Equal([]string{cluster.ClusterTypeOpenShift}))
	g.Expect(pc.Message).To(Equal("guidance"))
}

func TestRunAll_EmptyList(t *testing.T) {
	g := NewWithT(t)

	rr := newRR()
	shouldStop := precondition.RunAll(t.Context(), rr, nil)

	g.Expect(shouldStop).To(BeFalse())
}

func TestRunAll_AllPass(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	pcs := []precondition.PreCondition{
		precondition.New(passingCheck),
		precondition.New(passingCheck),
	}

	shouldStop := precondition.RunAll(t.Context(), rr, pcs)

	g.Expect(shouldStop).To(BeFalse())

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionTrue))
	g.Expect(got.Reason).To(BeEmpty())
	g.Expect(got.Message).To(BeEmpty())
}

func TestRunAll_OneFailsNoStop(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	pcs := []precondition.PreCondition{
		precondition.New(passingCheck),
		precondition.New(failingCheck("CRD missing")),
	}

	shouldStop := precondition.RunAll(t.Context(), rr, pcs)

	g.Expect(shouldStop).To(BeFalse())

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(got.Reason).To(Equal("PreConditionFailed"))
	g.Expect(got.Message).To(ContainSubstring("CRD missing"))
}

func TestRunAll_OneFailsWithStop(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	pcs := []precondition.PreCondition{
		precondition.New(passingCheck),
		precondition.New(failingCheck("CRD missing"), precondition.WithStopReconciliation()),
	}

	shouldStop := precondition.RunAll(t.Context(), rr, pcs)

	g.Expect(shouldStop).To(BeTrue())

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
}

func TestRunAll_CheckError_Unknown(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	pcs := []precondition.PreCondition{
		precondition.New(errorCheck),
	}

	shouldStop := precondition.RunAll(t.Context(), rr, pcs)

	g.Expect(shouldStop).To(BeFalse())

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(got.Reason).To(Equal("PreConditionFailed"))
	g.Expect(got.Severity).To(Equal(common.ConditionSeverityError))
	g.Expect(got.Message).To(ContainSubstring("test error"))
}

func TestRunAll_CheckErrorWithStop(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	pcs := []precondition.PreCondition{
		precondition.New(errorCheck, precondition.WithStopReconciliation()),
	}

	shouldStop := precondition.RunAll(t.Context(), rr, pcs)

	g.Expect(shouldStop).To(BeTrue())

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionUnknown))
}

func TestRunAll_MixedUnknownAndFailed_FalseWins(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	pcs := []precondition.PreCondition{
		precondition.New(failingCheck("CRD missing")),
		precondition.New(errorCheck),
	}

	shouldStop := precondition.RunAll(t.Context(), rr, pcs)

	g.Expect(shouldStop).To(BeFalse())

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionFalse))
}

func TestRunAll_AggregatesMessages(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	pcs := []precondition.PreCondition{
		precondition.New(failingCheck("CRD A missing")),
		precondition.New(failingCheck("CRD B missing")),
	}

	precondition.RunAll(t.Context(), rr, pcs)

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Message).To(ContainSubstring("CRD A missing"))
	g.Expect(got.Message).To(ContainSubstring("CRD B missing"))
}

func TestRunAll_SeverityAggregation_ErrorIfAny(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	pcs := []precondition.PreCondition{
		precondition.New(failingCheck("info dep"), precondition.WithSeverity(common.ConditionSeverityInfo)),
		precondition.New(failingCheck("error dep")),
	}

	precondition.RunAll(t.Context(), rr, pcs)

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Severity).To(Equal(common.ConditionSeverityError))
}

func TestRunAll_SeverityAggregation_InfoIfAllInfo(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	pcs := []precondition.PreCondition{
		precondition.New(failingCheck("info dep 1"), precondition.WithSeverity(common.ConditionSeverityInfo)),
		precondition.New(failingCheck("info dep 2"), precondition.WithSeverity(common.ConditionSeverityInfo)),
	}

	precondition.RunAll(t.Context(), rr, pcs)

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Severity).To(Equal(common.ConditionSeverityInfo))
}

func TestRunAll_CustomMessageOverridesResult(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	pcs := []precondition.PreCondition{
		precondition.New(failingCheck("original msg"), precondition.WithMessage("custom guidance")),
	}

	precondition.RunAll(t.Context(), rr, pcs)

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Message).To(ContainSubstring("custom guidance"))
	g.Expect(got.Message).NotTo(ContainSubstring("original msg"))
}

func TestRunAll_ClusterTypeFiltering(t *testing.T) {
	g := NewWithT(t)

	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

	rr := newRR(status.ConditionDependenciesAvailable)
	pcs := []precondition.PreCondition{
		precondition.New(
			failingCheck("k8s only check"),
			precondition.WithClusterTypes(cluster.ClusterTypeKubernetes),
			precondition.WithStopReconciliation(),
		),
	}

	shouldStop := precondition.RunAll(t.Context(), rr, pcs)

	g.Expect(shouldStop).To(BeFalse())
	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).NotTo(Equal(metav1.ConditionFalse))
}

func TestRunAll_MultipleConditionTypes(t *testing.T) {
	g := NewWithT(t)

	customCondition := "CustomDeps"
	rr := newRR(status.ConditionDependenciesAvailable, customCondition)

	pcs := []precondition.PreCondition{
		precondition.New(passingCheck),
		precondition.New(failingCheck("custom failed"), precondition.WithConditionType(customCondition)),
	}

	precondition.RunAll(t.Context(), rr, pcs)

	defaultCond := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(defaultCond).NotTo(BeNil())
	g.Expect(defaultCond.Status).To(Equal(metav1.ConditionTrue))

	customCond := rr.Conditions.GetCondition(customCondition)
	g.Expect(customCond).NotTo(BeNil())
	g.Expect(customCond.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(customCond.Message).To(ContainSubstring("custom failed"))
}

func TestRunAll_NilCheck_HonorsSeverityAndStop(t *testing.T) {
	g := NewWithT(t)

	rr := newRR(status.ConditionDependenciesAvailable)
	pcs := []precondition.PreCondition{
		{
			ConditionType:      status.ConditionDependenciesAvailable,
			Severity:           common.ConditionSeverityError,
			StopReconciliation: true,
		},
	}

	shouldStop := precondition.RunAll(t.Context(), rr, pcs)

	g.Expect(shouldStop).To(BeTrue())

	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(got.Severity).To(Equal(common.ConditionSeverityError))
	g.Expect(got.Message).To(ContainSubstring("precondition check function is nil"))
}

func TestRunAll_AllPreconditionsRunEvenWhenSomeFail(t *testing.T) {
	g := NewWithT(t)

	callCount := 0
	countingCheck := func(_ context.Context, _ *types.ReconciliationRequest) (precondition.CheckResult, error) {
		callCount++
		return precondition.CheckResult{Pass: false, Message: "fail"}, nil
	}

	rr := newRR(status.ConditionDependenciesAvailable)
	pcs := []precondition.PreCondition{
		precondition.New(countingCheck, precondition.WithStopReconciliation()),
		precondition.New(countingCheck, precondition.WithStopReconciliation()),
		precondition.New(countingCheck, precondition.WithStopReconciliation()),
	}

	precondition.RunAll(t.Context(), rr, pcs)

	g.Expect(callCount).To(Equal(3))
}

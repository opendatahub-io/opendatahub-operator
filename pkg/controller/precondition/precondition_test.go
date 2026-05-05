//nolint:testpackage
package precondition

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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

var errTest = errors.New("test error")

func passingCheck(_ context.Context, _ *types.ReconciliationRequest) (CheckResult, error) {
	return CheckResult{Pass: true}, nil
}

func failingCheck(msg string) CheckFunc {
	return func(_ context.Context, _ *types.ReconciliationRequest) (CheckResult, error) {
		return CheckResult{Pass: false, Message: msg}, nil
	}
}

func errorCheck(_ context.Context, _ *types.ReconciliationRequest) (CheckResult, error) {
	return CheckResult{}, errTest
}

func newRR(conditionTypes ...string) *types.ReconciliationRequest {
	instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}

	return &types.ReconciliationRequest{
		Instance:   instance,
		Conditions: cond.NewManager(instance, status.ConditionTypeReady, conditionTypes...),
	}
}

func Test_newPreCondition_Defaults(t *testing.T) {
	g := NewWithT(t)

	pc := newPreCondition(passingCheck)

	g.Expect(pc.check).NotTo(BeNil())
	g.Expect(pc.conditionType).To(Equal(status.ConditionDependenciesAvailable))
	g.Expect(pc.severity).To(Equal(common.ConditionSeverityError))
	g.Expect(pc.stopReconciliation).To(BeFalse())
	g.Expect(pc.clusterTypes).To(BeNil())
	g.Expect(pc.message).To(BeEmpty())
}

func Test_newPreCondition_Options(t *testing.T) {
	tests := []struct {
		name   string
		opt    Option
		assert func(g Gomega, pc PreCondition)
	}{
		{
			name: "WithConditionType sets condition type",
			opt:  WithConditionType("CustomCondition"),
			assert: func(g Gomega, pc PreCondition) {
				g.Expect(pc.conditionType).To(Equal("CustomCondition"))
			},
		},
		{
			name: "WithConditionType empty string preserves default",
			opt:  WithConditionType(""),
			assert: func(g Gomega, pc PreCondition) {
				g.Expect(pc.conditionType).To(Equal(status.ConditionDependenciesAvailable))
			},
		},
		{
			name: "WithSeverity",
			opt:  WithSeverity(common.ConditionSeverityInfo),
			assert: func(g Gomega, pc PreCondition) {
				g.Expect(pc.severity).To(Equal(common.ConditionSeverityInfo))
			},
		},
		{
			name: "WithStopReconciliation",
			opt:  WithStopReconciliation(),
			assert: func(g Gomega, pc PreCondition) {
				g.Expect(pc.stopReconciliation).To(BeTrue())
			},
		},
		{
			name: "WithClusterTypes",
			opt:  WithClusterTypes(cluster.ClusterTypeOpenShift, cluster.ClusterTypeKubernetes),
			assert: func(g Gomega, pc PreCondition) {
				g.Expect(pc.clusterTypes).To(Equal([]string{cluster.ClusterTypeOpenShift, cluster.ClusterTypeKubernetes}))
			},
		},
		{
			name: "WithMessage",
			opt:  WithMessage("custom message"),
			assert: func(g Gomega, pc PreCondition) {
				g.Expect(pc.message).To(Equal("custom message"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			pc := newPreCondition(passingCheck, tt.opt)
			tt.assert(g, pc)
		})
	}
}

func Test_newPreCondition_MultipleOptions(t *testing.T) {
	g := NewWithT(t)

	pc := newPreCondition(
		passingCheck,
		WithConditionType("Custom"),
		WithSeverity(common.ConditionSeverityInfo),
		WithStopReconciliation(),
		WithClusterTypes(cluster.ClusterTypeOpenShift),
		WithMessage("guidance"),
	)

	g.Expect(pc.conditionType).To(Equal("Custom"))
	g.Expect(pc.severity).To(Equal(common.ConditionSeverityInfo))
	g.Expect(pc.stopReconciliation).To(BeTrue())
	g.Expect(pc.clusterTypes).To(Equal([]string{cluster.ClusterTypeOpenShift}))
	g.Expect(pc.message).To(Equal("guidance"))
}

func TestRunAll(t *testing.T) {
	tests := []struct {
		name                   string
		preConditions          []PreCondition
		generation             int64
		expectedShouldStop     bool
		expectedStatus         metav1.ConditionStatus
		expectedReason         string
		expectedSeverity       common.ConditionSeverity
		expectedMsgContains    []string
		expectedMsgNotContains []string
	}{
		{
			name:               "all pass",
			preConditions:      []PreCondition{newPreCondition(passingCheck), newPreCondition(passingCheck)},
			expectedShouldStop: false,
			expectedStatus:     metav1.ConditionTrue,
		},
		{
			name:                "one fails without stop",
			preConditions:       []PreCondition{newPreCondition(passingCheck), newPreCondition(failingCheck("CRD missing"))},
			generation:          5,
			expectedShouldStop:  false,
			expectedStatus:      metav1.ConditionFalse,
			expectedReason:      PreConditionFailedReason,
			expectedMsgContains: []string{"CRD missing"},
		},
		{
			name:               "one fails with stop",
			preConditions:      []PreCondition{newPreCondition(passingCheck), newPreCondition(failingCheck("CRD missing"), WithStopReconciliation())},
			expectedShouldStop: true,
			expectedStatus:     metav1.ConditionFalse,
		},
		{
			name:                "check error yields Unknown",
			preConditions:       []PreCondition{newPreCondition(errorCheck)},
			expectedShouldStop:  false,
			expectedStatus:      metav1.ConditionUnknown,
			expectedReason:      PreConditionFailedReason,
			expectedSeverity:    common.ConditionSeverityError,
			expectedMsgContains: []string{"test error"},
		},
		{
			name:               "check error with stop",
			preConditions:      []PreCondition{newPreCondition(errorCheck, WithStopReconciliation())},
			expectedShouldStop: true,
			expectedStatus:     metav1.ConditionUnknown,
		},
		{
			name:               "mixed Unknown and Failed, False wins",
			preConditions:      []PreCondition{newPreCondition(failingCheck("CRD missing")), newPreCondition(errorCheck)},
			expectedShouldStop: false,
			expectedStatus:     metav1.ConditionFalse,
		},
		{
			name:                "aggregates messages from multiple failures",
			preConditions:       []PreCondition{newPreCondition(failingCheck("CRD A missing")), newPreCondition(failingCheck("CRD B missing"))},
			expectedShouldStop:  false,
			expectedStatus:      metav1.ConditionFalse,
			expectedMsgContains: []string{"CRD A missing", "CRD B missing"},
		},
		{
			name: "severity aggregation: Error if any Error",
			preConditions: []PreCondition{
				newPreCondition(failingCheck("info dep"), WithSeverity(common.ConditionSeverityInfo)),
				newPreCondition(failingCheck("error dep")),
			},
			expectedShouldStop: false,
			expectedStatus:     metav1.ConditionFalse,
			expectedSeverity:   common.ConditionSeverityError,
		},
		{
			name: "severity aggregation: Info if all Info",
			preConditions: []PreCondition{
				newPreCondition(failingCheck("info dep 1"), WithSeverity(common.ConditionSeverityInfo)),
				newPreCondition(failingCheck("info dep 2"), WithSeverity(common.ConditionSeverityInfo)),
			},
			expectedShouldStop: false,
			expectedStatus:     metav1.ConditionFalse,
			expectedSeverity:   common.ConditionSeverityInfo,
		},
		{
			name:                   "custom message overrides check result",
			preConditions:          []PreCondition{newPreCondition(failingCheck("original msg"), WithMessage("custom guidance"))},
			expectedShouldStop:     false,
			expectedStatus:         metav1.ConditionFalse,
			expectedMsgContains:    []string{"custom guidance"},
			expectedMsgNotContains: []string{"original msg"},
		},
		{
			name: "nil check honors severity and stop",
			preConditions: []PreCondition{
				newPreCondition(nil, WithSeverity(common.ConditionSeverityError), WithStopReconciliation()),
			},
			expectedShouldStop:  true,
			expectedStatus:      metav1.ConditionUnknown,
			expectedSeverity:    common.ConditionSeverityError,
			expectedMsgContains: []string{"precondition check function is nil"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			rr := newRR(status.ConditionDependenciesAvailable)
			if tt.generation > 0 {
				rr.Instance.SetGeneration(tt.generation)
			}

			shouldStop := RunAll(t.Context(), rr, tt.preConditions)
			g.Expect(shouldStop).To(Equal(tt.expectedShouldStop))

			got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
			g.Expect(got).NotTo(BeNil())
			g.Expect(got.Status).To(Equal(tt.expectedStatus))

			if tt.expectedStatus == metav1.ConditionTrue {
				g.Expect(got.Reason).To(BeEmpty())
				g.Expect(got.Message).To(BeEmpty())
			}

			if tt.expectedReason != "" {
				g.Expect(got.Reason).To(Equal(tt.expectedReason))
			}
			if tt.expectedSeverity != "" {
				g.Expect(got.Severity).To(Equal(tt.expectedSeverity))
			}
			for _, s := range tt.expectedMsgContains {
				g.Expect(got.Message).To(ContainSubstring(s))
			}
			for _, s := range tt.expectedMsgNotContains {
				g.Expect(got.Message).NotTo(ContainSubstring(s))
			}
			if tt.generation > 0 {
				g.Expect(got.ObservedGeneration).To(Equal(tt.generation))
			}
		})
	}
}

func TestRunAll_EmptyList(t *testing.T) {
	g := NewWithT(t)

	rr := newRR()
	shouldStop := RunAll(t.Context(), rr, nil)

	g.Expect(shouldStop).To(BeFalse())
}

func TestRunAll_ClusterTypeFiltering(t *testing.T) {
	g := NewWithT(t)

	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })

	rr := newRR(status.ConditionDependenciesAvailable)
	pcs := []PreCondition{
		newPreCondition(
			failingCheck("k8s only check"),
			WithClusterTypes(cluster.ClusterTypeKubernetes),
			WithStopReconciliation(),
		),
	}

	shouldStop := RunAll(t.Context(), rr, pcs)

	g.Expect(shouldStop).To(BeFalse())
	got := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).NotTo(Equal(metav1.ConditionFalse))
}

func TestRunAll_MultipleConditionTypes(t *testing.T) {
	g := NewWithT(t)

	customCondition := "CustomDeps"
	rr := newRR(status.ConditionDependenciesAvailable, customCondition)

	pcs := []PreCondition{
		newPreCondition(passingCheck),
		newPreCondition(failingCheck("custom failed"), WithConditionType(customCondition)),
	}

	RunAll(t.Context(), rr, pcs)

	defaultCond := rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(defaultCond).NotTo(BeNil())
	g.Expect(defaultCond.Status).To(Equal(metav1.ConditionTrue))

	customCond := rr.Conditions.GetCondition(customCondition)
	g.Expect(customCond).NotTo(BeNil())
	g.Expect(customCond.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(customCond.Message).To(ContainSubstring("custom failed"))
}

func TestRunAll_AllPreconditionsRunEvenWhenSomeFail(t *testing.T) {
	g := NewWithT(t)

	callCount := 0
	countingCheck := CheckFunc(func(_ context.Context, _ *types.ReconciliationRequest) (CheckResult, error) {
		callCount++
		return CheckResult{Pass: false, Message: "fail"}, nil
	})

	rr := newRR(status.ConditionDependenciesAvailable)
	pcs := []PreCondition{
		newPreCondition(countingCheck, WithStopReconciliation()),
		newPreCondition(countingCheck, WithStopReconciliation()),
		newPreCondition(countingCheck, WithStopReconciliation()),
	}

	RunAll(t.Context(), rr, pcs)

	g.Expect(callCount).To(Equal(3))
}

func TestRecord_StatusPriority(t *testing.T) {
	pc := &PreCondition{severity: common.ConditionSeverityError}

	tests := []struct {
		name           string
		sequence       []metav1.ConditionStatus
		expectedStatus metav1.ConditionStatus
	}{
		{
			name:           "False wins over Unknown",
			sequence:       []metav1.ConditionStatus{metav1.ConditionUnknown, metav1.ConditionFalse},
			expectedStatus: metav1.ConditionFalse,
		},
		{
			name:           "False wins over True",
			sequence:       []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse},
			expectedStatus: metav1.ConditionFalse,
		},
		{
			name:           "Unknown wins over True",
			sequence:       []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionUnknown},
			expectedStatus: metav1.ConditionUnknown,
		},
		{
			name:           "Unknown not downgraded by True",
			sequence:       []metav1.ConditionStatus{metav1.ConditionUnknown, metav1.ConditionTrue},
			expectedStatus: metav1.ConditionUnknown,
		},
		{
			name:           "False not downgraded by Unknown",
			sequence:       []metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionUnknown},
			expectedStatus: metav1.ConditionFalse,
		},
		{
			name:           "False not downgraded by True",
			sequence:       []metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionTrue},
			expectedStatus: metav1.ConditionFalse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			agg := &conditionAggregate{
				status:   metav1.ConditionTrue,
				severity: common.ConditionSeverityInfo,
			}

			for _, s := range tt.sequence {
				agg.record(s, "msg", pc)
			}

			g.Expect(agg.status).To(Equal(tt.expectedStatus))
		})
	}
}

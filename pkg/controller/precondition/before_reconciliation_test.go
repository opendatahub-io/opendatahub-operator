//nolint:testpackage // Exercises both execution phases and their shared options.
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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func TestRunBeforeReconciliation_Results(t *testing.T) {
	tests := []struct {
		name              string
		check             CheckFunc
		options           []Option
		wantResult        CheckResult
		wantErr           error
		wantNilCheckError bool
	}{
		{name: "pass", check: passingCheck, wantResult: CheckResult{Pass: true}},
		{name: "failure without stop option", check: failingCheck("not ready"), wantResult: CheckResult{Message: "not ready"}},
		{
			name: "informational failure still stops", check: failingCheck("not ready"),
			options: []Option{WithSeverity(common.ConditionSeverityInfo)}, wantResult: CheckResult{Message: "not ready"},
		},
		{
			name: "stop option still stops", check: failingCheck("not ready"),
			options: []Option{WithStopReconciliation()}, wantResult: CheckResult{Message: "not ready"},
		},
		{
			name: "message override", check: failingCheck("original"),
			options: []Option{WithMessage("overridden")}, wantResult: CheckResult{Message: "overridden"},
		},
		{name: "check error", check: errorCheck, wantErr: errTest},
		{
			name: "error takes precedence over pass", wantErr: errTest,
			check: func(context.Context, *types.ReconciliationRequest) (CheckResult, error) {
				return CheckResult{Pass: true}, errTest
			},
		},
		{name: "nil check", wantNilCheckError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			instance := &scheme.TestPlatformObject{}
			before := instance.DeepCopyObject()
			rr := &types.ReconciliationRequest{Instance: instance}
			options := append([]Option{BeforeReconciliation()}, tt.options...)
			result, err := RunBeforeReconciliation(t.Context(), rr, []PreCondition{Custom(tt.check, options...)})
			switch {
			case tt.wantNilCheckError:
				g.Expect(err).To(MatchError(ContainSubstring("check function is nil")))
			case tt.wantErr != nil:
				g.Expect(errors.Is(err, tt.wantErr)).To(BeTrue())
				g.Expect(err).To(MatchError(ContainSubstring("before reconciliation")))
			default:
				g.Expect(err).NotTo(HaveOccurred())
			}
			g.Expect(result).To(Equal(tt.wantResult))
			g.Expect(rr.Conditions).To(BeNil())
			g.Expect(instance).To(Equal(before), "early execution must not initialize or report conditions")
		})
	}
}

func TestRunBeforeReconciliation_SelectionAndOrder(t *testing.T) {
	g := NewWithT(t)
	var calls []string
	check := func(name string, pass bool) CheckFunc {
		return func(context.Context, *types.ReconciliationRequest) (CheckResult, error) {
			calls = append(calls, name)
			return CheckResult{Pass: pass, Message: name}, nil
		}
	}
	pcs := []PreCondition{
		Custom(check("ordinary", false)),
		Custom(check("first", true), BeforeReconciliation()),
		Custom(check("second", false), BeforeReconciliation()),
		Custom(check("third", true), BeforeReconciliation()),
	}
	rr := &types.ReconciliationRequest{}
	result, err := RunBeforeReconciliation(t.Context(), rr, pcs)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(CheckResult{Message: "second"}))
	g.Expect(calls).To(Equal([]string{"first", "second"}))

	calls = nil
	pcs[2] = Custom(errorCheck, BeforeReconciliation())
	_, err = RunBeforeReconciliation(t.Context(), rr, pcs)
	g.Expect(errors.Is(err, errTest)).To(BeTrue())
	g.Expect(calls).To(Equal([]string{"first"}), "check errors must also stop later checks")
}

func TestRunBeforeReconciliation_FilteringAndSkipping(t *testing.T) {
	g := NewWithT(t)
	originalCluster := cluster.GetClusterInfo()
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(originalCluster) })
	var calls []string
	skip := func(name string, skipped bool, err error) SkipFunc {
		return func(context.Context, *types.ReconciliationRequest) (bool, error) {
			calls = append(calls, name)
			return skipped, err
		}
	}
	rr := &types.ReconciliationRequest{}
	pcs := []PreCondition{
		Custom(nil, BeforeReconciliation(), WithClusterTypes(cluster.ClusterTypeKubernetes), WithSkipFunc(skip("excluded", false, errTest))),
		Custom(nil, BeforeReconciliation(), WithSkipFunc(skip("skipped", true, nil))),
		Custom(passingCheck, BeforeReconciliation(), WithClusterTypes(cluster.ClusterTypeOpenShift), WithSkipFunc(skip("checked", false, nil))),
	}
	result, err := RunBeforeReconciliation(t.Context(), rr, pcs)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Pass).To(BeTrue())
	g.Expect(calls).To(Equal([]string{"skipped", "checked"}))

	calls = nil
	pcs[1] = Custom(passingCheck, BeforeReconciliation(), WithSkipFunc(skip("skip error", true, errTest)))
	result, err = RunBeforeReconciliation(t.Context(), rr, pcs)
	g.Expect(errors.Is(err, errTest)).To(BeTrue())
	g.Expect(result.Pass).To(BeFalse())
	g.Expect(calls).To(Equal([]string{"skip error"}), "a skip error must not be treated as a successful skip")

	result, err = RunBeforeReconciliation(t.Context(), rr, pcs[:1])
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Pass).To(BeTrue(), "all filtered checks must allow reconciliation")
}

func TestRunBeforeReconciliation_EmptyAndOrdinaryLists(t *testing.T) {
	g := NewWithT(t)
	for _, pcs := range [][]PreCondition{
		nil,
		{},
		{Custom(nil)},
		{Custom(nil, WithSkipFunc(func(context.Context, *types.ReconciliationRequest) (bool, error) {
			t.Fatal("early execution invoked an ordinary skip callback")
			return false, nil
		}))},
		{Custom(nil, BeforeReconciliation(), WithSkipFunc(func(context.Context, *types.ReconciliationRequest) (bool, error) {
			return true, nil
		}))},
	} {
		result, err := RunBeforeReconciliation(t.Context(), &types.ReconciliationRequest{}, pcs)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(Equal(CheckResult{Pass: true}))
	}
}

func TestPreconditions_SeparateExecutionPhases(t *testing.T) {
	g := NewWithT(t)
	var calls []string
	pcs := []PreCondition{
		Custom(func(context.Context, *types.ReconciliationRequest) (CheckResult, error) {
			calls = append(calls, "ordinary error")
			return CheckResult{}, errTest
		}, WithConditionType("OrdinaryError"), WithStopReconciliation()),
		Custom(func(context.Context, *types.ReconciliationRequest) (CheckResult, error) {
			calls = append(calls, "before reconciliation")
			return CheckResult{Pass: true}, nil
		}, BeforeReconciliation(), WithConditionType("EarlyCondition")),
		Custom(func(context.Context, *types.ReconciliationRequest) (CheckResult, error) {
			calls = append(calls, "ordinary pass")
			return CheckResult{Pass: true}, nil
		}, WithConditionType("OrdinaryPass")),
	}
	result, err := RunBeforeReconciliation(t.Context(), &types.ReconciliationRequest{}, pcs)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Pass).To(BeTrue())
	rr := newRR()
	g.Expect(RunAll(t.Context(), rr, pcs)).To(BeTrue())
	g.Expect(calls).To(Equal([]string{"before reconciliation", "ordinary error", "ordinary pass"}))
	g.Expect(rr.Conditions.GetCondition("EarlyCondition")).To(BeNil())
	g.Expect(rr.Conditions.GetCondition("OrdinaryError").Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(rr.Conditions.GetCondition("OrdinaryPass").Status).To(Equal(metav1.ConditionTrue))

	// RunAll must skip early entries before filtering, skip callbacks, or reporting.
	pcs = []PreCondition{Custom(nil, BeforeReconciliation(), WithSkipFunc(func(context.Context, *types.ReconciliationRequest) (bool, error) {
		t.Fatal("ordinary execution invoked an early skip callback")
		return false, nil
	}))}
	g.Expect(RunAll(t.Context(), &types.ReconciliationRequest{}, pcs)).To(BeFalse())
	g.Expect(rr.Conditions.GetCondition(status.ConditionDependenciesAvailable)).To(BeNil())
}

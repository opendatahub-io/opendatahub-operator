package conditions_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"

	. "github.com/onsi/gomega"
)

func TestSetStatusCondition_LastTransitionTime(t *testing.T) {
	a := fakeAccessor{}
	a.conditions = make([]common.Condition, 0)

	ref := common.Condition{
		Type:   "foo",
		Status: metav1.ConditionFalse,
	}

	t.Run("LastTransitionTime should be set if not present", func(t *testing.T) {
		g := NewWithT(t)

		pre := conditions.FindStatusCondition(&a, "foo")
		g.Expect(pre).Should(BeNil())

		g.Expect(conditions.SetStatusCondition(&a, ref)).Should(BeTrue())
		g.Expect(a.conditions).Should(HaveLen(1))

		post := conditions.FindStatusCondition(&a, "foo")
		g.Expect(post).Should(And(
			Not(BeNil()),
			HaveField("LastTransitionTime", Not(BeZero())),
		))
	})

	t.Run("LastTransitionTime should not change when status is not changing", func(t *testing.T) {
		g := NewWithT(t)

		pre := conditions.FindStatusCondition(&a, "foo")
		g.Expect(pre).Should(And(
			Not(BeNil()),
			HaveField("LastTransitionTime", Not(BeZero())),
		))

		g.Expect(conditions.SetStatusCondition(&a, ref)).ShouldNot(BeTrue())
		g.Expect(a.conditions).Should(HaveLen(1))

		post := conditions.FindStatusCondition(&a, "foo")
		g.Expect(post).Should(And(
			Not(BeNil()),
			HaveField("LastTransitionTime", Equal(pre.LastTransitionTime)),
		))
	})

	t.Run("LastTransitionTime should change when status is changing", func(t *testing.T) {
		g := NewWithT(t)

		pre := conditions.FindStatusCondition(&a, "foo")
		g.Expect(pre).Should(And(
			Not(BeNil()),
			HaveField("LastTransitionTime", Not(BeZero())),
		))

		nc := common.Condition{
			Type:   "foo",
			Status: metav1.ConditionTrue,
		}

		g.Expect(conditions.SetStatusCondition(&a, nc)).Should(BeTrue())
		g.Expect(a.conditions).Should(HaveLen(1))

		post := conditions.FindStatusCondition(&a, "foo")
		g.Expect(post).Should(And(
			Not(BeNil()),
			HaveField("LastTransitionTime", Not(BeZero())),
			HaveField("LastTransitionTime", Not(Equal(pre.LastTransitionTime))),
		))
	})
}

func TestSetStatusCondition_Update(t *testing.T) {
	g := NewWithT(t)

	a := fakeAccessor{}
	a.conditions = make([]common.Condition, 0)

	ref := common.Condition{
		Type:               "foo",
		Status:             metav1.ConditionFalse,
		Reason:             "bar reason",
		Message:            "bar msg",
		Severity:           "bar severity",
		ObservedGeneration: 1,
	}

	g.Expect(conditions.SetStatusCondition(&a, ref)).Should(BeTrue())
	g.Expect(a.conditions).Should(HaveLen(1))

	pre := conditions.FindStatusCondition(&a, "foo")
	g.Expect(pre).Should(And(
		Not(BeNil()),
		HaveField("LastTransitionTime", Not(BeZero())),
	))

	nc := common.Condition{
		Type:    "foo",
		Status:  metav1.ConditionTrue,
		Reason:  "baz reason",
		Message: "baz msg",
	}

	g.Expect(conditions.SetStatusCondition(&a, nc)).Should(BeTrue())
	g.Expect(a.conditions).Should(HaveLen(1))

	post := conditions.FindStatusCondition(&a, "foo")
	g.Expect(post).Should(And(
		Not(BeNil()),
		HaveField("LastTransitionTime", Not(BeZero())),
		HaveField("LastTransitionTime", Not(Equal(pre.LastTransitionTime))),
		HaveField("Reason", Equal(nc.Reason)),
		HaveField("Message", Equal(nc.Message)),
		HaveField("Severity", BeZero()),
		HaveField("ObservedGeneration", BeZero()),
	))
}

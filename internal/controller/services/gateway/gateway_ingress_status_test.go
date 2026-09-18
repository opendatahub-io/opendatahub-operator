//nolint:testpackage
package gateway

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"

	. "github.com/onsi/gomega"
)

func TestBuildAdditionalIngressStatuses(t *testing.T) {
	g := NewWithT(t)

	statuses := buildAdditionalIngressStatuses(
		[]serviceApi.AdditionalIngress{
			{Name: "zeta", Hostname: "zeta.example.com"},
			{Name: "alpha", Hostname: "alpha.example.com"},
		},
		nil,
		4,
	)

	g.Expect(statuses).To(HaveLen(2))
	g.Expect(statuses[0].Name).To(Equal("alpha"))
	g.Expect(statuses[1].Name).To(Equal("zeta"))
	for _, status := range statuses {
		g.Expect(status.Conditions).To(HaveLen(4))
		for _, condition := range status.Conditions {
			g.Expect(condition.Status).To(Equal(metav1.ConditionUnknown))
			g.Expect(condition.ObservedGeneration).To(Equal(int64(4)))
			g.Expect(condition.Reason).To(Equal(serviceApi.AdditionalIngressReconciliationPendingReason))
		}
	}
}

func TestBuildAdditionalIngressStatusesPreservesCurrentConditions(t *testing.T) {
	g := NewWithT(t)
	ready := common.Condition{
		Type:               serviceApi.AdditionalIngressListenerReadyConditionType,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: 7,
		Reason:             "Programmed",
	}

	statuses := buildAdditionalIngressStatuses(
		[]serviceApi.AdditionalIngress{{Name: "alpha", Hostname: "new.example.com"}},
		[]serviceApi.AdditionalIngressStatus{{
			Name:     "alpha",
			Hostname: "old.example.com",
			Conditions: []common.Condition{
				ready,
				{Type: serviceApi.AdditionalIngressRouteAdmittedConditionType, Status: metav1.ConditionFalse, ObservedGeneration: 6},
			},
		}},
		7,
	)

	g.Expect(statuses).To(HaveLen(1))
	g.Expect(statuses[0].Hostname).To(Equal("new.example.com"))
	g.Expect(statuses[0].Conditions[0]).To(Equal(ready))
	g.Expect(statuses[0].Conditions[1].Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(statuses[0].Conditions[1].ObservedGeneration).To(Equal(int64(7)))
}

func TestBuildAdditionalIngressStatusesPrunesRemovedEntries(t *testing.T) {
	g := NewWithT(t)

	statuses := buildAdditionalIngressStatuses(
		[]serviceApi.AdditionalIngress{{Name: "current", Hostname: "current.example.com"}},
		[]serviceApi.AdditionalIngressStatus{
			{Name: "current"},
			{Name: "removed"},
		},
		2,
	)

	g.Expect(statuses).To(HaveLen(1))
	g.Expect(statuses[0].Name).To(Equal("current"))
}

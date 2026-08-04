package registry_test

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"

	. "github.com/onsi/gomega"
)

type fakeComponentHandler struct {
	name    string
	enabled bool
}

func (f *fakeComponentHandler) Init(_ common.Platform, _ operatorconfig.OperatorSettings) error {
	return nil
}
func (f *fakeComponentHandler) GetName() string { return f.name }
func (f *fakeComponentHandler) NewCRObject(_ context.Context, _ client.Client, _ *dscv2.DataScienceCluster) (common.PlatformObject, error) {
	return nil, nil
}
func (f *fakeComponentHandler) NewComponentReconciler(_ context.Context, _ ctrl.Manager) error {
	return nil
}
func (f *fakeComponentHandler) UpdateDSCStatus(_ context.Context, _ *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	return metav1.ConditionTrue, nil
}
func (f *fakeComponentHandler) IsEnabled(_ *dscv2.DataScienceCluster) bool {
	return f.enabled
}

func TestForEachSkipsSuppressedComponents(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeComponentHandler{name: "comp-a", enabled: true})
	reg.Add(&fakeComponentHandler{name: "comp-b", enabled: true})
	reg.Add(&fakeComponentHandler{name: "comp-c", enabled: true})

	reg.Disable("comp-b")

	var visited []string
	err := reg.ForEach(func(ch registry.ComponentHandler) error {
		visited = append(visited, ch.GetName())
		return nil
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(visited).Should(HaveLen(2))
	g.Expect(visited).Should(ContainElement("comp-a"))
	g.Expect(visited).Should(ContainElement("comp-c"))
	g.Expect(visited).ShouldNot(ContainElement("comp-b"))
}

func TestForEachVisitsAllWhenNoneSuppressed(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeComponentHandler{name: "comp-x", enabled: true})
	reg.Add(&fakeComponentHandler{name: "comp-y", enabled: true})

	var visited []string
	err := reg.ForEach(func(ch registry.ComponentHandler) error {
		visited = append(visited, ch.GetName())
		return nil
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(visited).Should(HaveLen(2))
}

func TestForEachCollectsErrors(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeComponentHandler{name: "comp-err", enabled: true})

	err := reg.ForEach(func(ch registry.ComponentHandler) error {
		return errors.New("test error")
	})

	g.Expect(err).Should(HaveOccurred())
}

func TestIsComponentEnabledReturnsFalseWhenSuppressed(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeComponentHandler{name: "comp-sup", enabled: true})

	g.Expect(reg.IsComponentEnabled("comp-sup", nil)).Should(BeTrue(), "should be enabled when not suppressed")

	reg.Disable("comp-sup")
	g.Expect(reg.IsComponentEnabled("comp-sup", nil)).Should(BeFalse(), "should be disabled when suppressed")
}

func TestSetEnabled(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeComponentHandler{name: "flagtest", enabled: true})

	g.Expect(reg.IsEnabled("flagtest")).Should(BeTrue(), "should be enabled by default")

	reg.Disable("flagtest")
	g.Expect(reg.IsEnabled("flagtest")).Should(BeFalse(), "should be disabled after Disable()")

	reg.Enable("flagtest")
	g.Expect(reg.IsEnabled("flagtest")).Should(BeTrue(), "should be enabled after Enable()")
}

func TestForEachIsDeterministic(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeComponentHandler{name: "zebra", enabled: true}, registry.WithRunlevel(dag.RL(20)))
	reg.Add(&fakeComponentHandler{name: "alpha", enabled: true}, registry.WithRunlevel(dag.RL(20)))
	reg.Add(&fakeComponentHandler{name: "middle", enabled: true}, registry.WithRunlevel(dag.RL(20)))

	var visited []string
	err := reg.ForEach(func(ch registry.ComponentHandler) error {
		visited = append(visited, ch.GetName())
		return nil
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(visited).Should(Equal([]string{"alpha", "middle", "zebra"}))
}

func TestResolvedBatchesRunlevelOrdering(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeComponentHandler{name: "ext-comp", enabled: true}, registry.WithRunlevel(dag.RL(30)))
	reg.Add(&fakeComponentHandler{name: "infra-comp", enabled: true}, registry.WithRunlevel(dag.RL(0)))
	reg.Add(&fakeComponentHandler{name: "ai-comp", enabled: true}, registry.WithRunlevel(dag.RL(20)))

	batches, err := reg.ResolvedBatches()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(batches).Should(HaveLen(3))

	g.Expect(batches[0]).Should(HaveLen(1))
	g.Expect(batches[0][0].GetName()).Should(Equal("infra-comp"))
	g.Expect(batches[1]).Should(HaveLen(1))
	g.Expect(batches[1][0].GetName()).Should(Equal("ai-comp"))
	g.Expect(batches[2]).Should(HaveLen(1))
	g.Expect(batches[2][0].GetName()).Should(Equal("ext-comp"))
}

func TestResolvedBatchesSkipsDisabled(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeComponentHandler{name: "a", enabled: true}, registry.WithRunlevel(dag.RL(20)))
	reg.Add(&fakeComponentHandler{name: "b", enabled: true}, registry.WithRunlevel(dag.RL(20)))
	reg.Disable("b")

	batches, err := reg.ResolvedBatches()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(batches).Should(HaveLen(1))
	g.Expect(batches[0]).Should(HaveLen(1))
	g.Expect(batches[0][0].GetName()).Should(Equal("a"))
}

func TestResolvedBatchesCacheInvalidation(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeComponentHandler{name: "comp-a", enabled: true}, registry.WithRunlevel(dag.RL(20)))

	batches1, err := reg.ResolvedBatches()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(batches1).Should(HaveLen(1))

	reg.Add(&fakeComponentHandler{name: "comp-b", enabled: true}, registry.WithRunlevel(dag.RL(30)))

	batches2, err := reg.ResolvedBatches()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(batches2).Should(HaveLen(2))
}

func TestReverseBatchesOrdering(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeComponentHandler{name: "infra", enabled: true}, registry.WithRunlevel(dag.RL(0)))
	reg.Add(&fakeComponentHandler{name: "ext-a", enabled: true}, registry.WithRunlevel(dag.RL(30)))
	reg.Add(&fakeComponentHandler{name: "ext-b", enabled: true}, registry.WithRunlevel(dag.RL(30)))

	batches, err := reg.ReverseBatches()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(batches).Should(HaveLen(2))

	g.Expect(batches[0][0].GetName()).Should(Equal("ext-b"))
	g.Expect(batches[0][1].GetName()).Should(Equal("ext-a"))
	g.Expect(batches[1][0].GetName()).Should(Equal("infra"))
}

func TestLookup(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	h := &fakeComponentHandler{name: "findme", enabled: true}
	reg.Add(h)

	g.Expect(reg.Lookup("findme")).Should(Equal(h))
	g.Expect(reg.Lookup("nonexistent")).Should(BeNil())
}

func TestForAllIncludesDisabled(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeComponentHandler{name: "enabled-comp", enabled: true})
	reg.Add(&fakeComponentHandler{name: "disabled-comp", enabled: true})
	reg.Disable("disabled-comp")

	type visit struct {
		name    string
		enabled bool
	}
	var visits []visit

	err := reg.ForAll(func(handler registry.ComponentHandler, enabled bool) error {
		visits = append(visits, visit{name: handler.GetName(), enabled: enabled})
		return nil
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(visits).Should(HaveLen(2))
	g.Expect(visits).Should(ContainElement(visit{name: "disabled-comp", enabled: false}))
	g.Expect(visits).Should(ContainElement(visit{name: "enabled-comp", enabled: true}))
}

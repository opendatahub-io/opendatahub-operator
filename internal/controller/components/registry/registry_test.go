package registry_test

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"

	. "github.com/onsi/gomega"
)

type fakeComponentHandler struct {
	name string
}

func (f *fakeComponentHandler) Init(_ common.Platform, _ operatorconfig.OperatorSettings) error {
	return nil
}
func (f *fakeComponentHandler) GetName() string { return f.name }
func (f *fakeComponentHandler) NewCRObject(_ context.Context, _ client.Client, _ *dscApi.DataScienceCluster) (common.PlatformObject, error) {
	return nil, nil
}
func (f *fakeComponentHandler) NewComponentReconciler(_ context.Context, _ ctrl.Manager) error {
	return nil
}
func (f *fakeComponentHandler) UpdateDSCStatus(_ context.Context, _ *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	return metav1.ConditionTrue, nil
}
func (f *fakeComponentHandler) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{}
}
func (f *fakeComponentHandler) IsEnabled(_ *dscApi.DataScienceCluster) bool {
	return true
}

func TestForEachSkipsSuppressedComponents(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeComponentHandler{name: "comp-a"})
	reg.Add(&fakeComponentHandler{name: "comp-b"})
	reg.Add(&fakeComponentHandler{name: "comp-c"})

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
	reg.Add(&fakeComponentHandler{name: "comp-x"})
	reg.Add(&fakeComponentHandler{name: "comp-y"})

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
	reg.Add(&fakeComponentHandler{name: "alpha"})
	reg.Add(&fakeComponentHandler{name: "middle"})
	reg.Add(&fakeComponentHandler{name: "zebra"})
	failures := map[string]error{"alpha": errors.New("first failure"), "zebra": errors.New("last failure")}
	var visited []string

	err := reg.ForEach(func(ch registry.ComponentHandler) error {
		visited = append(visited, ch.GetName())
		return failures[ch.GetName()]
	})

	g.Expect(visited).To(Equal([]string{"alpha", "middle", "zebra"}))
	g.Expect(errors.Is(err, failures["alpha"])).To(BeTrue())
	g.Expect(errors.Is(err, failures["zebra"])).To(BeTrue())
}

func TestForEachIsDeterministic(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeComponentHandler{name: "zebra"})
	reg.Add(&fakeComponentHandler{name: "alpha"})
	reg.Add(&fakeComponentHandler{name: "middle"})

	var visited []string
	err := reg.ForEach(func(ch registry.ComponentHandler) error {
		visited = append(visited, ch.GetName())
		return nil
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(visited).Should(Equal([]string{"alpha", "middle", "zebra"}))
}

func TestLookup(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	h := &fakeComponentHandler{name: "findme"}
	reg.Add(h)

	g.Expect(reg.Lookup("findme")).Should(Equal(h))
	g.Expect(reg.Lookup("nonexistent")).Should(BeNil())
}

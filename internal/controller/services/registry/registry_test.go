package registry_test

import (
	"context"
	"errors"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/registry"

	. "github.com/onsi/gomega"
)

type fakeServiceHandler struct {
	name string
}

func (f *fakeServiceHandler) Init(_ common.Platform) error { return nil }
func (f *fakeServiceHandler) GetName() string              { return f.name }
func (f *fakeServiceHandler) GetManagementState(_ common.Platform, _ *dsciv2.DSCInitialization) operatorv1.ManagementState {
	return operatorv1.Managed
}
func (f *fakeServiceHandler) NewReconciler(_ context.Context, _ ctrl.Manager) error {
	return nil
}

func TestForEachSkipsSuppressedServices(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeServiceHandler{name: "svc-a"})
	reg.Add(&fakeServiceHandler{name: "svc-b"})
	reg.Add(&fakeServiceHandler{name: "svc-c"})

	reg.Disable("svc-b")

	var visited []string
	err := reg.ForEach(func(ch registry.ServiceHandler) error {
		visited = append(visited, ch.GetName())
		return nil
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(visited).Should(HaveLen(2))
	g.Expect(visited).Should(ContainElement("svc-a"))
	g.Expect(visited).Should(ContainElement("svc-c"))
	g.Expect(visited).ShouldNot(ContainElement("svc-b"))
}

func TestForEachVisitsAllWhenNoneSuppressed(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeServiceHandler{name: "svc-x"})
	reg.Add(&fakeServiceHandler{name: "svc-y"})

	var visited []string
	err := reg.ForEach(func(ch registry.ServiceHandler) error {
		visited = append(visited, ch.GetName())
		return nil
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(visited).Should(HaveLen(2))
}

func TestForEachCollectsErrors(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeServiceHandler{name: "svc-err"})

	err := reg.ForEach(func(ch registry.ServiceHandler) error {
		return errors.New("test error")
	})

	g.Expect(err).Should(HaveOccurred())
}

func TestSetEnabled(t *testing.T) {
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&fakeServiceHandler{name: "svcflagtest"})

	g.Expect(reg.IsEnabled("svcflagtest")).Should(BeTrue(), "should be enabled by default")

	reg.Disable("svcflagtest")
	g.Expect(reg.IsEnabled("svcflagtest")).Should(BeFalse(), "should be disabled after Disable()")

	reg.Enable("svcflagtest")
	g.Expect(reg.IsEnabled("svcflagtest")).Should(BeTrue(), "should be enabled after Enable()")
}

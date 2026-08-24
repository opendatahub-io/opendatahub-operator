//nolint:testpackage
package modules

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

type cleanupMockHandler struct {
	BaseHandler

	crState            CRState
	crStateErr         error
	deletedCR          bool
	crDeleteErr        error
	deletedOperatorRes bool
	operatorDeleteErr  error
	operatorManifests  OperatorManifests
}

func (m *cleanupMockHandler) IsEnabled(_ *configv1alpha1.PlatformModules) bool {
	return false
}

func (m *cleanupMockHandler) BuildModuleCR(_ context.Context, _ client.Client, _ *DSCContext, _ *ModuleCRConfig) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (m *cleanupMockHandler) GetModuleCRState(_ context.Context, _ client.Client) (CRState, error) {
	return m.crState, m.crStateErr
}

func (m *cleanupMockHandler) GetOperatorManifests(_ *PlatformContext) OperatorManifests {
	return m.operatorManifests
}

func (m *cleanupMockHandler) DeleteModuleCR(_ context.Context, _ client.Client) error {
	m.deletedCR = true
	return m.crDeleteErr
}

func (m *cleanupMockHandler) DeleteOperatorResources(_ context.Context, _ client.Client, _ *PlatformContext) error {
	m.deletedOperatorRes = true
	return m.operatorDeleteErr
}

var _ ModuleHandler = (*cleanupMockHandler)(nil)

func newCleanupMock(name string, crState CRState) *cleanupMockHandler {
	return &cleanupMockHandler{
		BaseHandler: BaseHandler{
			Config: ModuleConfig{
				Name:   name,
				CRName: "default",
				GVK: schema.GroupVersionKind{
					Group:   "components.platform.opendatahub.io",
					Version: "v1alpha1",
					Kind:    "TestModule",
				},
			},
		},
		crState: crState,
	}
}

func setupCleanupTest(t *testing.T, handler *cleanupMockHandler) (*odhtype.ReconciliationRequest, func()) {
	t.Helper()
	g := NewWithT(t)

	oldR := r
	r = &Registry{}
	r.Add(handler)
	r.Disable(handler.GetName())

	provision.Add(handler.GetName(), provision.KindModule, dag.RL(99))

	platform := &configv1alpha1.Platform{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
	}

	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: "test-ns",
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
	g.Expect(err).ShouldNot(HaveOccurred())

	cm := conditions.NewManager(platform, status.ConditionTypeReady, status.ConditionTypeModulesReady)

	rr := &odhtype.ReconciliationRequest{
		Client:     cli,
		Instance:   platform,
		Conditions: cm,
		Release:    common.Release{Name: cluster.OpenDataHub},
	}

	cleanup := func() {
		r = oldR
		provision.Disable(handler.GetName())
		provision.InvalidateCache()
	}

	return rr, cleanup
}

func TestCleanupDisabledModules_CRAbsent_DeletesOperatorResources(t *testing.T) {
	g := NewWithT(t)

	handler := newCleanupMock("test-mod", CRStateAbsent)
	rr, cleanup := setupCleanupTest(t, handler)
	defer cleanup()

	err := cleanupDisabledModules(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(handler.deletedOperatorRes).Should(BeTrue())
}

func TestCleanupDisabledModules_CRAlive_NoPerModuleCondition(t *testing.T) {
	g := NewWithT(t)

	handler := newCleanupMock("test-mod", CRStateAlive)
	rr, cleanup := setupCleanupTest(t, handler)
	defer cleanup()

	err := cleanupDisabledModules(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(handler.deletedOperatorRes).Should(BeFalse())

	cond := rr.Conditions.GetCondition("TestModuleReady")
	g.Expect(cond).Should(BeNil(), "cleanup should not set per-module conditions on Platform")
}

func TestCleanupDisabledModules_CRDeleting_NoPerModuleCondition(t *testing.T) {
	g := NewWithT(t)

	handler := newCleanupMock("test-mod", CRStateDeleting)
	rr, cleanup := setupCleanupTest(t, handler)
	defer cleanup()

	err := cleanupDisabledModules(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(handler.deletedOperatorRes).Should(BeFalse())

	cond := rr.Conditions.GetCondition("TestModuleReady")
	g.Expect(cond).Should(BeNil(), "cleanup should not set per-module conditions on Platform")
}

func TestWaitForModuleCRDeletion_NoModules(t *testing.T) {
	g := NewWithT(t)

	oldR := r
	r = &Registry{}
	defer func() { r = oldR }()

	err := waitForModuleCRDeletion(t.Context(), &odhtype.ReconciliationRequest{})
	g.Expect(err).ShouldNot(HaveOccurred())
}

func TestWaitForModuleCRDeletion_CRAbsent_Succeeds(t *testing.T) {
	g := NewWithT(t)

	handler := newCleanupMock("test-mod", CRStateAbsent)
	rr, cleanup := setupCleanupTest(t, handler)
	defer cleanup()

	err := waitForModuleCRDeletion(t.Context(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(handler.deletedCR).Should(BeFalse())
}

func TestWaitForModuleCRDeletion_CRAlive_DeletesAndWaits(t *testing.T) {
	g := NewWithT(t)

	handler := newCleanupMock("test-mod", CRStateAlive)
	rr, cleanup := setupCleanupTest(t, handler)
	defer cleanup()

	err := waitForModuleCRDeletion(t.Context(), rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("waiting for module CRs to be deleted"))
	g.Expect(err.Error()).Should(ContainSubstring("test-mod"))
	g.Expect(handler.deletedCR).Should(BeTrue())
}

func TestWaitForModuleCRDeletion_CRDeleting_WaitsWithoutDelete(t *testing.T) {
	g := NewWithT(t)

	handler := newCleanupMock("test-mod", CRStateDeleting)
	rr, cleanup := setupCleanupTest(t, handler)
	defer cleanup()

	err := waitForModuleCRDeletion(t.Context(), rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("waiting for module CRs to be deleted"))
	g.Expect(err.Error()).Should(ContainSubstring("test-mod"))
	g.Expect(handler.deletedCR).Should(BeFalse())
}

func TestWaitForModuleCRDeletion_StateError_IsReturned(t *testing.T) {
	g := NewWithT(t)

	handler := newCleanupMock("test-mod", CRStateAlive)
	handler.crStateErr = context.DeadlineExceeded
	rr, cleanup := setupCleanupTest(t, handler)
	defer cleanup()

	err := waitForModuleCRDeletion(t.Context(), rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("getting module CR state for test-mod"))
	g.Expect(handler.deletedCR).Should(BeFalse())
}

func TestWaitForModuleCRDeletion_MultipleModules_ListsPending(t *testing.T) {
	g := NewWithT(t)

	absent := newCleanupMock("absent-mod", CRStateAbsent)
	deleting := newCleanupMock("deleting-mod", CRStateDeleting)

	oldR := r
	r = &Registry{}
	r.Add(absent)
	r.Add(deleting)
	defer func() { r = oldR }()

	err := waitForModuleCRDeletion(t.Context(), &odhtype.ReconciliationRequest{})
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("deleting-mod"))
	g.Expect(err.Error()).ShouldNot(ContainSubstring("absent-mod"))
	g.Expect(absent.deletedCR).Should(BeFalse())
	g.Expect(deleting.deletedCR).Should(BeFalse())
}

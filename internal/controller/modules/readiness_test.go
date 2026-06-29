package modules_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"

	. "github.com/onsi/gomega"
)

type statusMockHandler struct {
	modules.BaseHandler

	enabled bool
	status  *modules.ModuleStatus
	err     error
}

func (m *statusMockHandler) IsEnabled(_ *modules.PlatformContext) bool {
	return m.enabled
}

func (m *statusMockHandler) BuildModuleCR(_ context.Context, _ client.Client, _ *modules.PlatformContext) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (m *statusMockHandler) GetModuleStatus(_ context.Context, _ client.Client) (*modules.ModuleStatus, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.status, nil
}

var _ modules.ModuleHandler = (*statusMockHandler)(nil)

func newStatusMock(name string, status *modules.ModuleStatus) *statusMockHandler {
	return &statusMockHandler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{Name: name, CRName: "default"},
		},
		enabled: true,
		status:  status,
	}
}

func TestReadinessChecker_ReadyWithMatchingVersion(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	reg := &modules.Registry{}
	reg.Add(newStatusMock("mod-a", &modules.ModuleStatus{
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue},
		},
		ObservedGeneration: 1,
		Generation:         1,
		ReleaseVersion:     "2.20.0",
	}))

	checker := modules.NewReadinessChecker(reg, nil, "2.20.0")
	ready, err := checker.IsReady(context.Background(), "mod-a")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ready).Should(BeTrue())
}

func TestReadinessChecker_NotReadyVersionMismatch(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	reg := &modules.Registry{}
	reg.Add(newStatusMock("mod-b", &modules.ModuleStatus{
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue},
		},
		ObservedGeneration: 1,
		Generation:         1,
		ReleaseVersion:     "2.19.0",
	}))

	checker := modules.NewReadinessChecker(reg, nil, "2.20.0")
	ready, err := checker.IsReady(context.Background(), "mod-b")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ready).Should(BeFalse(), "module reporting old version should not be ready")
}

func TestReadinessChecker_EmptyVersionSkipsCheck(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	reg := &modules.Registry{}
	reg.Add(newStatusMock("mod-c", &modules.ModuleStatus{
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue},
		},
		ObservedGeneration: 1,
		Generation:         1,
		ReleaseVersion:     "",
	}))

	checker := modules.NewReadinessChecker(reg, nil, "2.20.0")
	ready, err := checker.IsReady(context.Background(), "mod-c")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ready).Should(BeTrue(), "module without version field should fall through to Ready check")
}

func TestReadinessChecker_StaleGenerationNotReady(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	reg := &modules.Registry{}
	reg.Add(newStatusMock("mod-d", &modules.ModuleStatus{
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue},
		},
		ObservedGeneration: 1,
		Generation:         2,
		ReleaseVersion:     "2.20.0",
	}))

	checker := modules.NewReadinessChecker(reg, nil, "2.20.0")
	ready, err := checker.IsReady(context.Background(), "mod-d")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ready).Should(BeFalse(), "stale observedGeneration should mean not ready")
}

func TestReadinessChecker_ReadyFalseNotReady(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	reg := &modules.Registry{}
	reg.Add(newStatusMock("mod-e", &modules.ModuleStatus{
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionFalse},
		},
		ObservedGeneration: 1,
		Generation:         1,
		ReleaseVersion:     "2.20.0",
	}))

	checker := modules.NewReadinessChecker(reg, nil, "2.20.0")
	ready, err := checker.IsReady(context.Background(), "mod-e")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ready).Should(BeFalse())
}

func TestReadinessChecker_NoPlatformVersionSkipsCheck(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	reg := &modules.Registry{}
	reg.Add(newStatusMock("mod-f", &modules.ModuleStatus{
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue},
		},
		ObservedGeneration: 1,
		Generation:         1,
		ReleaseVersion:     "2.19.0",
	}))

	checker := modules.NewReadinessChecker(reg, nil, "")
	ready, err := checker.IsReady(context.Background(), "mod-f")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ready).Should(BeTrue(), "empty platform version should skip version check")
}

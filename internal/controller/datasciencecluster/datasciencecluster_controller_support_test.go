//nolint:testpackage
package datasciencecluster

import (
	"context"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

// mockHandler is a minimal ComponentHandler for testing computeComponentsStatus.
type mockHandler struct {
	name    string
	enabled bool
	status  metav1.ConditionStatus
	err     error
}

func (m *mockHandler) Init(_ common.Platform, _ operatorconfig.OperatorSettings) error { return nil }
func (m *mockHandler) GetName() string                                                 { return m.name }
func (m *mockHandler) NewCRObject(_ context.Context, _ client.Client, _ *dscv2.DataScienceCluster) (common.PlatformObject, error) {
	return nil, nil
}
func (m *mockHandler) NewComponentReconciler(_ context.Context, _ ctrl.Manager) error {
	return nil
}
func (m *mockHandler) IsEnabled(_ *dscv2.DataScienceCluster) bool { return m.enabled }
func (m *mockHandler) UpdateDSCStatus(_ context.Context, _ *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	return m.status, m.err
}

func newRegistry(handlers ...cr.ComponentHandler) *cr.Registry {
	reg := &cr.Registry{}
	for _, h := range handlers {
		reg.Add(h)
	}
	return reg
}

func newDSC() *dscv2.DataScienceCluster {
	dsc := &dscv2.DataScienceCluster{}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)
	dsc.SetName("test-dsc")
	return dsc
}

func TestComputeComponentsStatus(t *testing.T) {
	t.Run("all managed components ready should set ComponentsReady=True", func(t *testing.T) {
		g := NewWithT(t)
		dsc := newDSC()
		reg := newRegistry(
			&mockHandler{name: "comp-a", enabled: true, status: metav1.ConditionTrue},
			&mockHandler{name: "comp-b", enabled: true, status: metav1.ConditionTrue},
		)

		rr := &types.ReconciliationRequest{
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, status.ConditionTypeComponentsReady),
		}

		err := computeComponentsStatus(t.Context(), rr, reg)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(dsc).Should(WithTransform(json.Marshal,
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeComponentsReady, metav1.ConditionTrue),
		))
	})

	t.Run("ConditionUnknown from enabled component should set ComponentsReady=False", func(t *testing.T) {
		g := NewWithT(t)
		dsc := newDSC()
		reg := newRegistry(
			&mockHandler{name: "comp-a", enabled: true, status: metav1.ConditionTrue},
			&mockHandler{name: "comp-b", enabled: true, status: metav1.ConditionUnknown},
		)

		rr := &types.ReconciliationRequest{
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, status.ConditionTypeComponentsReady),
		}

		err := computeComponentsStatus(t.Context(), rr, reg)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeComponentsReady, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("comp-b")`,
				status.ConditionTypeComponentsReady),
		)))
	})

	t.Run("ConditionFalse from enabled component should set ComponentsReady=False", func(t *testing.T) {
		g := NewWithT(t)
		dsc := newDSC()
		reg := newRegistry(
			&mockHandler{name: "comp-a", enabled: true, status: metav1.ConditionTrue},
			&mockHandler{name: "comp-b", enabled: true, status: metav1.ConditionFalse},
		)

		rr := &types.ReconciliationRequest{
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, status.ConditionTypeComponentsReady),
		}

		err := computeComponentsStatus(t.Context(), rr, reg)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeComponentsReady, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("comp-b")`,
				status.ConditionTypeComponentsReady),
		)))
	})

	t.Run("disabled component returning ConditionFalse should count as not ready", func(t *testing.T) {
		g := NewWithT(t)
		dsc := newDSC()
		reg := newRegistry(
			&mockHandler{name: "comp-a", enabled: true, status: metav1.ConditionTrue},
			&mockHandler{name: "comp-stuck", enabled: false, status: metav1.ConditionFalse},
		)

		rr := &types.ReconciliationRequest{
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, status.ConditionTypeComponentsReady),
		}

		err := computeComponentsStatus(t.Context(), rr, reg)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeComponentsReady, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("comp-stuck")`,
				status.ConditionTypeComponentsReady),
		)))
	})

	t.Run("disabled component returning ConditionUnknown should be skipped", func(t *testing.T) {
		g := NewWithT(t)
		dsc := newDSC()
		reg := newRegistry(
			&mockHandler{name: "comp-a", enabled: true, status: metav1.ConditionTrue},
			&mockHandler{name: "comp-disabled", enabled: false, status: metav1.ConditionUnknown},
		)

		rr := &types.ReconciliationRequest{
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, status.ConditionTypeComponentsReady),
		}

		err := computeComponentsStatus(t.Context(), rr, reg)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(dsc).Should(WithTransform(json.Marshal,
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeComponentsReady, metav1.ConditionTrue),
		))
	})

	t.Run("no managed components should set ComponentsReady=True with info severity", func(t *testing.T) {
		g := NewWithT(t)
		dsc := newDSC()
		reg := newRegistry(
			&mockHandler{name: "comp-a", enabled: false, status: metav1.ConditionUnknown},
		)

		rr := &types.ReconciliationRequest{
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, status.ConditionTypeComponentsReady),
		}

		err := computeComponentsStatus(t.Context(), rr, reg)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
				status.ConditionTypeComponentsReady, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`,
				status.ConditionTypeComponentsReady, status.NoManagedComponentsReason),
		)))
	})
}

// mockModuleHandler is a minimal ModuleHandler for testing collectModuleGVKs.
type mockModuleHandler struct {
	modules.BaseHandler

	enabled bool
}

func newMockModuleHandler(name string, moduleGVK schema.GroupVersionKind, enabled bool) *mockModuleHandler {
	return &mockModuleHandler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:   name,
				CRName: "default",
				GVK:    moduleGVK,
			},
		},
		enabled: enabled,
	}
}

func (m *mockModuleHandler) IsEnabled(_ *modules.PlatformContext) bool {
	return m.enabled
}

func (m *mockModuleHandler) BuildModuleCR(_ context.Context, _ client.Client, _ *modules.PlatformContext) (*unstructured.Unstructured, error) {
	return nil, nil
}

// Verify mockModuleHandler satisfies ModuleHandler at compile time.
var _ modules.ModuleHandler = (*mockModuleHandler)(nil)

func TestCollectModuleGVKs(t *testing.T) {
	t.Run("empty registry returns nil", func(t *testing.T) {
		g := NewWithT(t)
		reg := &modules.Registry{}

		result := collectModuleGVKs(reg)
		g.Expect(result).Should(BeNil())
	})

	t.Run("single module returns its GVK", func(t *testing.T) {
		g := NewWithT(t)
		reg := &modules.Registry{}

		expectedGVK := schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "AIGateway"}
		reg.Add(newMockModuleHandler("aigateway", expectedGVK, true))

		result := collectModuleGVKs(reg)
		g.Expect(result).Should(HaveLen(1))
		g.Expect(result[0]).Should(Equal(expectedGVK))
	})

	t.Run("multiple modules return all GVKs", func(t *testing.T) {
		g := NewWithT(t)
		reg := &modules.Registry{}

		gvk1 := schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "ModuleA"}
		gvk2 := schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "ModuleB"}
		reg.Add(newMockModuleHandler("module-a", gvk1, true))
		reg.Add(newMockModuleHandler("module-b", gvk2, true))

		result := collectModuleGVKs(reg)
		g.Expect(result).Should(HaveLen(2))
		g.Expect(result).Should(ConsistOf(gvk1, gvk2))
	})

	t.Run("disabled modules are included", func(t *testing.T) {
		g := NewWithT(t)
		reg := &modules.Registry{}

		enabledGVK := schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "Enabled"}
		disabledGVK := schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "Disabled"}
		reg.Add(newMockModuleHandler("enabled-mod", enabledGVK, true))
		reg.Add(newMockModuleHandler("disabled-mod", disabledGVK, false))
		reg.Disable("disabled-mod")

		result := collectModuleGVKs(reg)
		g.Expect(result).Should(HaveLen(2))
		g.Expect(result).Should(ConsistOf(enabledGVK, disabledGVK))
	})
}

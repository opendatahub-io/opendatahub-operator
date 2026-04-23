package modules_test

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"

	. "github.com/onsi/gomega"
)

type mockHandler struct {
	modules.BaseHandler

	enabled bool
}

func newMockHandler(name string, enabled bool) *mockHandler {
	return &mockHandler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:   name,
				CRName: "default",
				GVK:    schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "Mock"},
			},
		},
		enabled: enabled,
	}
}

func (m *mockHandler) IsEnabled(_ *dscv2.DataScienceCluster) bool {
	return m.enabled
}

func (m *mockHandler) BuildModuleCR(_ context.Context, _ client.Client, _ *dscv2.DataScienceCluster, _ *dsciv2.DSCInitialization) (*unstructured.Unstructured, error) {
	return nil, nil
}

// Verify mockHandler satisfies ModuleHandler at compile time.
var _ modules.ModuleHandler = (*mockHandler)(nil)

func TestRegistryAdd(t *testing.T) {
	g := NewWithT(t)
	reg := &modules.Registry{}

	h := newMockHandler("test-module", true)
	reg.Add(h)

	g.Expect(reg.IsEnabled("test-module")).Should(BeTrue())
	g.Expect(reg.HasEntries()).Should(BeTrue())
}

func TestRegistryDisableEnable(t *testing.T) {
	g := NewWithT(t)
	reg := &modules.Registry{}

	h := newMockHandler("test-module", true)
	reg.Add(h)

	g.Expect(reg.IsEnabled("test-module")).Should(BeTrue())

	reg.Disable("test-module")
	g.Expect(reg.IsEnabled("test-module")).Should(BeFalse())

	reg.Enable("test-module")
	g.Expect(reg.IsEnabled("test-module")).Should(BeTrue())
}

func TestRegistryDisableNonExistent(t *testing.T) {
	g := NewWithT(t)
	reg := &modules.Registry{}

	reg.Disable("does-not-exist")
	g.Expect(reg.IsEnabled("does-not-exist")).Should(BeFalse())
}

func TestRegistryForEachSkipsDisabled(t *testing.T) {
	g := NewWithT(t)
	reg := &modules.Registry{}

	reg.Add(newMockHandler("a", true))
	reg.Add(newMockHandler("b", true))
	reg.Disable("b")

	var visited []string
	err := reg.ForEach(func(h modules.ModuleHandler) error {
		visited = append(visited, h.GetName())
		return nil
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(visited).Should(ConsistOf("a"))
}

func TestRegistryForEachCollectsErrors(t *testing.T) {
	g := NewWithT(t)
	reg := &modules.Registry{}

	reg.Add(newMockHandler("a", true))
	reg.Add(newMockHandler("b", true))

	err := reg.ForEach(func(h modules.ModuleHandler) error {
		return errors.New("fail-" + h.GetName())
	})

	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("fail-"))
}

func TestRegistryEmptyForEachIsNoop(t *testing.T) {
	g := NewWithT(t)
	reg := &modules.Registry{}

	g.Expect(reg.HasEntries()).Should(BeFalse())

	err := reg.ForEach(func(_ modules.ModuleHandler) error {
		t.Fatal("should not be called")
		return nil
	})

	g.Expect(err).ShouldNot(HaveOccurred())
}

func TestRegistryIsModuleEnabled(t *testing.T) {
	g := NewWithT(t)
	reg := &modules.Registry{}

	enabledHandler := newMockHandler("enabled-mod", true)
	disabledHandler := newMockHandler("disabled-mod", false)

	reg.Add(enabledHandler)
	reg.Add(disabledHandler)

	dsc := &dscv2.DataScienceCluster{}

	g.Expect(reg.IsModuleEnabled("enabled-mod", dsc)).Should(BeTrue())
	g.Expect(reg.IsModuleEnabled("disabled-mod", dsc)).Should(BeFalse())
	g.Expect(reg.IsModuleEnabled("nonexistent", dsc)).Should(BeFalse())

	reg.Disable("enabled-mod")
	g.Expect(reg.IsModuleEnabled("enabled-mod", dsc)).Should(BeFalse())
}

func TestBaseHandlerDefaultsHelmOnly(t *testing.T) {
	g := NewWithT(t)

	h := &mockHandler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:        "helm-mod",
				CRName:      "default",
				GVK:         schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "Mock"},
				ChartDir:    "mymodule",
				ReleaseName: "mymodule-operator",
			},
		},
		enabled: true,
	}

	g.Expect(h.GetName()).Should(Equal("helm-mod"))
	g.Expect(h.GetGVK()).Should(Equal(schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "Mock"}))

	manifests := h.GetOperatorManifests()
	g.Expect(manifests.HelmCharts).Should(HaveLen(1))
	g.Expect(manifests.HelmCharts[0].ReleaseName).Should(Equal("mymodule-operator"))
	g.Expect(manifests.Manifests).Should(BeEmpty())
}

func TestBaseHandlerDefaultsKustomizeOnly(t *testing.T) {
	g := NewWithT(t)

	h := &mockHandler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:        "kustomize-mod",
				CRName:      "default",
				GVK:         schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "Mock"},
				ManifestDir: "mymodule",
				ContextDir:  "operator",
				SourcePath:  "overlays/production",
			},
		},
		enabled: true,
	}

	manifests := h.GetOperatorManifests()
	g.Expect(manifests.HelmCharts).Should(BeEmpty())
	g.Expect(manifests.Manifests).Should(HaveLen(1))
	g.Expect(manifests.Manifests[0].Path).Should(Equal("mymodule"))
	g.Expect(manifests.Manifests[0].ContextDir).Should(Equal("operator"))
	g.Expect(manifests.Manifests[0].SourcePath).Should(Equal("overlays/production"))
}

func TestBaseHandlerDefaultsNoManifests(t *testing.T) {
	g := NewWithT(t)

	h := newMockHandler("empty", true)

	manifests := h.GetOperatorManifests()
	g.Expect(manifests.HelmCharts).Should(BeEmpty())
	g.Expect(manifests.Manifests).Should(BeEmpty())
}

func TestParseConditions(t *testing.T) {
	g := NewWithT(t)

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"status": map[string]any{
				"conditions": []any{
					map[string]any{
						"type":               "Ready",
						"status":             "True",
						"reason":             "AllGood",
						"message":            "Everything is fine",
						"observedGeneration": float64(3),
						"lastTransitionTime": "2026-04-22T10:30:00Z",
					},
					map[string]any{
						"type":   "Degraded",
						"status": "False",
					},
				},
			},
		},
	}

	conditions, err := modules.ParseConditions(u)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(conditions).Should(HaveLen(2))
	g.Expect(conditions[0].Type).Should(Equal("Ready"))
	g.Expect(conditions[0].Status).Should(Equal(metav1.ConditionTrue))
	g.Expect(conditions[0].Reason).Should(Equal("AllGood"))
	g.Expect(conditions[0].Message).Should(Equal("Everything is fine"))
	g.Expect(conditions[0].ObservedGeneration).Should(Equal(int64(3)))
	g.Expect(conditions[0].LastTransitionTime.IsZero()).Should(BeFalse())
	g.Expect(conditions[1].Type).Should(Equal("Degraded"))
	g.Expect(conditions[1].Status).Should(Equal(metav1.ConditionFalse))
	g.Expect(conditions[1].ObservedGeneration).Should(Equal(int64(0)))
	g.Expect(conditions[1].LastTransitionTime.IsZero()).Should(BeTrue())
}

func TestParseConditionsNoStatus(t *testing.T) {
	g := NewWithT(t)

	u := &unstructured.Unstructured{
		Object: map[string]any{},
	}

	conditions, err := modules.ParseConditions(u)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(conditions).Should(BeNil())
}

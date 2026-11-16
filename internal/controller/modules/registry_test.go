package modules_test

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"

	. "github.com/onsi/gomega"
)

type mockHandler struct {
	modules.BaseHandler
}

func newMockHandler(name string) *mockHandler {
	return &mockHandler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:   name,
				CRName: "default",
				GVK:    schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "Mock"},
			},
		},
	}
}

func (m *mockHandler) IsEnabled(_ *configApi.PlatformModules) bool {
	return true
}

func (m *mockHandler) BuildModuleCR(_ context.Context, _ client.Client, _ *modules.DSCContext, _ *modules.ModuleCRConfig) (*unstructured.Unstructured, error) {
	return nil, nil
}

// Verify mockHandler satisfies ModuleHandler at compile time.
var _ modules.ModuleHandler = (*mockHandler)(nil)

func TestRegistryAdd(t *testing.T) {
	g := NewWithT(t)
	reg := &modules.Registry{}

	h := newMockHandler("test-module")
	reg.Add(h)

	g.Expect(reg.IsEnabled("test-module")).Should(BeTrue())
	g.Expect(reg.HasEntries()).Should(BeTrue())
}

func TestRegistryDisable(t *testing.T) {
	g := NewWithT(t)
	reg := &modules.Registry{}

	h := newMockHandler("test-module")
	reg.Add(h)

	g.Expect(reg.IsEnabled("test-module")).Should(BeTrue())

	reg.Disable("test-module")
	g.Expect(reg.IsEnabled("test-module")).Should(BeFalse())
}

func TestRegistryDisableNonExistent(t *testing.T) {
	g := NewWithT(t)
	reg := &modules.Registry{}

	reg.Disable("does-not-exist")
	g.Expect(reg.IsEnabled("does-not-exist")).Should(BeFalse())
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
	}

	g.Expect(h.GetName()).Should(Equal("helm-mod"))
	g.Expect(h.GetGVK()).Should(Equal(schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "Mock"}))

	platformCtx := &modules.PlatformContext{ApplicationsNamespace: "test-ns"}
	manifests := h.GetOperatorManifests(platformCtx)
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
	}

	manifests := h.GetOperatorManifests(nil)
	g.Expect(manifests.HelmCharts).Should(BeEmpty())
	g.Expect(manifests.Manifests).Should(HaveLen(1))
	g.Expect(manifests.Manifests[0].Path).Should(Equal("mymodule"))
	g.Expect(manifests.Manifests[0].ContextDir).Should(Equal("operator"))
	g.Expect(manifests.Manifests[0].SourcePath).Should(Equal("overlays/production"))
}

func TestBaseHandlerDefaultsNoManifests(t *testing.T) {
	g := NewWithT(t)

	h := newMockHandler("empty")

	manifests := h.GetOperatorManifests(nil)
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

// ParseConditions must preserve the severity field. A module CR reports an
// optional dependency as Info severity to signal it is non-gating; if that
// signal is dropped here, aggregation would treat it as a hard failure (Error
// == "") and flip the DSC to Not Ready. A condition without a severity field
// must default to Error ("").
func TestParseConditionsPreservesSeverity(t *testing.T) {
	g := NewWithT(t)

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"status": map[string]any{
				"conditions": []any{
					map[string]any{
						"type":     "KserveLLMInferenceServiceDependencies",
						"status":   "False",
						"reason":   "PreConditionFailed",
						"severity": "Info",
					},
					map[string]any{
						"type":   "Ready",
						"status": "True",
					},
				},
			},
		},
	}

	conditions, err := modules.ParseConditions(u)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(conditions).Should(HaveLen(2))

	g.Expect(conditions[0].Type).Should(Equal("KserveLLMInferenceServiceDependencies"))
	g.Expect(conditions[0].Severity).Should(Equal(common.ConditionSeverityInfo))

	// No severity field -> defaults to Error (empty string).
	g.Expect(conditions[1].Type).Should(Equal("Ready"))
	g.Expect(conditions[1].Severity).Should(Equal(common.ConditionSeverityError))
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

func TestLookup(t *testing.T) {
	g := NewWithT(t)
	reg := &modules.Registry{}

	h := newMockHandler("findme")
	reg.Add(h)

	g.Expect(reg.Lookup("findme")).Should(Equal(h))
	g.Expect(reg.Lookup("nonexistent")).Should(BeNil())
}

func TestForConfigSourceFiltersBySource(t *testing.T) {
	g := NewWithT(t)
	reg := &modules.Registry{}

	reg.Add(newMockHandler("dsc-mod"))
	reg.Add(newMockHandler("dsci-mod"), modules.WithConfigSource(modules.ConfigFromDSCI))
	reg.Add(newMockHandler("dsc-mod-2"))

	var dscVisited, dsciVisited []string
	_ = reg.ForConfigSource(modules.ConfigFromDSC, func(h modules.ModuleHandler, _ bool) error {
		dscVisited = append(dscVisited, h.GetName())
		return nil
	})
	_ = reg.ForConfigSource(modules.ConfigFromDSCI, func(h modules.ModuleHandler, _ bool) error {
		dsciVisited = append(dsciVisited, h.GetName())
		return nil
	})

	g.Expect(dscVisited).Should(ConsistOf("dsc-mod", "dsc-mod-2"))
	g.Expect(dsciVisited).Should(ConsistOf("dsci-mod"))
}

func TestRegisteredModuleDefaultsToDSCConfigured(t *testing.T) {
	g := NewWithT(t)
	reg := &modules.Registry{}

	reg.Add(newMockHandler("default-source"))

	var dscVisited, dsciVisited []string
	_ = reg.ForConfigSource(modules.ConfigFromDSC, func(h modules.ModuleHandler, _ bool) error {
		dscVisited = append(dscVisited, h.GetName())
		return nil
	})
	_ = reg.ForConfigSource(modules.ConfigFromDSCI, func(h modules.ModuleHandler, _ bool) error {
		dsciVisited = append(dsciVisited, h.GetName())
		return nil
	})

	g.Expect(dscVisited).Should(ConsistOf("default-source"))
	g.Expect(dsciVisited).Should(BeEmpty())
}

func TestRegistryForAllReportsStaticSuppression(t *testing.T) {
	g := NewWithT(t)
	reg := &modules.Registry{}

	reg.Add(newMockHandler("a"))
	reg.Add(newMockHandler("b"))
	reg.Disable("b")

	var visited []string
	err := reg.ForAll(func(h modules.ModuleHandler, registryEnabled bool) error {
		if !registryEnabled {
			return nil
		}
		visited = append(visited, h.GetName())
		return nil
	})

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(visited).Should(ConsistOf("a"))
}

func TestRegistryForAllCollectsErrors(t *testing.T) {
	g := NewWithT(t)
	reg := &modules.Registry{}

	reg.Add(newMockHandler("a"))
	reg.Add(newMockHandler("b"))

	err := reg.ForAll(func(h modules.ModuleHandler, _ bool) error {
		return errors.New("fail-" + h.GetName())
	})

	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("fail-a"))
	g.Expect(err.Error()).Should(ContainSubstring("fail-b"))
}

func TestRegistryEmptyForAllIsNoop(t *testing.T) {
	g := NewWithT(t)
	reg := &modules.Registry{}

	g.Expect(reg.HasEntries()).Should(BeFalse())

	err := reg.ForAll(func(_ modules.ModuleHandler, _ bool) error {
		t.Fatal("should not be called")
		return nil
	})

	g.Expect(err).ShouldNot(HaveOccurred())
}

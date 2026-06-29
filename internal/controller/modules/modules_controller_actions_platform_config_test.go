//nolint:testpackage
package modules

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	. "github.com/onsi/gomega"
)

func TestPlatformConfigName(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	g.Expect(PlatformConfigName("mymodule")).Should(Equal("odh-mymodule-config"))
	g.Expect(PlatformConfigName("aigateway")).Should(Equal("odh-aigateway-config"))
}

func TestBuildPlatformConfigMap(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	cm := buildPlatformConfigMap("odh-testmod-config", "test-ns", "2.20.0")

	g.Expect(cm.Name).Should(Equal("odh-testmod-config"))
	g.Expect(cm.Namespace).Should(Equal("test-ns"))
	g.Expect(cm.Data).Should(HaveKeyWithValue(PlatformVersionKey, "2.20.0"))
	g.Expect(cm.Kind).Should(Equal("ConfigMap"))
	g.Expect(cm.APIVersion).Should(Equal("v1"))
}

func TestMergePlatformKeys(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "odh-testmod-config",
				"namespace": "opendatahub",
			},
			"data": map[string]any{
				"LOG_LEVEL":    "info",
				"LEADER_ELECT": "true",
			},
		},
	}

	mergePlatformKeys(u, "2.20.0")

	data, _, _ := unstructured.NestedStringMap(u.Object, "data")
	g.Expect(data).Should(HaveKeyWithValue(PlatformVersionKey, "2.20.0"))
	g.Expect(data).Should(HaveKeyWithValue("LOG_LEVEL", "info"))
	g.Expect(data).Should(HaveKeyWithValue("LEADER_ELECT", "true"))
}

func TestMergePlatformKeys_NilData(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "odh-empty-config"},
		},
	}

	mergePlatformKeys(u, "2.20.0")

	data, _, _ := unstructured.NestedStringMap(u.Object, "data")
	g.Expect(data).Should(HaveKeyWithValue(PlatformVersionKey, "2.20.0"))
}

func TestMergePlatformKeys_OverwritesOldVersion(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "odh-mod-config"},
			"data": map[string]any{
				PlatformVersionKey: "2.19.0",
			},
		},
	}

	mergePlatformKeys(u, "2.20.0")

	data, _, _ := unstructured.NestedStringMap(u.Object, "data")
	g.Expect(data).Should(HaveKeyWithValue(PlatformVersionKey, "2.20.0"))
}

func TestMergePlatformKeys_UserEditedPlatformVar_ReconciledBack(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "odh-mod-config"},
			"data": map[string]any{
				PlatformVersionKey: "HACKED",
				"LOG_LEVEL":        "debug",
			},
		},
	}

	mergePlatformKeys(u, "2.20.0")

	data, _, _ := unstructured.NestedStringMap(u.Object, "data")
	g.Expect(data).Should(HaveKeyWithValue(PlatformVersionKey, "2.20.0"),
		"platform-managed key must be reconciled back to the correct value")
	g.Expect(data).Should(HaveKeyWithValue("LOG_LEVEL", "debug"),
		"module-owned key must not be affected by platform reconciliation")
}

func TestMergePlatformKeys_ModuleAddsNewKeys_Preserved(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "odh-mod-config"},
			"data": map[string]any{
				PlatformVersionKey: "2.20.0",
				"LOG_LEVEL":        "info",
				"FEATURE_FLAG_X":   "enabled",
			},
		},
	}

	mergePlatformKeys(u, "2.20.0")

	data, _, _ := unstructured.NestedStringMap(u.Object, "data")
	g.Expect(data).Should(HaveLen(3),
		"platform merge must not add or remove non-platform keys")
	g.Expect(data).Should(HaveKeyWithValue("LOG_LEVEL", "info"))
	g.Expect(data).Should(HaveKeyWithValue("FEATURE_FLAG_X", "enabled"))
	g.Expect(data).Should(HaveKeyWithValue(PlatformVersionKey, "2.20.0"))
}

func TestMergePlatformKeys_ModuleChangesOwnKeys_NotReverted(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "odh-mod-config"},
			"data": map[string]any{
				PlatformVersionKey: "2.20.0",
				"LOG_LEVEL":        "debug",
			},
		},
	}

	mergePlatformKeys(u, "2.20.0")

	data, _, _ := unstructured.NestedStringMap(u.Object, "data")
	g.Expect(data).Should(HaveKeyWithValue("LOG_LEVEL", "debug"),
		"module-owned key changes must be preserved across platform merge")
	g.Expect(data).Should(HaveKeyWithValue(PlatformVersionKey, "2.20.0"))
}

func TestIndexConfigMapsByName(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	resources := []unstructured.Unstructured{
		{Object: map[string]any{
			"kind":     "Deployment",
			"metadata": map[string]any{"name": "dep-1"},
		}},
		{Object: map[string]any{
			"kind":     "ConfigMap",
			"metadata": map[string]any{"name": "odh-mod-config"},
		}},
		{Object: map[string]any{
			"kind":     "ConfigMap",
			"metadata": map[string]any{"name": "other-cm"},
		}},
	}

	idx := indexConfigMapsByName(resources)
	g.Expect(idx).Should(HaveLen(2))
	g.Expect(idx).Should(HaveKeyWithValue("odh-mod-config", 1))
	g.Expect(idx).Should(HaveKeyWithValue("other-cm", 2))
}

func TestToUnstructured(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	cm := buildPlatformConfigMap("odh-test-config", "test-ns", "1.0.0")
	u, err := toUnstructured(cm)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetKind()).Should(Equal("ConfigMap"))
	g.Expect(u.GetName()).Should(Equal("odh-test-config"))
	g.Expect(u.GetNamespace()).Should(Equal("test-ns"))

	data, _, _ := unstructured.NestedStringMap(u.Object, "data")
	g.Expect(data).Should(HaveKeyWithValue(PlatformVersionKey, "1.0.0"))
}

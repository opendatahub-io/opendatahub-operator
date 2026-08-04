//nolint:testpackage
package modules

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"

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

	cm := buildPlatformConfigMap("odh-testmod-config", "test-ns", "2.20.0", nil)

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

	mergePlatformKeys(u, "2.20.0", nil)

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

	mergePlatformKeys(u, "2.20.0", nil)

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

	mergePlatformKeys(u, "2.20.0", nil)

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

	mergePlatformKeys(u, "2.20.0", nil)

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

	mergePlatformKeys(u, "2.20.0", nil)

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

	mergePlatformKeys(u, "2.20.0", nil)

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

	cm := buildPlatformConfigMap("odh-test-config", "test-ns", "1.0.0", nil)
	u, err := toUnstructured(cm)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetKind()).Should(Equal("ConfigMap"))
	g.Expect(u.GetName()).Should(Equal("odh-test-config"))
	g.Expect(u.GetNamespace()).Should(Equal("test-ns"))

	data, _, _ := unstructured.NestedStringMap(u.Object, "data")
	g.Expect(data).Should(HaveKeyWithValue(PlatformVersionKey, "1.0.0"))
}

func TestBuildCertManagerConfigParams_Defaults(t *testing.T) {
	g := NewWithT(t)

	for _, envVar := range []string{
		certmanager.EnvCAIssuerName,
		certmanager.EnvIssuerRefKind,
		certmanager.EnvCertName,
		certmanager.EnvCertManagerNS,
		certmanager.EnvIstioCACertPath,
	} {
		t.Setenv(envVar, "")
	}

	params := buildCertManagerConfigParams()

	bc := certmanager.DefaultBootstrapConfig()
	g.Expect(params).Should(HaveKeyWithValue(CertManagerIssuerRefNameKey, bc.CAIssuerName))
	g.Expect(params).Should(HaveKeyWithValue(CertManagerIssuerRefKindKey, certmanager.DefaultIssuerRefKind))
	g.Expect(params).Should(HaveKeyWithValue(CertManagerCASecretNameKey, bc.CertName))
	g.Expect(params).Should(HaveKeyWithValue(CertManagerCASecretNamespaceKey, bc.CertManagerNamespace))
	g.Expect(params).ShouldNot(HaveKey(CertManagerIstioCACertPathKey))
}

func TestBuildCertManagerConfigParams_EnvOverrides(t *testing.T) {
	g := NewWithT(t)

	t.Setenv(certmanager.EnvCAIssuerName, "custom-issuer")
	t.Setenv(certmanager.EnvIssuerRefKind, "Issuer")
	t.Setenv(certmanager.EnvCertName, "custom-ca")
	t.Setenv(certmanager.EnvCertManagerNS, "custom-ns")
	t.Setenv(certmanager.EnvIstioCACertPath, "/custom/ca.crt")

	params := buildCertManagerConfigParams()

	g.Expect(params).Should(HaveKeyWithValue(CertManagerIssuerRefNameKey, "custom-issuer"))
	g.Expect(params).Should(HaveKeyWithValue(CertManagerIssuerRefKindKey, "Issuer"))
	g.Expect(params).Should(HaveKeyWithValue(CertManagerCASecretNameKey, "custom-ca"))
	g.Expect(params).Should(HaveKeyWithValue(CertManagerCASecretNamespaceKey, "custom-ns"))
	g.Expect(params).Should(HaveKeyWithValue(CertManagerIstioCACertPathKey, "/custom/ca.crt"))
}

func TestBuildPlatformConfigMap_WithExtraParams(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	extra := map[string]string{
		CertManagerIssuerRefNameKey: "my-issuer",
		CertManagerIssuerRefKindKey: "ClusterIssuer",
	}

	cm := buildPlatformConfigMap("odh-mod-config", "test-ns", "2.20.0", extra)

	g.Expect(cm.Data).Should(HaveKeyWithValue(PlatformVersionKey, "2.20.0"))
	g.Expect(cm.Data).Should(HaveKeyWithValue(CertManagerIssuerRefNameKey, "my-issuer"))
	g.Expect(cm.Data).Should(HaveKeyWithValue(CertManagerIssuerRefKindKey, "ClusterIssuer"))
}

func TestBuildPlatformConfigMap_NilExtraParams(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	cm := buildPlatformConfigMap("odh-mod-config", "test-ns", "2.20.0", nil)

	g.Expect(cm.Data).Should(HaveLen(1))
	g.Expect(cm.Data).Should(HaveKeyWithValue(PlatformVersionKey, "2.20.0"))
}

func TestMergePlatformKeys_WithExtraParams(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "odh-mod-config"},
			"data": map[string]any{
				"LOG_LEVEL": "info",
			},
		},
	}

	extra := map[string]string{
		CertManagerCASecretNameKey:      "my-ca",
		CertManagerCASecretNamespaceKey: "cert-manager",
	}

	mergePlatformKeys(u, "2.20.0", extra)

	data, _, _ := unstructured.NestedStringMap(u.Object, "data")
	g.Expect(data).Should(HaveKeyWithValue(PlatformVersionKey, "2.20.0"))
	g.Expect(data).Should(HaveKeyWithValue(CertManagerCASecretNameKey, "my-ca"))
	g.Expect(data).Should(HaveKeyWithValue(CertManagerCASecretNamespaceKey, "cert-manager"))
	g.Expect(data).Should(HaveKeyWithValue("LOG_LEVEL", "info"))
}

func TestMergePlatformKeys_RemovesStaleCertManagerKeys(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "odh-mod-config"},
			"data": map[string]any{
				PlatformVersionKey:          "2.19.0",
				CertManagerIssuerRefNameKey: "old-issuer",
				CertManagerIssuerRefKindKey: "ClusterIssuer",
				CertManagerCASecretNameKey:  "old-ca",
				"LOG_LEVEL":                 "info",
			},
		},
	}

	mergePlatformKeys(u, "2.20.0", nil)

	data, _, _ := unstructured.NestedStringMap(u.Object, "data")
	g.Expect(data).Should(HaveKeyWithValue(PlatformVersionKey, "2.20.0"))
	g.Expect(data).Should(HaveKeyWithValue("LOG_LEVEL", "info"))
	g.Expect(data).ShouldNot(HaveKey(CertManagerIssuerRefNameKey),
		"stale cert-manager keys must be removed when extraParams is nil")
	g.Expect(data).ShouldNot(HaveKey(CertManagerIssuerRefKindKey))
	g.Expect(data).ShouldNot(HaveKey(CertManagerCASecretNameKey))
}

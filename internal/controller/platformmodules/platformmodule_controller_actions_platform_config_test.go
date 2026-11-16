//nolint:testpackage
package platformmodules

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

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
	g.Expect(cm.Kind).Should(Equal(gvk.ConfigMap.Kind))
	g.Expect(cm.APIVersion).Should(Equal("v1"))
}

func TestInjectModulePlatformConfigUpdatesDataWithoutChangingDeployment(t *testing.T) {
	g := NewWithT(t)
	withCleanRegistries(t)
	modules.Add(&stubModuleHandler{BaseHandler: modules.BaseHandler{Config: modules.ModuleConfig{Name: "testmod"}}})
	dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"}}
	dsci.Spec.ApplicationsNamespace = "apps"
	cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
	g.Expect(err).NotTo(HaveOccurred())
	deployment := makeDeployment("testmod-operator")
	before := deployment.DeepCopy()
	rr := &odhtype.ReconciliationRequest{
		Client: cli, Instance: &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "testmod"}},
		Release: testRelease("1.0.0"), Resources: []unstructured.Unstructured{deployment},
	}
	g.Expect(injectModulePlatformConfig(t.Context(), rr)).To(Succeed())
	g.Expect(rr.Resources).To(HaveLen(2))
	config := &rr.Resources[1]
	g.Expect(config.GroupVersionKind()).To(Equal(gvk.ConfigMap))
	g.Expect(config.GetName()).To(Equal("odh-testmod-config"))
	g.Expect(config.GetNamespace()).To(Equal("apps"))
	g.Expect(unstructured.SetNestedField(config.Object, "debug", "data", "LOG_LEVEL")).To(Succeed())
	rr.Release = testRelease("2.0.0")
	g.Expect(injectModulePlatformConfig(t.Context(), rr)).To(Succeed())
	g.Expect(rr.Resources).To(HaveLen(2))
	data, _, err := unstructured.NestedStringMap(config.Object, "data")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(data).To(Equal(map[string]string{"platformVersion": "2.0.0", "LOG_LEVEL": "debug"}))
	g.Expect(rr.Resources[0]).To(Equal(*before), "updating platform config must not change the operator pod template")
}

func TestMergePlatformKeys(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       gvk.ConfigMap.Kind,
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
			"kind":       gvk.ConfigMap.Kind,
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
			"kind":       gvk.ConfigMap.Kind,
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
			"kind":       gvk.ConfigMap.Kind,
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
			"kind":       gvk.ConfigMap.Kind,
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
			"kind":       gvk.ConfigMap.Kind,
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
			"kind":     gvk.Deployment.Kind,
			"metadata": map[string]any{"name": "dep-1"},
		}},
		{Object: map[string]any{
			"kind":     gvk.ConfigMap.Kind,
			"metadata": map[string]any{"name": "odh-mod-config"},
		}},
		{Object: map[string]any{
			"kind":     gvk.ConfigMap.Kind,
			"metadata": map[string]any{"name": "other-cm"},
		}},
	}

	idx := indexConfigMapsByName(resources)
	g.Expect(idx).Should(HaveLen(2))
	g.Expect(idx).Should(HaveKeyWithValue("odh-mod-config", 1))
	g.Expect(idx).Should(HaveKeyWithValue("other-cm", 2))
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
			"kind":       gvk.ConfigMap.Kind,
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
			"kind":       gvk.ConfigMap.Kind,
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

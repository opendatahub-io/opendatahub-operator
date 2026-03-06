//nolint:testpackage
package kserve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/env"

	. "github.com/onsi/gomega"
)

func TestBuildCertManagerParams_ReturnsDefaultsWhenEnvUnset(t *testing.T) {
	g := NewWithT(t)

	// Clear all env vars to ensure defaults are used.
	t.Setenv(envIssuerRefName, "")
	t.Setenv(envIssuerRefKind, "")
	t.Setenv(envCASecretName, "")
	t.Setenv(envCASecretNamespace, "")
	t.Setenv(envIstioCACertificatePath, "")
	t.Setenv(envApplicationsNamespace, "")

	result := buildCertManagerParams()
	g.Expect(result).ShouldNot(BeNil())
	g.Expect(result).Should(HaveLen(7))
	g.Expect(result["ISSUER_REF_NAME"]).Should(Equal(defaultIssuerRefName))
	g.Expect(result["ISSUER_REF_KIND"]).Should(Equal(defaultIssuerRefKind))
	g.Expect(result["ISSUER_REF_GROUP"]).Should(Equal(defaultIssuerRefGroup))
	g.Expect(result["CA_SECRET_NAME"]).Should(Equal(defaultCASecretName))
	g.Expect(result["CA_SECRET_NAMESPACE"]).Should(Equal(defaultCASecretNamespace))
	g.Expect(result["ISTIO_CA_CERTIFICATE_PATH"]).Should(Equal(defaultIstioCACertificatePath))
	g.Expect(result["NAMESPACE"]).Should(Equal(defaultApplicationsNamespace))
}

func TestBuildCertManagerParams_RespectsAllEnvOverrides(t *testing.T) {
	g := NewWithT(t)

	t.Setenv(envIssuerRefName, "custom-issuer")
	t.Setenv(envIssuerRefKind, "Issuer")
	t.Setenv(envCASecretName, "custom-ca")
	t.Setenv(envCASecretNamespace, "custom-ns")
	t.Setenv(envIstioCACertificatePath, "/custom/path/ca.crt")
	t.Setenv(envApplicationsNamespace, "my-app-ns")

	result := buildCertManagerParams()
	g.Expect(result).ShouldNot(BeNil())
	g.Expect(result["ISSUER_REF_NAME"]).Should(Equal("custom-issuer"))
	g.Expect(result["ISSUER_REF_KIND"]).Should(Equal("Issuer"))
	g.Expect(result["ISSUER_REF_GROUP"]).Should(Equal(defaultIssuerRefGroup))
	g.Expect(result["CA_SECRET_NAME"]).Should(Equal("custom-ca"))
	g.Expect(result["CA_SECRET_NAMESPACE"]).Should(Equal("custom-ns"))
	g.Expect(result["ISTIO_CA_CERTIFICATE_PATH"]).Should(Equal("/custom/path/ca.crt"))
	g.Expect(result["NAMESPACE"]).Should(Equal("my-app-ns"))
}

func TestGetEnvOrDefault(t *testing.T) {
	g := NewWithT(t)

	t.Run("returns env value when set", func(t *testing.T) {
		t.Setenv("TEST_GET_ENV_OR_DEFAULT", "from-env")
		g.Expect(env.GetOrDefault("TEST_GET_ENV_OR_DEFAULT", "fallback")).Should(Equal("from-env"))
	})

	t.Run("returns default when env unset", func(t *testing.T) {
		t.Setenv("TEST_GET_ENV_OR_DEFAULT", "")
		g.Expect(env.GetOrDefault("TEST_GET_ENV_OR_DEFAULT", "fallback")).Should(Equal("fallback"))
	})
}

func TestInit_InjectsCertManagerParamsOnXKS(t *testing.T) {
	g := NewWithT(t)

	// Create a temporary directory structure that mimics the manifest layout.
	// DefaultManifestPath is evaluated at package init, so t.Setenv is too late;
	// override the package var directly and restore after the test.
	tmpDir := t.TempDir()
	origPath := odhdeploy.DefaultManifestPath
	odhdeploy.DefaultManifestPath = tmpDir
	t.Cleanup(func() { odhdeploy.DefaultManifestPath = origPath })

	// Create the xKS overlay params.env with placeholder values.
	xksDir := filepath.Join(tmpDir, componentName, kserveManifestSourcePathXKS)
	g.Expect(os.MkdirAll(xksDir, 0o755)).Should(Succeed())

	paramsContent := `NAMESPACE=placeholder
ISSUER_REF_NAME=placeholder
ISSUER_REF_KIND=placeholder
ISSUER_REF_GROUP=placeholder
CA_SECRET_NAME=placeholder
CA_SECRET_NAMESPACE=placeholder
ISTIO_CA_CERTIFICATE_PATH=placeholder
`
	g.Expect(os.WriteFile(filepath.Join(xksDir, "params.env"), []byte(paramsContent), 0o600)).Should(Succeed())

	// Also create the odh overlay params.env (required by Init).
	odhDir := filepath.Join(tmpDir, componentName, kserveManifestSourcePath)
	g.Expect(os.MkdirAll(odhDir, 0o755)).Should(Succeed())
	g.Expect(os.WriteFile(filepath.Join(odhDir, "params.env"), []byte(""), 0o600)).Should(Succeed())

	// Set cert-manager config env vars.
	t.Setenv(envIssuerRefName, "test-issuer")
	t.Setenv(envApplicationsNamespace, "test-ns")

	handler := &componentHandler{}
	err := handler.Init(cluster.XKS)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Read back the xKS params.env and verify values were injected.
	data, err := os.ReadFile(filepath.Join(xksDir, "params.env"))
	g.Expect(err).ShouldNot(HaveOccurred())

	content := string(data)
	g.Expect(content).Should(ContainSubstring("ISSUER_REF_NAME=test-issuer"))
	g.Expect(content).Should(ContainSubstring("NAMESPACE=test-ns"))
	g.Expect(content).Should(ContainSubstring("ISSUER_REF_KIND=" + defaultIssuerRefKind))
	g.Expect(content).Should(ContainSubstring("ISSUER_REF_GROUP=" + defaultIssuerRefGroup))
	g.Expect(content).Should(ContainSubstring("CA_SECRET_NAME=" + defaultCASecretName))
	g.Expect(content).Should(ContainSubstring("CA_SECRET_NAMESPACE=" + defaultCASecretNamespace))
	g.Expect(content).Should(ContainSubstring("ISTIO_CA_CERTIFICATE_PATH=" + defaultIstioCACertificatePath))
}

func TestInit_SkipsCertParamsOnNonXKSPlatform(t *testing.T) {
	g := NewWithT(t)

	tmpDir := t.TempDir()
	origPath := odhdeploy.DefaultManifestPath
	odhdeploy.DefaultManifestPath = tmpDir
	t.Cleanup(func() { odhdeploy.DefaultManifestPath = origPath })

	// Create the odh overlay params.env (required by Init).
	odhDir := filepath.Join(tmpDir, componentName, kserveManifestSourcePath)
	g.Expect(os.MkdirAll(odhDir, 0o755)).Should(Succeed())
	g.Expect(os.WriteFile(filepath.Join(odhDir, "params.env"), []byte(""), 0o600)).Should(Succeed())

	// Create xKS overlay params.env with placeholders.
	xksDir := filepath.Join(tmpDir, componentName, kserveManifestSourcePathXKS)
	g.Expect(os.MkdirAll(xksDir, 0o755)).Should(Succeed())

	original := "ISSUER_REF_NAME=placeholder\n"
	g.Expect(os.WriteFile(filepath.Join(xksDir, "params.env"), []byte(original), 0o600)).Should(Succeed())

	// Set env vars (irrelevant since platform isn't XKS).
	t.Setenv(envIssuerRefName, "test-issuer")

	handler := &componentHandler{}
	// Use OpenDataHub platform (not XKS) — cert params should NOT be injected.
	err := handler.Init(cluster.OpenDataHub)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify xKS params.env was NOT modified.
	data, err := os.ReadFile(filepath.Join(xksDir, "params.env"))
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(string(data)).Should(Equal(original))
}

func TestInit_NoErrorWhenXKSOverlayMissing(t *testing.T) {
	g := NewWithT(t)

	tmpDir := t.TempDir()
	origPath := odhdeploy.DefaultManifestPath
	odhdeploy.DefaultManifestPath = tmpDir
	t.Cleanup(func() { odhdeploy.DefaultManifestPath = origPath })

	// Create only the odh overlay — xKS overlay does not exist on disk.
	odhDir := filepath.Join(tmpDir, componentName, kserveManifestSourcePath)
	g.Expect(os.MkdirAll(odhDir, 0o755)).Should(Succeed())
	g.Expect(os.WriteFile(filepath.Join(odhDir, "params.env"), []byte(""), 0o600)).Should(Succeed())

	// Set cert-manager config (will be used since platform is XKS).
	t.Setenv(envIssuerRefName, "test-issuer")

	handler := &componentHandler{}
	err := handler.Init(cluster.XKS)
	// ApplyParams safely no-ops when params.env doesn't exist.
	g.Expect(err).ShouldNot(HaveOccurred())
}

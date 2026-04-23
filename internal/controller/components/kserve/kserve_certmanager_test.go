//nolint:testpackage
package kserve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"

	. "github.com/onsi/gomega"
)

func TestInit_InjectsCertManagerParamsFromEnv(t *testing.T) {
	g := NewWithT(t)

	// Clear all RHAI_* env vars to make the test hermetic.
	for _, envVar := range []string{
		certmanager.EnvCAIssuerName,
		certmanager.EnvIssuerRefKind,
		certmanager.EnvCertName,
		certmanager.EnvCertManagerNS,
		certmanager.EnvIstioCACertPath,
	} {
		t.Setenv(envVar, "")
	}

	tmpDir := t.TempDir()

	// Create the xKS overlay params.env with default values.
	xksDir := filepath.Join(tmpDir, componentName, kserveManifestSourcePathXKS)
	g.Expect(os.MkdirAll(xksDir, 0o755)).Should(Succeed())

	paramsContent := `NAMESPACE=opendatahub
ISSUER_REF_NAME=opendatahub-ca-issuer
ISSUER_REF_KIND=ClusterIssuer
ISSUER_REF_GROUP=cert-manager.io
CA_SECRET_NAME=opendatahub-ca
CA_SECRET_NAMESPACE=cert-manager
ISTIO_CA_CERTIFICATE_PATH=/var/run/secrets/opendatahub/ca.crt
`
	g.Expect(os.WriteFile(filepath.Join(xksDir, "params.env"), []byte(paramsContent), 0o600)).Should(Succeed())

	// Create the odh overlay params.env (required by Init).
	odhDir := filepath.Join(tmpDir, componentName, kserveManifestSourcePath)
	g.Expect(os.MkdirAll(odhDir, 0o755)).Should(Succeed())
	g.Expect(os.WriteFile(filepath.Join(odhDir, "params.env"), []byte(""), 0o600)).Should(Succeed())

	// Override issuer via env var; the rest should stay at defaults.
	t.Setenv(certmanager.EnvCAIssuerName, "test-issuer")

	handler := &componentHandler{}
	err := handler.Init(cluster.XKS, operatorconfig.OperatorSettings{ManifestsBasePath: tmpDir})
	g.Expect(err).ShouldNot(HaveOccurred())

	// Read back and verify: overridden values updated, rest unchanged.
	data, err := os.ReadFile(filepath.Join(xksDir, "params.env"))
	g.Expect(err).ShouldNot(HaveOccurred())

	content := string(data)
	g.Expect(content).Should(ContainSubstring("ISSUER_REF_NAME=test-issuer"))
	// NAMESPACE comes from cluster.GetApplicationNamespace() (defaults to "opendatahub" in tests).
	g.Expect(content).Should(ContainSubstring("NAMESPACE=opendatahub"))
	// Unset env vars → kustomize defaults preserved.
	g.Expect(content).Should(ContainSubstring("ISSUER_REF_KIND=ClusterIssuer"))
	g.Expect(content).Should(ContainSubstring("ISSUER_REF_GROUP=cert-manager.io"))
	g.Expect(content).Should(ContainSubstring("CA_SECRET_NAME=opendatahub-ca"))
	g.Expect(content).Should(ContainSubstring("CA_SECRET_NAMESPACE=cert-manager"))
	g.Expect(content).Should(ContainSubstring("ISTIO_CA_CERTIFICATE_PATH=/var/run/secrets/opendatahub/ca.crt"))
}

func TestInit_PreservesDefaultsWhenEnvVarsUnset(t *testing.T) {
	g := NewWithT(t)

	// Clear all RHAI_* env vars.
	for _, envVar := range []string{
		certmanager.EnvCAIssuerName,
		certmanager.EnvIssuerRefKind,
		certmanager.EnvCertName,
		certmanager.EnvCertManagerNS,
		certmanager.EnvIstioCACertPath,
	} {
		t.Setenv(envVar, "")
	}

	tmpDir := t.TempDir()

	// Create the xKS overlay params.env with default values.
	xksDir := filepath.Join(tmpDir, componentName, kserveManifestSourcePathXKS)
	g.Expect(os.MkdirAll(xksDir, 0o755)).Should(Succeed())

	original := `NAMESPACE=opendatahub
ISSUER_REF_NAME=opendatahub-ca-issuer
ISSUER_REF_KIND=ClusterIssuer
ISSUER_REF_GROUP=cert-manager.io
CA_SECRET_NAME=opendatahub-ca
CA_SECRET_NAMESPACE=cert-manager
ISTIO_CA_CERTIFICATE_PATH=/var/run/secrets/opendatahub/ca.crt
`
	g.Expect(os.WriteFile(filepath.Join(xksDir, "params.env"), []byte(original), 0o600)).Should(Succeed())

	// Create the odh overlay params.env (required by Init).
	odhDir := filepath.Join(tmpDir, componentName, kserveManifestSourcePath)
	g.Expect(os.MkdirAll(odhDir, 0o755)).Should(Succeed())
	g.Expect(os.WriteFile(filepath.Join(odhDir, "params.env"), []byte(""), 0o600)).Should(Succeed())

	handler := &componentHandler{}
	err := handler.Init(cluster.XKS, operatorconfig.OperatorSettings{ManifestsBasePath: tmpDir})
	g.Expect(err).ShouldNot(HaveOccurred())

	// No env vars set → params.env should remain unchanged.
	data, err := os.ReadFile(filepath.Join(xksDir, "params.env"))
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(string(data)).Should(Equal(original))
}

func TestBuildCertManagerParams_ConsistentWithBootstrapConfig(t *testing.T) {
	g := NewWithT(t)

	t.Setenv(certmanager.EnvCAIssuerName, "custom-issuer")
	t.Setenv(certmanager.EnvCertName, "custom-ca")
	t.Setenv(certmanager.EnvCertManagerNS, "custom-ns")
	t.Setenv(certmanager.EnvIssuerRefKind, "Issuer")
	t.Setenv(certmanager.EnvIstioCACertPath, "/custom/ca.crt")

	bc := certmanager.DefaultBootstrapConfig()
	params := buildCertManagerParams()

	g.Expect(params["ISSUER_REF_NAME"]).To(Equal(bc.CAIssuerName),
		"params and bootstrap config should resolve CAIssuerName identically")
	g.Expect(params["CA_SECRET_NAME"]).To(Equal(bc.CertName),
		"params and bootstrap config should resolve CertName identically")
	g.Expect(params["CA_SECRET_NAMESPACE"]).To(Equal(bc.CertManagerNamespace),
		"params and bootstrap config should resolve CertManagerNamespace identically")
	g.Expect(params["ISSUER_REF_KIND"]).To(Equal("Issuer"))
	g.Expect(params["ISTIO_CA_CERTIFICATE_PATH"]).To(Equal("/custom/ca.crt"))
}

func TestInit_NoErrorWhenXKSOverlayMissing(t *testing.T) {
	g := NewWithT(t)

	tmpDir := t.TempDir()

	// Create only the odh overlay — xKS overlay does not exist on disk.
	odhDir := filepath.Join(tmpDir, componentName, kserveManifestSourcePath)
	g.Expect(os.MkdirAll(odhDir, 0o755)).Should(Succeed())
	g.Expect(os.WriteFile(filepath.Join(odhDir, "params.env"), []byte(""), 0o600)).Should(Succeed())

	handler := &componentHandler{}
	err := handler.Init(cluster.XKS, operatorconfig.OperatorSettings{ManifestsBasePath: tmpDir})
	// ApplyParams safely no-ops when params.env doesn't exist.
	g.Expect(err).ShouldNot(HaveOccurred())
}

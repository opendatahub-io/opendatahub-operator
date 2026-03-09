//nolint:testpackage
package kserve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"

	. "github.com/onsi/gomega"
)

func TestInit_InjectsCertManagerParamsFromEnv(t *testing.T) {
	g := NewWithT(t)

	// Clear all RHAI_* env vars to make the test hermetic.
	for _, envVar := range []string{
		"RHAI_APPLICATIONS_NAMESPACE",
		"RHAI_ISSUER_REF_NAME",
		"RHAI_ISSUER_REF_KIND",
		"RHAI_CA_SECRET_NAME",
		"RHAI_CA_SECRET_NAMESPACE",
		"RHAI_ISTIO_CA_CERTIFICATE_PATH",
	} {
		t.Setenv(envVar, "")
	}

	tmpDir := t.TempDir()
	origPath := odhdeploy.DefaultManifestPath
	odhdeploy.DefaultManifestPath = tmpDir
	t.Cleanup(func() { odhdeploy.DefaultManifestPath = origPath })

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

	// Override two params via env vars; the rest should stay at kustomize defaults.
	t.Setenv("RHAI_ISSUER_REF_NAME", "test-issuer")
	t.Setenv("RHAI_APPLICATIONS_NAMESPACE", "test-ns")

	handler := &componentHandler{}
	err := handler.Init(cluster.XKS)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Read back and verify: overridden values updated, rest unchanged.
	data, err := os.ReadFile(filepath.Join(xksDir, "params.env"))
	g.Expect(err).ShouldNot(HaveOccurred())

	content := string(data)
	g.Expect(content).Should(ContainSubstring("ISSUER_REF_NAME=test-issuer"))
	g.Expect(content).Should(ContainSubstring("NAMESPACE=test-ns"))
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
		"RHAI_APPLICATIONS_NAMESPACE",
		"RHAI_ISSUER_REF_NAME",
		"RHAI_ISSUER_REF_KIND",
		"RHAI_CA_SECRET_NAME",
		"RHAI_CA_SECRET_NAMESPACE",
		"RHAI_ISTIO_CA_CERTIFICATE_PATH",
	} {
		t.Setenv(envVar, "")
	}

	tmpDir := t.TempDir()
	origPath := odhdeploy.DefaultManifestPath
	odhdeploy.DefaultManifestPath = tmpDir
	t.Cleanup(func() { odhdeploy.DefaultManifestPath = origPath })

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
	err := handler.Init(cluster.XKS)
	g.Expect(err).ShouldNot(HaveOccurred())

	// No env vars set → params.env should remain unchanged.
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

	handler := &componentHandler{}
	err := handler.Init(cluster.XKS)
	// ApplyParams safely no-ops when params.env doesn't exist.
	g.Expect(err).ShouldNot(HaveOccurred())
}

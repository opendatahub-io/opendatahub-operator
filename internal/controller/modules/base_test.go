package modules_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

// TestGetOperatorManifests_KustomizeOverlaySelection verifies the per-platform
// overlay is selected and the manifests base path is joined with ManifestDir.
func TestGetOperatorManifests_KustomizeOverlaySelection(t *testing.T) {
	cfg := modules.ModuleConfig{
		ManifestDir: "mymodule",
		SourcePath:  "somepath",
		SourcePathByPlatform: map[common.Platform]string{
			cluster.OpenDataHub:      "overlays/a",
			cluster.SelfManagedRhoai: "overlays/b",
		},
	}

	tests := []struct {
		name       string
		platform   common.Platform
		wantSource string
	}{
		{"odh", cluster.OpenDataHub, "overlays/a"},
		{"self-managed rhoai", cluster.SelfManagedRhoai, "overlays/b"},
		{"non-supported-managedrhoai", cluster.ManagedRhoai, "somepath"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			b := modules.BaseHandler{Config: cfg}

			out := b.GetOperatorManifests(&modules.PlatformContext{
				ManifestsBasePath: "/opt/manifests",
				Release:           common.Release{Name: tt.platform},
			})

			g.Expect(out.Manifests).Should(HaveLen(1))
			g.Expect(out.Manifests[0].Path).Should(Equal("/opt/manifests/mymodule"))
			g.Expect(out.Manifests[0].SourcePath).Should(Equal(tt.wantSource))
		})
	}
}

func TestGetOperatorManifests_NoBasePathFallback(t *testing.T) {
	g := NewWithT(t)
	b := modules.BaseHandler{Config: modules.ModuleConfig{ManifestDir: "mymodule"}}

	out := b.GetOperatorManifests(&modules.PlatformContext{})

	g.Expect(out.Manifests).Should(HaveLen(1))
	g.Expect(out.Manifests[0].Path).Should(Equal("mymodule"))
}

func writeParamsModule(g *WithT, base, content string) string {
	paramsDir := filepath.Join(base, "mymodule", "base")
	g.Expect(os.MkdirAll(paramsDir, 0o750)).Should(Succeed())
	paramsFile := filepath.Join(paramsDir, "params.env")
	g.Expect(os.WriteFile(paramsFile, []byte(content), 0o600)).Should(Succeed())
	return paramsFile
}

func paramsModuleHandler() modules.BaseHandler {
	return modules.BaseHandler{Config: modules.ModuleConfig{
		ManifestDir:   "mymodule",
		ParamsPath:    "base",
		SourcePath:    "overlays/odh",
		ImageParamMap: map[string]string{"MYIMAGE_IMAGE": "RELATED_IMAGE_MYIMAGE"},
	}}
}

// positive case: replace what is set in params.env with the RELATED_IMAGE value.
func TestGetOperatorManifests_AppliesImageParams(t *testing.T) {
	g := NewWithT(t)

	base := t.TempDir()
	paramsFile := writeParamsModule(g, base, "MYIMAGE_IMAGE=placeholder\n")

	t.Setenv("RELATED_IMAGE_MYIMAGE", "quay.io/opendatahub-io/myimage@sha256:123456789")

	b := paramsModuleHandler()
	out := b.GetOperatorManifests(&modules.PlatformContext{
		ManifestsBasePath: base,
		Release:           common.Release{Name: cluster.OpenDataHub},
	})

	g.Expect(out.Manifests).Should(HaveLen(1))
	g.Expect(out.Manifests[0].Path).Should(Equal(filepath.Join(base, "mymodule")))

	got, err := os.ReadFile(paramsFile)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(string(got)).Should(ContainSubstring("MYIMAGE_IMAGE=quay.io/opendatahub-io/myimage@sha256:123456789"))
}

// negative case: no matching RELATED_IMAGE value, so params.env is unchanged.
func TestGetOperatorManifests_ImageParamsNoMatch(t *testing.T) {
	g := NewWithT(t)

	base := t.TempDir()
	paramsFile := writeParamsModule(g, base, "MYIMAGE_IMAGE=placeholder\n")

	t.Setenv("RELATED_IMAGE_YOUIMAGE", "")

	b := paramsModuleHandler()
	_ = b.GetOperatorManifests(&modules.PlatformContext{
		ManifestsBasePath: base,
		Release:           common.Release{Name: cluster.OpenDataHub},
	})

	got, err := os.ReadFile(paramsFile)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(string(got)).Should(ContainSubstring("MYIMAGE_IMAGE=placeholder"))
}

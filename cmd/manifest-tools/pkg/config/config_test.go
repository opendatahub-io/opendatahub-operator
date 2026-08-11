package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
)

const testConfig = `
components:
  datasciencepipelines:
    odh:
      repo: opendatahub-io/data-science-pipelines-operator
      ref: main@ed98cd55e9d094d5928dc3723e491bf04252b1ab
      sourcePath: config
    rhoai:
      repo: red-hat-data-services/data-science-pipelines-operator
      ref: rhoai-3.5@324aa96d3bad5891701b660e6c47cf69fd8207c8
      sourcePath: config
  ray:
    odh:
      repo: opendatahub-io/kuberay
      ref: dev@ad425f7febc4039f2378747f2a0ea5dcf5a2263f
      sourcePath: ray-operator/config
ccmCharts:
  cert-manager-operator:
    odh:
      repo: opendatahub-io/odh-gitops
      ref: main@fb256df8af631e4d882d15f6c9c8f194a2fbfab8
      sourcePath: charts/dependencies/cert-manager-operator
componentCharts:
  dashboard-operator:
    odh:
      repo: opendatahub-io/odh-dashboard
      ref: main@3ee4d0bf68f629ff2d6fea0942372df356729a59
      sourcePath: dashboard-operator/charts/dashboard
imageOverrides:
  RELATED_IMAGE_ODH_DSP_IMAGE:
    component: datasciencepipelines
    paramsEnvKey: IMAGES_DSPO
    odh:
      base: quay.io/opendatahub/data-science-pipelines-operator
      digest: "sha256:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc"
    rhoai:
      shaFrom: odh
      base: quay.io/opendatahub/data-science-pipelines-operator
      digest: "sha256:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc"
  RELATED_IMAGE_ODH_RAY_IMAGE:
    component: ray
    paramsEnvKey: odh-kuberay-operator-controller-image
    tagTemplate: "{SHA}"
    odh:
      base: quay.io/opendatahub/kuberay-operator
      digest: ""
      pinned: true
    rhoai:
      base: quay.io/opendatahub/kuberay-operator
      digest: ""
`

func writeTestConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "manifests-config.yaml")
	if err := os.WriteFile(path, []byte(testConfig), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	path := writeTestConfig(t)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Components) != 2 {
		t.Errorf("expected 2 components, got %d", len(cfg.Components))
	}
	if len(cfg.CCMCharts) != 1 {
		t.Errorf("expected 1 ccmChart, got %d", len(cfg.CCMCharts))
	}
	if len(cfg.ComponentCharts) != 1 {
		t.Errorf("expected 1 componentChart, got %d", len(cfg.ComponentCharts))
	}
	if len(cfg.ImageOverrides) != 2 {
		t.Errorf("expected 2 imageOverrides, got %d", len(cfg.ImageOverrides))
	}
}

func TestFindComponent(t *testing.T) {
	path := writeTestConfig(t)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	comp := cfg.FindComponent("datasciencepipelines")
	if comp == nil {
		t.Fatal("expected to find datasciencepipelines")
	}
	if comp.ODH.Repo != "opendatahub-io/data-science-pipelines-operator" {
		t.Errorf("unexpected repo: %s", comp.ODH.Repo)
	}

	comp = cfg.FindComponent("cert-manager-operator")
	if comp == nil {
		t.Fatal("expected to find cert-manager-operator in ccmCharts")
	}

	comp = cfg.FindComponent("dashboard-operator")
	if comp == nil {
		t.Fatal("expected to find dashboard-operator in componentCharts")
	}

	comp = cfg.FindComponent("nonexistent")
	if comp != nil {
		t.Error("expected nil for nonexistent component")
	}
}

func TestExtractSHA(t *testing.T) {
	tests := []struct {
		ref      string
		expected string
	}{
		{"main@ed98cd55e9d094d5928dc3723e491bf04252b1ab", "ed98cd55e9d094d5928dc3723e491bf04252b1ab"},
		{"release-v0.17@01e23b5", "01e23b5"},
		{"main", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := config.ExtractSHA(tt.ref)
		if got != tt.expected {
			t.Errorf("ExtractSHA(%q) = %q, want %q", tt.ref, got, tt.expected)
		}
	}
}

func TestExtractBranch(t *testing.T) {
	tests := []struct {
		ref      string
		expected string
	}{
		{"main@ed98cd55", "main"},
		{"release-v0.17@01e23b5", "release-v0.17"},
		{"main", "main"},
	}
	for _, tt := range tests {
		got := config.ExtractBranch(tt.ref)
		if got != tt.expected {
			t.Errorf("ExtractBranch(%q) = %q, want %q", tt.ref, got, tt.expected)
		}
	}
}

func TestPlatformImage(t *testing.T) {
	path := writeTestConfig(t)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	dsp := cfg.ImageOverrides["RELATED_IMAGE_ODH_DSP_IMAGE"]

	odh := dsp.PlatformImage("odh")
	if odh == nil {
		t.Fatal("expected ODH platform image")
	}
	if !odh.HasValidDigest() {
		t.Error("expected valid digest for ODH")
	}

	rhoai := dsp.PlatformImage("rhoai")
	if rhoai == nil {
		t.Fatal("expected RHOAI platform image")
	}
	if rhoai.SHAFrom != "odh" {
		t.Errorf("expected shaFrom=odh, got %s", rhoai.SHAFrom)
	}
}

func TestPinnedImage(t *testing.T) {
	path := writeTestConfig(t)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	ray := cfg.ImageOverrides["RELATED_IMAGE_ODH_RAY_IMAGE"]
	odh := ray.PlatformImage("odh")
	if odh == nil {
		t.Fatal("expected ODH platform image for ray")
	}
	if !odh.Pinned {
		t.Error("expected pinned=true for ray ODH")
	}
}

func TestDigestPattern(t *testing.T) {
	valid := "sha256:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc"
	if !config.DigestPattern.MatchString(valid) {
		t.Errorf("expected valid digest: %s", valid)
	}

	invalid := []string{
		"sha256:short",
		"md5:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc",
		"",
		"sha256:UPPERCASE4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a1",
	}
	for _, d := range invalid {
		if config.DigestPattern.MatchString(d) {
			t.Errorf("expected invalid digest: %s", d)
		}
	}
}

func TestLoadNode_EmptyDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := config.LoadNode(path)
	if err == nil {
		t.Fatal("expected error for empty YAML document")
	}
}

func TestLoadNode_NonMappingRoot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "list.yaml")
	if err := os.WriteFile(path, []byte("- item1\n- item2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := config.LoadNode(path)
	if err == nil {
		t.Fatal("expected error for non-mapping root node")
	}
}

func TestLoadNode_MissingFile(t *testing.T) {
	_, err := config.LoadNode("/nonexistent/path.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestNodeDocSetAndSave(t *testing.T) {
	path := writeTestConfig(t)
	doc, err := config.LoadNode(path)
	if err != nil {
		t.Fatal(err)
	}

	err = doc.SetImageOverrideField("RELATED_IMAGE_ODH_DSP_IMAGE", "odh", "digest", "sha256:newdigest1234567890abcdef1234567890abcdef1234567890abcdef12345678")
	if err != nil {
		t.Fatalf("SetImageOverrideField failed: %v", err)
	}

	outPath := filepath.Join(t.TempDir(), "output.yaml")
	if err := doc.Save(outPath); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	cfg, err := config.Load(outPath)
	if err != nil {
		t.Fatalf("Load saved file failed: %v", err)
	}

	dsp := cfg.ImageOverrides["RELATED_IMAGE_ODH_DSP_IMAGE"]
	if dsp.ODH.Digest != "sha256:newdigest1234567890abcdef1234567890abcdef1234567890abcdef12345678" {
		t.Errorf("unexpected digest after save: %s", dsp.ODH.Digest)
	}
}

package generator_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/generator"
)

const testConfig = `
imageOverrides:
  RELATED_IMAGE_ODH_DSP_IMAGE:
    component: datasciencepipelines
    paramsEnvKey: IMAGES_DSPO
    odh:
      base: quay.io/opendatahub/data-science-pipelines-operator
      digest: "sha256:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc"
    rhoai:
      base: quay.io/rhoai/data-science-pipelines-operator
      digest: "sha256:aabbccdd11223344556677889900aabbccddeeff11223344556677889900aabb"
  RELATED_IMAGE_NO_DIGEST:
    component: ray
    odh:
      base: quay.io/opendatahub/ray
  RELATED_IMAGE_INVALID_DIGEST:
    component: ray
    odh:
      base: quay.io/opendatahub/ray
      digest: "sha256:short"
  RELATED_IMAGE_NO_BASE:
    component: ray
    odh:
      digest: "sha256:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc"
components:
  datasciencepipelines:
    odh:
      repo: opendatahub-io/dsp
      ref: main@abc123
      sourcePath: config
`

func TestGenerate(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	outPath := filepath.Join(dir, "output.env")

	if err := os.WriteFile(cfgPath, []byte(testConfig), 0644); err != nil {
		t.Fatal(err)
	}

	err := generator.Generate(generator.Options{
		ConfigFile: cfgPath,
		Platform:   "odh",
		OutputFile: outPath,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "RELATED_IMAGE_ODH_DSP_IMAGE=quay.io/opendatahub/data-science-pipelines-operator@sha256:4db7f864") {
		t.Error("expected DSP override in output")
	}

	if strings.Contains(content, "RELATED_IMAGE_NO_DIGEST") {
		t.Error("should not contain entry without digest")
	}

	if strings.Contains(content, "RELATED_IMAGE_INVALID_DIGEST") {
		t.Error("should not contain entry with invalid digest")
	}

	if strings.Contains(content, "RELATED_IMAGE_NO_BASE") {
		t.Error("should not contain entry without base")
	}
}

func TestGenerateRHOAI(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	outPath := filepath.Join(dir, "output.env")

	if err := os.WriteFile(cfgPath, []byte(testConfig), 0644); err != nil {
		t.Fatal(err)
	}

	err := generator.Generate(generator.Options{
		ConfigFile: cfgPath,
		Platform:   "rhoai",
		OutputFile: outPath,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "quay.io/rhoai/data-science-pipelines-operator@sha256:aabbccdd") {
		t.Error("expected RHOAI-specific base in output")
	}
}

func TestGenerateMissingConfig(t *testing.T) {
	err := generator.Generate(generator.Options{
		ConfigFile: "/nonexistent/config.yaml",
		Platform:   "odh",
		OutputFile: "/dev/null",
	})
	if err == nil {
		t.Error("expected error for missing config")
	}
}

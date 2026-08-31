package resolver_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/resolver"
)

func TestReadParamsEnvKey(t *testing.T) {
	dir := t.TempDir()
	paramsFile := filepath.Join(dir, "params.env")
	content := `IMAGES_DSPO=quay.io/opendatahub/dsp-operator@sha256:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc
kube-rbac-proxy=registry.redhat.io/openshift4/ose-kube-rbac-proxy-rhel9:latest
EMPTY_KEY=
`
	if err := os.WriteFile(paramsFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		key     string
		want    string
		wantErr bool
	}{
		{"digest-pinned", "IMAGES_DSPO", "quay.io/opendatahub/dsp-operator@sha256:4db7f864ed11d3ea5585b56cb7d7473bf80d8a1dcfc47de343a7a182c805ecdc", false},
		{"tagged", "kube-rbac-proxy", "registry.redhat.io/openshift4/ose-kube-rbac-proxy-rhel9:latest", false},
		{"empty value", "EMPTY_KEY", "", false},
		{"missing key", "NONEXISTENT", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.ReadParamsEnvKey(paramsFile, tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadParamsEnvKey() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ReadParamsEnvKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadParamsEnvKey_FileNotFound(t *testing.T) {
	_, err := resolver.ReadParamsEnvKey("/nonexistent/params.env", "KEY")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestSplitImageRef_EdgeCases(t *testing.T) {
	tests := []struct {
		ref        string
		wantBase   string
		wantDigest string
	}{
		{"", "", ""},
		{"no-at-sign", "no-at-sign", ""},
		{"host:5000/repo@sha256:abc", "host:5000/repo", "sha256:abc"},
		{"multi@at@signs", "multi@at", "signs"},
	}

	for _, tt := range tests {
		base, digest := resolver.SplitImageRef(tt.ref)
		if base != tt.wantBase || digest != tt.wantDigest {
			t.Errorf("SplitImageRef(%q) = (%q, %q), want (%q, %q)", tt.ref, base, digest, tt.wantBase, tt.wantDigest)
		}
	}
}

func TestResolve_UnknownComponent_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "manifests-config.yaml")
	manifestsDir := filepath.Join(dir, "manifests")
	os.MkdirAll(manifestsDir, 0755)

	// Config with imageOverrides entry pointing to non-existent component
	content := `components: {}
imageOverrides:
  RELATED_IMAGE_TEST:
    component: "nonexistent-component"
    odh:
      base: "quay.io/test/image"
      tagTemplate: "v{SHA}"
`
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := resolver.Resolve(t.Context(), resolver.Options{
		ConfigFile:   configFile,
		ManifestsDir: manifestsDir,
		FetchCSVImages: func(context.Context) (map[string]resolver.CSVImage, error) {
			return map[string]resolver.CSVImage{}, nil
		},
	})
	if err == nil {
		t.Error("expected error for unknown component, got nil")
	}
}

func TestResolve_ClonedCommitImageNotFound_FallsBackToCSV(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "manifests-config.yaml")
	manifestsDir := filepath.Join(dir, "manifests")
	os.MkdirAll(manifestsDir, 0755)

	content := `components:
  test-component:
    odh:
      repo: "test-org/test-repo"
      ref: "main@abc123def456"
      sourcePath: "config"
imageOverrides:
  RELATED_IMAGE_TEST:
    component: "test-component"
    odh:
      base: "quay.io/test/image"
      tagTemplate: "v{SHA}"
`
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	fakeFetch := func(_ context.Context) (map[string]resolver.CSVImage, error) {
		return map[string]resolver.CSVImage{
			"RELATED_IMAGE_TEST": {Base: "quay.io/bundle/image", Digest: "sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		}, nil
	}

	_, err := resolver.Resolve(t.Context(), resolver.Options{
		ConfigFile:     configFile,
		ManifestsDir:   manifestsDir,
		FetchCSVImages: fakeFetch,
	})
	if err != nil {
		t.Errorf("expected CSV fallback to succeed when SHA image not found, got error: %v", err)
	}
}

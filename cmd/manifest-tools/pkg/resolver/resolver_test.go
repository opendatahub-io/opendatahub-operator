package resolver_test

import (
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

func TestFindParamsEnvKey(t *testing.T) {
	dir := t.TempDir()

	// Create nested structure: component/base/params.env
	baseDir := filepath.Join(dir, "base")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "params.env"), []byte("MY_KEY=my-value\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create another nested: component/overlays/odh/params.env
	overlayDir := filepath.Join(dir, "overlays", "odh")
	if err := os.MkdirAll(overlayDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlayDir, "params.env"), []byte("OVERLAY_KEY=overlay-value\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("finds key in subdirectory", func(t *testing.T) {
		got, err := resolver.FindParamsEnvKey(dir, "MY_KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "my-value" {
			t.Errorf("got %q, want %q", got, "my-value")
		}
	})

	t.Run("finds key in deeper subdirectory", func(t *testing.T) {
		got, err := resolver.FindParamsEnvKey(dir, "OVERLAY_KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "overlay-value" {
			t.Errorf("got %q, want %q", got, "overlay-value")
		}
	})

	t.Run("returns error for missing key", func(t *testing.T) {
		_, err := resolver.FindParamsEnvKey(dir, "NONEXISTENT")
		if err == nil {
			t.Error("expected error for missing key")
		}
	})

	t.Run("returns error for missing directory", func(t *testing.T) {
		_, err := resolver.FindParamsEnvKey("/nonexistent/dir", "KEY")
		if err == nil {
			t.Error("expected error for missing directory")
		}
	})
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


package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
)

func TestLookupParamsEnvKey(t *testing.T) {
	dir := t.TempDir()

	baseDir := filepath.Join(dir, "base")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "params.env"), []byte("MY_KEY=my-value\nEMPTY_KEY=\n"), 0644); err != nil {
		t.Fatal(err)
	}

	overlayDir := filepath.Join(dir, "overlays", "odh")
	if err := os.MkdirAll(overlayDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlayDir, "params.env"), []byte("OVERLAY_KEY=overlay-value\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		dir       string
		key       string
		want      string
		wantFound bool
		wantErr   bool
	}{
		{name: "subdirectory", key: "MY_KEY", want: "my-value", wantFound: true},
		{name: "deeper subdirectory", key: "OVERLAY_KEY", want: "overlay-value", wantFound: true},
		{name: "empty value", key: "EMPTY_KEY", wantFound: true},
		{name: "missing key", key: "NONEXISTENT"},
		{name: "missing directory", dir: "/nonexistent/dir", key: "KEY", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookupDir := tt.dir
			if lookupDir == "" {
				lookupDir = dir
			}
			got, found, err := lookupParamsEnvKey(lookupDir, tt.key)
			if (err != nil) != tt.wantErr {
				t.Fatalf("lookupParamsEnvKey() error = %v, wantErr %v", err, tt.wantErr)
			}
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParamsEnvKeyLookup(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "component-with-params", "base")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	const present = "quay.io/opendatahub/test-operator@sha256:abc"
	if err := os.WriteFile(filepath.Join(dir, "params.env"), []byte("test-image-key="+present+"\nempty-key=\n"), 0644); err != nil {
		t.Fatal(err)
	}

	noParamsDir := filepath.Join(root, "component-without-params", "default")
	if err := os.MkdirAll(noParamsDir, 0755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		manifestsDir string
		component    string
		paramsEnvKey string
		want         string
		wantErr      bool
	}{
		{
			name:         "present",
			manifestsDir: root,
			component:    "component-with-params",
			paramsEnvKey: "test-image-key",
			want:         present,
		},
		{
			name:         "empty value",
			manifestsDir: root,
			component:    "component-with-params",
			paramsEnvKey: "empty-key",
		},
		{
			name:         "missing key in params.env",
			manifestsDir: root,
			component:    "component-with-params",
			paramsEnvKey: "nonexistent-key",
			wantErr:      true,
		},
		{
			name:         "component directory has no params.env file",
			manifestsDir: root,
			component:    "component-without-params",
			paramsEnvKey: "SOME_IMAGE_KEY",
			wantErr:      true,
		},
		{
			name:         "missing component directory",
			manifestsDir: root,
			component:    "nonexistent-component",
			paramsEnvKey: "some-key",
			wantErr:      true,
		},
		{
			name:         "empty ManifestsDir",
			manifestsDir: "",
			component:    "component-with-params",
			paramsEnvKey: "test-image-key",
			wantErr:      true,
		},
		{
			name:         "empty paramsEnvKey skips lookup",
			manifestsDir: root,
			component:    "component-with-params",
			paramsEnvKey: "",
			want:         "",
			wantErr:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := paramsEnvKeyLookup(Options{ManifestsDir: tt.manifestsDir}, config.ImageOverride{
				Component:    tt.component,
				ParamsEnvKey: tt.paramsEnvKey,
			}, "RELATED_IMAGE_TEST_IMAGE")
			if (err != nil) != tt.wantErr {
				t.Fatalf("paramsEnvKeyLookup() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParamsEnvKeyLookup_MissingComponentDir(t *testing.T) {
	emptyDir := t.TempDir()
	got, err := paramsEnvKeyLookup(Options{ManifestsDir: emptyDir}, config.ImageOverride{
		Component:    "ray",
		ParamsEnvKey: "some-key",
	}, "RELATED_IMAGE_ODH_RAY_IMAGE")
	if err != nil {
		t.Fatalf("expected no error for missing component directory, got: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty value, got %q", got)
	}
}

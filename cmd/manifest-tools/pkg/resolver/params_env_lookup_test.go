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
	dir := filepath.Join(root, "ray", "base")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	const present = "quay.io/opendatahub/kuberay-operator@sha256:abc"
	if err := os.WriteFile(filepath.Join(dir, "params.env"), []byte("odh-kuberay-operator-controller-image="+present+"\nempty-key=\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		paramsEnvKey string
		want         string
	}{
		{name: "present", paramsEnvKey: "odh-kuberay-operator-controller-image", want: present},
		{name: "empty value", paramsEnvKey: "empty-key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := paramsEnvKeyLookup(Options{ManifestsDir: root}, config.ImageOverride{
				Component:    "ray",
				ParamsEnvKey: tt.paramsEnvKey,
			}, "RELATED_IMAGE_ODH_RAY_IMAGE")
			if err != nil {
				t.Fatal(err)
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

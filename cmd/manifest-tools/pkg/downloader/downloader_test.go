package downloader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
)

func TestNormalizePlatform(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "odh"},
		{"OpenDataHub", "odh"},
		{"odh", "odh"},
		{"rhoai", "rhoai"},
		{"anything-else", "rhoai"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizePlatform(tt.input)
			if got != tt.expected {
				t.Errorf("normalizePlatform(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCollectEntries(t *testing.T) {
	components := map[string]config.Component{
		"comp-a": {
			ODH:   &config.PlatformRepo{Repo: "org/repo-a", Ref: "main@abc1234", SourcePath: "config"},
			RHOAI: &config.PlatformRepo{Repo: "org/repo-a-rhoai", Ref: "rhoai-3.5@def5678", SourcePath: "config"},
		},
		"comp-b": {
			ODH: &config.PlatformRepo{Repo: "org/repo-b", Ref: "dev@1234567", SourcePath: "src"},
		},
		"comp-c": {
			RHOAI: &config.PlatformRepo{Repo: "org/repo-c", Ref: "main@aaa1111", SourcePath: "config"},
		},
		"comp-empty": {},
	}

	odh := collectEntries(components, "odh")
	if len(odh) != 2 {
		t.Fatalf("expected 2 ODH entries, got %d", len(odh))
	}

	rhoai := collectEntries(components, "rhoai")
	if len(rhoai) != 2 {
		t.Fatalf("expected 2 RHOAI entries, got %d", len(rhoai))
	}
}

func TestMergeCharts_DuplicateKey(t *testing.T) {
	ccm := map[string]config.Component{
		"shared": {ODH: &config.PlatformRepo{Repo: "org/a", Ref: "main", SourcePath: "chart"}},
	}
	component := map[string]config.Component{
		"shared": {ODH: &config.PlatformRepo{Repo: "org/b", Ref: "main", SourcePath: "chart"}},
	}

	_, err := mergeCharts(ccm, component, "odh")
	if err == nil {
		t.Fatal("expected duplicate key error")
	}
}

func TestMergeCharts_NoDuplicate(t *testing.T) {
	ccm := map[string]config.Component{
		"ccm-chart": {ODH: &config.PlatformRepo{Repo: "org/a", Ref: "main", SourcePath: "chart"}},
	}
	component := map[string]config.Component{
		"comp-chart": {ODH: &config.PlatformRepo{Repo: "org/b", Ref: "main", SourcePath: "chart"}},
	}

	entries, err := mergeCharts(ccm, component, "odh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 merged entries, got %d", len(entries))
	}
}

func TestApplyOverrides(t *testing.T) {
	entries := []componentEntry{
		{Key: "dashboard", Repo: config.PlatformRepo{Repo: "org/dash", Ref: "main@abc", SourcePath: "config"}},
		{Key: "ray", Repo: config.PlatformRepo{Repo: "org/ray", Ref: "dev@def", SourcePath: "config"}},
	}

	overrides := map[string]string{
		"dashboard": "my-org:my-repo:feature-branch@1234567:my/path",
	}

	if err := applyOverrides(entries, overrides); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entries[0].Repo.Repo != "my-org/my-repo" {
		t.Errorf("expected repo my-org/my-repo, got %s", entries[0].Repo.Repo)
	}
	if entries[0].Repo.Ref != "feature-branch@1234567" {
		t.Errorf("expected ref feature-branch@1234567, got %s", entries[0].Repo.Ref)
	}
	if entries[0].Repo.SourcePath != "my/path" {
		t.Errorf("expected sourcePath my/path, got %s", entries[0].Repo.SourcePath)
	}
}

func TestApplyOverrides_UnknownKey(t *testing.T) {
	entries := []componentEntry{
		{Key: "dashboard", Repo: config.PlatformRepo{Repo: "org/dash", Ref: "main", SourcePath: "config"}},
	}

	overrides := map[string]string{
		"nonexistent": "org:repo:ref:path",
	}

	err := applyOverrides(entries, overrides)
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestApplyOverrides_InvalidFormat(t *testing.T) {
	entries := []componentEntry{
		{Key: "dashboard", Repo: config.PlatformRepo{Repo: "org/dash", Ref: "main", SourcePath: "config"}},
	}

	overrides := map[string]string{
		"dashboard": "invalid-format",
	}

	err := applyOverrides(entries, overrides)
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestChartsOnlyConfigDoesNotError(t *testing.T) {
	manifests := collectEntries(nil, "odh")
	charts, err := mergeCharts(
		map[string]config.Component{
			"cert-manager": {ODH: &config.PlatformRepo{Repo: "org/repo", Ref: "main", SourcePath: "charts/cert-manager"}},
		},
		nil,
		"odh",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(manifests) != 0 {
		t.Fatalf("expected 0 manifests, got %d", len(manifests))
	}
	if len(charts) != 1 {
		t.Fatalf("expected 1 chart, got %d", len(charts))
	}
}

func TestEmptyConfigErrors(t *testing.T) {
	manifests := collectEntries(nil, "odh")
	charts, err := mergeCharts(nil, nil, "odh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(manifests) != 0 || len(charts) != 0 {
		t.Fatalf("expected both empty, got manifests=%d charts=%d", len(manifests), len(charts))
	}
}

func TestCopyDir(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "output")

	// Create source structure
	subDir := filepath.Join(srcDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "file2.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyDir(srcDir, dstDir); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dstDir, "file1.txt"))
	if err != nil {
		t.Fatalf("reading file1.txt: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", string(data))
	}

	data, err = os.ReadFile(filepath.Join(dstDir, "sub", "file2.txt"))
	if err != nil {
		t.Fatalf("reading sub/file2.txt: %v", err)
	}
	if string(data) != "world" {
		t.Errorf("expected 'world', got %q", string(data))
	}
}

func TestSymlinkPlatformManifests(t *testing.T) {
	tmpDir := t.TempDir()
	manifestsDir := filepath.Join(tmpDir, "opt", "manifests")
	if err := os.MkdirAll(manifestsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	testManifests := map[string]string{
		"osd-configs":      "config/osd-configs",
		"hardwareprofiles": "config/hardwareprofiles",
	}

	for _, src := range testManifests {
		if err := os.MkdirAll(filepath.Join(tmpDir, src), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := symlinkPlatformManifestsFromBase(manifestsDir, testManifests, tmpDir); err != nil {
		t.Fatalf("symlinkPlatformManifests: %v", err)
	}

	for key := range testManifests {
		linkPath := filepath.Join(manifestsDir, key)
		info, err := os.Lstat(linkPath)
		if err != nil {
			t.Errorf("expected symlink for %q: %v", key, err)
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("expected %q to be a symlink", key)
		}
	}
}

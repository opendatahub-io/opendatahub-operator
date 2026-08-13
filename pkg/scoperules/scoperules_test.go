package scoperules_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/pkg/scoperules"
)

const testRules = `
framework_dirs: [registry]
manifest_files: [manifests-config.yaml]

patterns:
  components:
    - "^internal/controller/components/([^/]+)/"
    - "^internal/controller/modules/([^/]+)/"
    - "^tests/e2e/([a-z][a-z0-9_]+)_test\\.go$"
  services:
    - "^internal/controller/services/([^/]+)/"

ignored:
  patterns: ["^docs/", "\\.md$"]
  names: [cert-manager-operator]

components:
  kserve:
    deps: [trustyai]
    aliases: [kserve-module-operator]
  dashboard: {}

services:
  auth: {}
  setupcontroller:
    covered: false
    aliases: [setup]
`

func writeTestRules(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "e2e-scope-rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(testRules), 0o600))
	return path
}

func TestLoad(t *testing.T) {
	rules, err := scoperules.Load(writeTestRules(t))
	require.NoError(t, err)

	assert.Equal(t, []string{"trustyai"}, rules.Components["kserve"].Deps)
	assert.Equal(t, []string{"kserve-module-operator"}, rules.Components["kserve"].Aliases)
	assert.NotNil(t, rules.Services["setupcontroller"].Covered)
	assert.False(t, *rules.Services["setupcontroller"].Covered)
	assert.Nil(t, rules.Services["auth"].Covered, "absent covered field must be nil, not defaulted to false")
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := scoperules.Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	assert.Error(t, err)
}

func TestLoad_RejectsUnknownField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "e2e-scope-rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
components:
  kserve:
    depss: [trustyai]
`), 0o600))

	_, err := scoperules.Load(path)
	assert.Error(t, err, "a typo'd field name (depss instead of deps) must fail to load, not silently leave Deps at its zero value")
}

func TestKnownNames(t *testing.T) {
	rules, err := scoperules.Load(writeTestRules(t))
	require.NoError(t, err)

	known := rules.KnownNames()

	for _, name := range []string{"kserve", "kserve-module-operator", "dashboard", "auth", "setupcontroller", "setup", "cert-manager-operator"} {
		assert.True(t, known[name], "expected %q to be known", name)
	}
	assert.False(t, known["not-a-real-name"])
}

func TestEntry_IsCovered(t *testing.T) {
	rules, err := scoperules.Load(writeTestRules(t))
	require.NoError(t, err)

	assert.True(t, rules.Services["auth"].IsCovered(), "absent covered field must default to true")
	assert.False(t, rules.Services["setupcontroller"].IsCovered())
}

func TestDepsTargets(t *testing.T) {
	rules, err := scoperules.Load(writeTestRules(t))
	require.NoError(t, err)

	assert.Equal(t, map[string]bool{"trustyai": true}, scoperules.DepsTargets(rules.Components))
	assert.Empty(t, scoperules.DepsTargets(rules.Services))
}

func TestCompilePatterns_Classify(t *testing.T) {
	rules, err := scoperules.Load(writeTestRules(t))
	require.NoError(t, err)

	patterns, err := rules.CompilePatterns()
	require.NoError(t, err)

	tests := []struct {
		path          string
		wantName      string
		wantIsService bool
		wantOK        bool
	}{
		{"internal/controller/components/kserve/handler.go", "kserve", false, true},
		{"internal/controller/modules/dashboard/handler.go", "dashboard", false, true},
		{"internal/controller/services/auth/handler.go", "auth", true, true},
		{"pkg/controller/actions/foo.go", "", false, false},
		{"internal/controller/components/registry/registry.go", "", false, false},
		{"tests/e2e/kserve_test.go", "kserve", false, true},
		{"tests/e2e/auth_test.go", "auth", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, ok := patterns.Classify(tt.path)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantName, got.Name)
			assert.Equal(t, tt.wantIsService, got.IsService)
		})
	}
}

func TestClassifyChangedFile(t *testing.T) {
	rules, err := scoperules.Load(writeTestRules(t))
	require.NoError(t, err)

	patterns, err := rules.CompilePatterns()
	require.NoError(t, err)

	tests := []struct {
		name     string
		path     string
		wantKind scoperules.FileKind
		wantName string
	}{
		{"component path", "internal/controller/components/kserve/handler.go", scoperules.KindComponent, "kserve"},
		{"module path (component pattern)", "internal/controller/modules/dashboard/handler.go", scoperules.KindComponent, "dashboard"},
		{"service path", "internal/controller/services/auth/handler.go", scoperules.KindService, "auth"},
		{"exact manifest file", "manifests-config.yaml", scoperules.KindManifest, ""},
		{"ignored by extension", "README.md", scoperules.KindIgnored, ""},
		{"ignored by directory", "docs/e2e-testing.md", scoperules.KindIgnored, ""},
		{"framework_dirs match falls through to shared, not ignored", "internal/controller/components/registry/registry.go", scoperules.KindShared, ""},
		{"unrecognized path", "pkg/controller/actions/foo.go", scoperules.KindShared, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := patterns.ClassifyChangedFile(tt.path)
			assert.Equal(t, tt.wantKind, got.Kind)
			assert.Equal(t, tt.wantName, got.Name)
		})
	}
}

func TestClassifyChangedFile_IgnoredCheckedBeforeManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "e2e-scope-rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
manifest_files: [manifests-config.yaml]
ignored:
  patterns: ["^manifests-config\\.yaml$"]
`), 0o600))

	rules, err := scoperules.Load(path)
	require.NoError(t, err)
	patterns, err := rules.CompilePatterns()
	require.NoError(t, err)

	got := patterns.ClassifyChangedFile("manifests-config.yaml")
	assert.Equal(t, scoperules.KindIgnored, got.Kind, "an ignored pattern must win over an exact manifest_files match")
}

func TestCompilePatterns_InvalidRegex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
patterns:
  components: ["("]
`), 0o600))

	rules, err := scoperules.Load(path)
	require.NoError(t, err)

	_, err = rules.CompilePatterns()
	assert.Error(t, err)
}

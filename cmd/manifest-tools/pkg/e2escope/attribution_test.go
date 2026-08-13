// This is an internal test (package e2escope, not e2escope_test) because
// findOwnersBySourceReference is unexported and testing it directly is the
// point: testing only through ResolveUnattributedEnvVars would hide which
// source file drove which attribution decision.
//
//nolint:testpackage // deliberate internal test, see comment above
package e2escope

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/pkg/scoperules"
)

const testScopeRules = `
framework_dirs: [registry]
manifest_files: [manifests-config.yaml]

patterns:
  components:
    - "^internal/controller/components/([^/]+)/"
    - "^internal/controller/modules/([^/]+)/"
  services:
    - "^internal/controller/services/([^/]+)/"

ignored: {}

components:
  kserve: {}
  dashboard: {}

services:
  auth: {}
`

func newTestPatterns(t *testing.T) *scoperules.CompiledPatterns {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "e2e-scope-rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(testScopeRules), 0o600))

	rules, err := scoperules.Load(path)
	require.NoError(t, err)

	patterns, err := rules.CompilePatterns()
	require.NoError(t, err)

	return patterns
}

// writeSourceFile creates repoRoot/relPath with the given content, making
// any needed parent directories.
func writeSourceFile(t *testing.T, repoRoot, relPath, content string) {
	t.Helper()

	fullPath := filepath.Join(repoRoot, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o600))
}

func classifications(names ...string) []scoperules.Classification {
	out := make([]scoperules.Classification, len(names))
	for i, n := range names {
		out[i] = scoperules.Classification{Name: n}
	}
	return out
}

func TestFindOwnersBySourceReference(t *testing.T) {
	patterns := newTestPatterns(t)

	t.Run("found in exactly one component", func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, "internal/controller/modules/kserve/handler.go",
			`package kserve
const relatedImageEnv = "RELATED_IMAGE_ODH_MLSERVER_IMAGE"`)

		owners, err := findOwnersBySourceReference(root, []string{"RELATED_IMAGE_ODH_MLSERVER_IMAGE"}, patterns)
		require.NoError(t, err)
		assert.Equal(t, classifications("kserve"), owners["RELATED_IMAGE_ODH_MLSERVER_IMAGE"])
	})

	t.Run("found in multiple files agreeing on the same owner", func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, "internal/controller/modules/kserve/handler.go",
			`package kserve
const x = "RELATED_IMAGE_ODH_MLSERVER_IMAGE"`)
		writeSourceFile(t, root, "internal/controller/modules/kserve/handler_test.go",
			`package kserve
const y = "RELATED_IMAGE_ODH_MLSERVER_IMAGE"`)

		owners, err := findOwnersBySourceReference(root, []string{"RELATED_IMAGE_ODH_MLSERVER_IMAGE"}, patterns)
		require.NoError(t, err)
		assert.Equal(t, classifications("kserve"), owners["RELATED_IMAGE_ODH_MLSERVER_IMAGE"])
	})

	t.Run("not found anywhere", func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, "internal/controller/modules/kserve/handler.go",
			`package kserve
const x = "RELATED_IMAGE_ODH_SOMETHING_ELSE_IMAGE"`)

		owners, err := findOwnersBySourceReference(root, []string{"RELATED_IMAGE_ODH_MLSERVER_IMAGE"}, patterns)
		require.NoError(t, err)
		assert.Empty(t, owners["RELATED_IMAGE_ODH_MLSERVER_IMAGE"])
	})

	t.Run("referenced by two real components: attributed to both, not dropped", func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, "internal/controller/modules/kserve/handler.go",
			`package kserve
const x = "RELATED_IMAGE_ODH_SHARED_IMAGE"`)
		writeSourceFile(t, root, "internal/controller/components/dashboard/handler.go",
			`package dashboard
const y = "RELATED_IMAGE_ODH_SHARED_IMAGE"`)

		owners, err := findOwnersBySourceReference(root, []string{"RELATED_IMAGE_ODH_SHARED_IMAGE"}, patterns)
		require.NoError(t, err)
		assert.Equal(t, classifications("dashboard", "kserve"), owners["RELATED_IMAGE_ODH_SHARED_IMAGE"],
			"real evidence of two owners means attribute to both, not discard both because there's more than one")
	})

	t.Run("referenced by a component and a service: each keeps its own kind", func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, "internal/controller/modules/kserve/handler.go",
			`package kserve
const x = "RELATED_IMAGE_ODH_SHARED_IMAGE"`)
		writeSourceFile(t, root, "internal/controller/services/auth/handler.go",
			`package auth
const y = "RELATED_IMAGE_ODH_SHARED_IMAGE"`)

		owners, err := findOwnersBySourceReference(root, []string{"RELATED_IMAGE_ODH_SHARED_IMAGE"}, patterns)
		require.NoError(t, err)
		assert.ElementsMatch(t, []scoperules.Classification{
			{Name: "kserve", IsService: false},
			{Name: "auth", IsService: true},
		}, owners["RELATED_IMAGE_ODH_SHARED_IMAGE"],
			"a service's own reference must not be misreported as a component")
	})

	t.Run("match in a file whose path no pattern captures is not attributed", func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, "internal/controller/components/registry/registry.go",
			`package registry
const x = "RELATED_IMAGE_ODH_MLSERVER_IMAGE"`)

		owners, err := findOwnersBySourceReference(root, []string{"RELATED_IMAGE_ODH_MLSERVER_IMAGE"}, patterns)
		require.NoError(t, err)
		assert.Empty(t, owners["RELATED_IMAGE_ODH_MLSERVER_IMAGE"])
	})

	t.Run("only .go files are searched", func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, "internal/controller/modules/kserve/manifests/deployment.yaml",
			`env: RELATED_IMAGE_ODH_MLSERVER_IMAGE`)

		owners, err := findOwnersBySourceReference(root, []string{"RELATED_IMAGE_ODH_MLSERVER_IMAGE"}, patterns)
		require.NoError(t, err)
		assert.Empty(t, owners["RELATED_IMAGE_ODH_MLSERVER_IMAGE"],
			"non-.go references aren't searched by this fallback — a known, documented limitation, not a silent miss of Go source")
	})

	t.Run("a reference that exists only in a _test.go file is not production evidence", func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, "internal/controller/modules/kserve/handler_test.go",
			`package kserve
const x = "RELATED_IMAGE_ODH_MLSERVER_IMAGE"`)

		owners, err := findOwnersBySourceReference(root, []string{"RELATED_IMAGE_ODH_MLSERVER_IMAGE"}, patterns)
		require.NoError(t, err)
		assert.Empty(t, owners["RELATED_IMAGE_ODH_MLSERVER_IMAGE"],
			"a fixture or comment in a test file doesn't mean the component needs this image at runtime")
	})

	t.Run("a longer name containing the target as a substring does not cause a false match", func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, "internal/controller/modules/kserve/handler.go",
			`package kserve
const x = "RELATED_IMAGE_ODH_MLSERVER_IMAGE_V2"`)

		owners, err := findOwnersBySourceReference(root, []string{"RELATED_IMAGE_ODH_MLSERVER_IMAGE"}, patterns)
		require.NoError(t, err)
		assert.Empty(t, owners["RELATED_IMAGE_ODH_MLSERVER_IMAGE"],
			"the file only references the _V2 variant, a different, longer identifier, not this exact env var")
	})

	t.Run("multiple env vars resolved in a single pass", func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, "internal/controller/modules/kserve/handler.go",
			`package kserve
const a = "RELATED_IMAGE_ODH_MLSERVER_IMAGE"
const b = "RELATED_IMAGE_ODH_KSERVE_ROUTER_IMAGE"`)
		writeSourceFile(t, root, "internal/controller/components/dashboard/handler.go",
			`package dashboard
const c = "RELATED_IMAGE_ODH_DASHBOARD_IMAGE"`)

		owners, err := findOwnersBySourceReference(root, []string{
			"RELATED_IMAGE_ODH_MLSERVER_IMAGE",
			"RELATED_IMAGE_ODH_KSERVE_ROUTER_IMAGE",
			"RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
			"RELATED_IMAGE_ODH_NOT_REFERENCED_ANYWHERE_IMAGE",
		}, patterns)
		require.NoError(t, err)

		assert.Equal(t, classifications("kserve"), owners["RELATED_IMAGE_ODH_MLSERVER_IMAGE"])
		assert.Equal(t, classifications("kserve"), owners["RELATED_IMAGE_ODH_KSERVE_ROUTER_IMAGE"])
		assert.Equal(t, classifications("dashboard"), owners["RELATED_IMAGE_ODH_DASHBOARD_IMAGE"])
		assert.Empty(t, owners["RELATED_IMAGE_ODH_NOT_REFERENCED_ANYWHERE_IMAGE"])
	})

	t.Run("no env vars is a no-op, not a repo walk over an empty pattern", func(t *testing.T) {
		owners, err := findOwnersBySourceReference(t.TempDir(), nil, patterns)
		require.NoError(t, err)
		assert.Empty(t, owners)
	})

	t.Run("a directory named build nested under real source is still searched", func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, "internal/controller/modules/kserve/build/handler.go",
			`package build
const x = "RELATED_IMAGE_ODH_MLSERVER_IMAGE"`)

		owners, err := findOwnersBySourceReference(root, []string{"RELATED_IMAGE_ODH_MLSERVER_IMAGE"}, patterns)
		require.NoError(t, err)
		assert.Equal(t, classifications("kserve"), owners["RELATED_IMAGE_ODH_MLSERVER_IMAGE"],
			"skippedSourceSearchDirs must only apply at the repo root, not to a same-named directory deeper in the tree")
	})

	t.Run("a nonexistent repoRoot is a filesystem error, not an empty result", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist")

		_, err := findOwnersBySourceReference(missing, []string{"RELATED_IMAGE_ODH_MLSERVER_IMAGE"}, patterns)
		require.Error(t, err, "an unreadable repoRoot must surface as a filesystem error, not silently produce an empty owner map")
	})
}

func TestResolveUnattributedEnvVars(t *testing.T) {
	patterns := newTestPatterns(t)

	t.Run("all resolved", func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, "internal/controller/modules/kserve/handler.go",
			`package kserve
const x = "RELATED_IMAGE_ODH_MLSERVER_IMAGE"`)

		result, err := ResolveUnattributedEnvVars(root, []string{"RELATED_IMAGE_ODH_MLSERVER_IMAGE"}, patterns)
		require.NoError(t, err)
		assert.Equal(t, []string{"kserve"}, result.Components)
		assert.Empty(t, result.Services)
	})

	t.Run("resolves to a service, not miscounted as a component", func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, "internal/controller/services/auth/handler.go",
			`package auth
const x = "RELATED_IMAGE_ODH_AUTH_PROXY_IMAGE"`)

		result, err := ResolveUnattributedEnvVars(root, []string{"RELATED_IMAGE_ODH_AUTH_PROXY_IMAGE"}, patterns)
		require.NoError(t, err)
		assert.Empty(t, result.Components)
		assert.Equal(t, []string{"auth"}, result.Services)
	})

	t.Run("no env vars resolves to an empty result, not an error", func(t *testing.T) {
		result, err := ResolveUnattributedEnvVars(t.TempDir(), nil, patterns)
		require.NoError(t, err)
		assert.Empty(t, result.Components)
		assert.Empty(t, result.Services)
	})

	t.Run("mixed resolved and unresolved fails closed -- discards even the ones that resolved", func(t *testing.T) {
		root := t.TempDir()
		writeSourceFile(t, root, "internal/controller/modules/kserve/handler.go",
			`package kserve
const x = "RELATED_IMAGE_ODH_MLSERVER_IMAGE"`)

		_, err := ResolveUnattributedEnvVars(root, []string{
			"RELATED_IMAGE_ODH_MLSERVER_IMAGE",
			"RELATED_IMAGE_ODH_NOT_REFERENCED_ANYWHERE_IMAGE",
		}, patterns)
		assert.Error(t, err,
			"one var resolving must not mask another var in the same batch failing to resolve")
	})
}

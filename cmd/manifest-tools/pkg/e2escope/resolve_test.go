// This is an internal test (package e2escope, not e2escope_test) because
// changedFiles/base/resolveManifest are all unexported test-only Options
// fields -- setting them directly is the point, matching
// attribution_test.go's own precedent in this same package.
//
//nolint:testpackage // deliberate internal test, see comment above
package e2escope

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/pkg/scoperules"
)

// testResolveRules is a synthetic fixture covering dependency expansion
// (components and services), an alias, a covered=false entry, an ignored
// name, and enough patterns to exercise the full classification cascade.
// It's independent from the real e2e-scope-rules.yaml, like
// scoperules_test.go's own fixture: these tests prove the resolution
// algorithm works, not that today's real config produces some answer.
const testResolveRules = `
framework_dirs: [registry]
manifest_files: [manifests-config.yaml]

patterns:
  components:
    - "^internal/controller/components/([^/]+)/"
    - "^internal/controller/modules/([^/]+)/"
    - "^api/components/v1alpha1/([a-z]+)_types.*\\.go$"
    - "^tests/e2e/([a-z][a-z0-9_]+)_test\\.go$"
  services:
    - "^internal/controller/services/([^/]+)/"

ignored:
  patterns:
    - "^docs/"
    - "^cmd/cloudmanager/"
  names:
    - cert-manager-operator

components:
  dashboard: {}
  kueue: {}
  mlflowoperator: {}
  trustyai: {}
  kserve-modelcache: {}
  kserve:
    deps: [trustyai, kserve-modelcache]
    aliases: [kserve-module-operator]
  workbenches:
    deps: [kueue, mlflowoperator]
  needs-covered-false-dep:
    deps: [setup]
  needs-ignored-dep:
    deps: [cert-manager-operator]
  needs-alias-dep:
    deps: [kserve-module-operator]

services:
  auth:
    deps: [gateway]
  gateway: {}
  setup:
    covered: false
  broken:
    deps: [totally-unknown-name]
`

func writeResolveRules(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "e2e-scope-rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(testResolveRules), 0o600))
	return path
}

func resolve(t *testing.T, files []string) (*Result, error) {
	t.Helper()
	return Resolve(context.Background(), Options{
		RepoRoot:     t.TempDir(),
		RulesPath:    writeResolveRules(t),
		changedFiles: files,
	})
}

func TestResolve_SingleComponent(t *testing.T) {
	result, err := resolve(t, []string{"internal/controller/components/dashboard/foo.go"})
	require.NoError(t, err)
	assert.Equal(t, []string{"dashboard"}, result.Components)
	assert.Empty(t, result.Services)
}

func TestResolve_DependencyExpansionIsAFixedPoint(t *testing.T) {
	result, err := resolve(t, []string{"internal/controller/modules/workbenches/foo.go"})
	require.NoError(t, err)
	assert.Equal(t, []string{"kueue", "mlflowoperator", "workbenches"}, result.Components)
	assert.Empty(t, result.Services)
}

func TestResolve_ServicesCanDependOnServices(t *testing.T) {
	result, err := resolve(t, []string{"internal/controller/services/auth/foo.go"})
	require.NoError(t, err)
	assert.Empty(t, result.Components)
	assert.Equal(t, []string{"auth", "gateway"}, result.Services)
}

func TestResolve_UnknownDependencyTargetForcesFullSuite(t *testing.T) {
	_, err := resolve(t, []string{"internal/controller/services/broken/foo.go"})
	assert.Error(t, err, "a deps target that isn't a known component or service must force the full suite, not silently vanish")
}

func TestResolve_DependencyOnCoveredFalseTargetIsDroppedNotSelected(t *testing.T) {
	result, err := resolve(t, []string{"internal/controller/components/needs-covered-false-dep/foo.go"})
	require.NoError(t, err)
	assert.Equal(t, []string{"needs-covered-false-dep"}, result.Components)
	assert.Empty(t, result.Services, "a deps target with covered=false must be dropped, not selected, same as when that name arrives directly")
}

func TestResolve_DependencyOnIgnoredNameIsDroppedNotForcedToFullSuite(t *testing.T) {
	result, err := resolve(t, []string{"internal/controller/components/needs-ignored-dep/foo.go"})
	require.NoError(t, err)
	assert.Equal(t, []string{"needs-ignored-dep"}, result.Components)
	assert.Empty(t, result.Services, "a deps target that's an ignored name must be silently dropped, not force the full suite")
}

func TestResolve_DependencyNamedByAliasResolvesToCanonicalAndKeepsExpanding(t *testing.T) {
	result, err := resolve(t, []string{"internal/controller/components/needs-alias-dep/foo.go"})
	require.NoError(t, err)
	assert.Equal(t, []string{"kserve", "kserve-modelcache", "needs-alias-dep", "trustyai"}, result.Components,
		"a deps target named by an alias must resolve to its canonical name, which must then keep expanding its own deps")
}

func TestResolve_ManifestAliasRedirectsThenExpandsDeps(t *testing.T) {
	// "kserve-module-operator" is an alias for "kserve" from
	// manifests-config.yaml, not something any path pattern produces.
	// This exercises alias redirection via the manifest fallback, and
	// confirms deps still expand afterward.
	result, err := Resolve(context.Background(), Options{
		RepoRoot:     t.TempDir(),
		RulesPath:    writeResolveRules(t),
		changedFiles: []string{"manifests-config.yaml"},
		base:         "deadbeef",
		resolveManifest: func(_ context.Context, _, _, _ string, _ *scoperules.CompiledPatterns) (Result, error) {
			return Result{Components: []string{"kserve-module-operator"}}, nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"kserve", "kserve-modelcache", "trustyai"}, result.Components)
	assert.Empty(t, result.Services)
}

func TestResolve_ManifestResolvedToAService(t *testing.T) {
	result, err := Resolve(context.Background(), Options{
		RepoRoot:     t.TempDir(),
		RulesPath:    writeResolveRules(t),
		changedFiles: []string{"manifests-config.yaml"},
		base:         "deadbeef",
		resolveManifest: func(_ context.Context, _, _, _ string, _ *scoperules.CompiledPatterns) (Result, error) {
			return Result{Services: []string{"gateway"}}, nil
		},
	})
	require.NoError(t, err)
	assert.Empty(t, result.Components)
	assert.Equal(t, []string{"gateway"}, result.Services)
}

func TestResolve_CoveredFalseAloneResolvesToConfirmedEmpty(t *testing.T) {
	result, err := resolve(t, []string{"internal/controller/services/setup/foo.go"})
	require.NoError(t, err, "a covered=false name being the only change is confirmed-nothing-selectable, not doubt -- it must not force the full suite")
	assert.Empty(t, result.Components)
	assert.Empty(t, result.Services)
}

func TestResolve_AllFilesIgnoredResolvesToConfirmedEmpty(t *testing.T) {
	result, err := resolve(t, []string{"docs/README.md"})
	require.NoError(t, err, "a diff touching only ignored files is confirmed-nothing-selectable, not doubt -- it must not force the full suite")
	assert.Empty(t, result.Components)
	assert.Empty(t, result.Services)
}

func TestResolve_CoveredFalseMixedWithRealComponent_ComponentSurvivesServiceDropped(t *testing.T) {
	result, err := resolve(t, []string{
		"internal/controller/services/setup/foo.go",
		"internal/controller/components/dashboard/foo.go",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"dashboard"}, result.Components)
	assert.Empty(t, result.Services)
}

func TestResolve_IgnoredPathMixedWithComponentDoesNotForceFullSuite(t *testing.T) {
	result, err := resolve(t, []string{
		"docs/README.md",
		"internal/controller/components/dashboard/foo.go",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"dashboard"}, result.Components)
}

func TestResolve_IgnoredNameFromManifestDiffIsDropped(t *testing.T) {
	result, err := Resolve(context.Background(), Options{
		RepoRoot:     t.TempDir(),
		RulesPath:    writeResolveRules(t),
		changedFiles: []string{"manifests-config.yaml"},
		base:         "deadbeef",
		resolveManifest: func(_ context.Context, _, _, _ string, _ *scoperules.CompiledPatterns) (Result, error) {
			return Result{Components: []string{"cert-manager-operator"}}, nil
		},
	})
	require.NoError(t, err, "an ignored name being the only thing that changed is confirmed-nothing-selectable, not doubt -- it must not force the full suite")
	assert.Empty(t, result.Components, "an ignored name must not resolve to a selectable scope even when it's the only thing that changed")
	assert.Empty(t, result.Services)
}

func TestResolve_GenuinelyUnrecognizedPathForcesFullSuite(t *testing.T) {
	_, err := resolve(t, []string{
		"pkg/controller/actions/foo.go",
		"internal/controller/components/dashboard/foo.go",
	})
	assert.Error(t, err)
}

func TestResolve_OwnAttributionCodeForcesFullSuiteEvenMixedWithRealComponent(t *testing.T) {
	_, err := resolve(t, []string{
		"cmd/manifest-tools/pkg/e2escope/attribution.go",
		"internal/controller/components/dashboard/foo.go",
	})
	assert.Error(t, err, "this resolver's own code must never be excluded from being treated as a risky change")
}

func TestResolve_E2ERunnerCodeForcesFullSuiteEvenMixedWithRealComponent(t *testing.T) {
	_, err := resolve(t, []string{
		"cmd/test-retry/main.go",
		"internal/controller/components/dashboard/foo.go",
	})
	assert.Error(t, err, "the actual e2e runner's own code must never be excluded from being treated as a risky change")
}

func TestResolve_CloudManagerCmdFootprintStillIgnoredOnItsOwnMerits(t *testing.T) {
	result, err := resolve(t, []string{
		"cmd/cloudmanager/main.go",
		"internal/controller/components/dashboard/foo.go",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"dashboard"}, result.Components)
}

func TestResolve_FrameworkDirIsSharedNotAFakeComponent(t *testing.T) {
	_, err := resolve(t, []string{"internal/controller/components/registry/registry.go"})
	assert.Error(t, err)
}

func TestResolve_UnregisteredCapturedNameForcesFullSuite(t *testing.T) {
	_, err := resolve(t, []string{"api/components/v1alpha1/modelsasservice_types.go"})
	assert.Error(t, err, "a name a pattern captures but that isn't registered must not silently pass through")
}

func TestResolve_E2ETestInfraFileForcesFullSuiteViaGeneralSafetyNet(t *testing.T) {
	_, err := resolve(t, []string{"tests/e2e/creation_test.go"})
	assert.Error(t, err, "e2e infra files with no dedicated exception list rely on the same unknown-name safety net as any other unregistered name")
}

func TestResolve_LogsEveryUnrecognizedFileNotJustTheFirst(t *testing.T) {
	var buf bytes.Buffer
	previous := logWriter
	logWriter = &buf
	defer func() { logWriter = previous }()

	_, err := resolve(t, []string{"pkg/foo/bar.go", "pkg/baz/qux.go"})
	require.Error(t, err)

	output := buf.String()
	assert.Contains(t, output, "pkg/foo/bar.go -> shared")
	assert.Contains(t, output, "pkg/baz/qux.go -> shared")
}

// repoTestRoot resolves the repository root regardless of the test
// binary's working directory, using this file's own location. Walks up
// looking for .git rather than hardcoding a directory depth.
func repoTestRoot(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed to resolve this test file's path")

	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "no .git found above %s", thisFile)
		dir = parent
	}
}

// TestResolve_RealConfigClassifiesServiceOnlyTestFilesAsServices uses the
// real tests/e2e/scripts/e2e-scope-rules.yaml, not a synthetic fixture:
// gateway and monitoring are only ever registered under services, but
// their e2e test files match the shared components-list test-file
// pattern. A regression here would have every such file force the full
// suite instead of narrowing to the one service it actually belongs to.
func TestResolve_RealConfigClassifiesServiceOnlyTestFilesAsServices(t *testing.T) {
	root := repoTestRoot(t)

	for _, name := range []string{"gateway", "monitoring"} {
		t.Run(name, func(t *testing.T) {
			result, err := Resolve(context.Background(), Options{
				RepoRoot:     root,
				RulesPath:    filepath.Join(root, scoperules.DefaultPath),
				changedFiles: []string{"tests/e2e/" + name + "_test.go"},
			})
			require.NoError(t, err)
			assert.Empty(t, result.Components)
			assert.Equal(t, []string{name}, result.Services)
		})
	}
}

const rulesWithDuplicateAlias = `
patterns:
  components:
    - "^internal/controller/components/([^/]+)/"

components:
  owner-a:
    aliases: [shared]
  owner-b:
    aliases: [shared]
`

func TestResolve_DuplicateAliasAcrossTwoEntriesForcesFullSuite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "e2e-scope-rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(rulesWithDuplicateAlias), 0o600))

	_, err := Resolve(context.Background(), Options{
		RepoRoot:     t.TempDir(),
		RulesPath:    path,
		changedFiles: []string{"internal/controller/components/owner-a/foo.go"},
	})
	assert.Error(t, err, "two entries declaring the same alias must not resolve to whichever wins map iteration order")
}

const rulesWithShadowingAlias = `
patterns:
  components:
    - "^internal/controller/components/([^/]+)/"

components:
  canonical-a:
    aliases: [canonical-b]
  canonical-b: {}
`

func TestResolve_AliasShadowingARealEntryForcesFullSuite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "e2e-scope-rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(rulesWithShadowingAlias), 0o600))

	_, err := Resolve(context.Background(), Options{
		RepoRoot:     t.TempDir(),
		RulesPath:    path,
		changedFiles: []string{"internal/controller/components/canonical-b/foo.go"},
	})
	assert.Error(t, err, "an alias that names another entry's own canonical name must not silently redirect it")
}

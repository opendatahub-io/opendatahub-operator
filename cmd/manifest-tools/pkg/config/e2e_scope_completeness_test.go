package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/pkg/scoperules"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
)

const manifestsConfigRelPath = "manifests-config.yaml"

const e2eScopeRulesRelPath = scoperules.DefaultPath

// repoRoot resolves the repository root regardless of the test binary's
// working directory, using this file's own location via runtime.Caller.
// Walks up looking for .git rather than hardcoding a directory depth, so
// moving this file doesn't silently break the constant.
func repoRoot(t *testing.T) string {
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

// TestManifestsConfigNamesAreKnownToE2EScopeRules checks that
// manifests-config.yaml hasn't drifted ahead of e2e-scope-rules.yaml. If a
// new components/ccmCharts/componentCharts entry, or a new imageOverrides
// Component field, is added without a matching scope-rules entry or alias,
// the resolver meets a name it doesn't recognize. It degrades safely (runs
// the full suite instead of crashing or misattributing), but this test
// catches the gap immediately instead of only losing selective scoping
// quietly on some unrelated PR.
func TestManifestsConfigNamesAreKnownToE2EScopeRules(t *testing.T) {
	root := repoRoot(t)

	manifestsCfg, err := config.Load(filepath.Join(root, manifestsConfigRelPath))
	require.NoError(t, err, "loading %s", manifestsConfigRelPath)

	scopeRules, err := scoperules.Load(filepath.Join(root, e2eScopeRulesRelPath))
	require.NoError(t, err, "loading %s", e2eScopeRulesRelPath)
	known := scopeRules.KnownNames()

	for name := range manifestsCfg.Components {
		if !known[name] {
			t.Errorf("manifests-config.yaml components.%s has no matching entry, alias, or ignored-name in %s", name, e2eScopeRulesRelPath)
		}
	}
	for name := range manifestsCfg.CCMCharts {
		if !known[name] {
			t.Errorf("manifests-config.yaml ccmCharts.%s has no matching entry, alias, or ignored-name in %s", name, e2eScopeRulesRelPath)
		}
	}
	for name := range manifestsCfg.ComponentCharts {
		if !known[name] {
			t.Errorf("manifests-config.yaml componentCharts.%s has no matching entry, alias, or ignored-name in %s", name, e2eScopeRulesRelPath)
		}
	}
	for envVar, override := range manifestsCfg.ImageOverrides {
		// Most imageOverrides entries (source: "csv") are auto-discovered
		// from a ClusterServiceVersion and carry no Component field at all
		// by design -- Component is only for tagTemplate resolution
		// against a known component's ref, not a universal field. The
		// resolver falls back to a source-reference search for these (see
		// e2escope.ResolveUnattributedEnvVars), which this static
		// completeness check can't verify -- that fallback either resolves
		// at diff time or safely forces a full-suite run.
		if override.Component == "" {
			continue
		}
		if !known[override.Component] {
			t.Errorf("manifests-config.yaml imageOverrides.%s references component %q, which has no matching entry, alias, or ignored-name in %s", envVar, override.Component, e2eScopeRulesRelPath)
		}
	}
}

// TestManifestsConfigImageOverridesAreValid validates that every entry in
// manifests-config.yaml conforms to CheckImageOverride rules (valid RELATED_IMAGE_
// prefix, known component, valid tagTemplate syntax, etc.).
func TestManifestsConfigImageOverridesAreValid(t *testing.T) {
	root := repoRoot(t)

	manifestsCfg, err := config.Load(filepath.Join(root, manifestsConfigRelPath))
	require.NoError(t, err, "loading %s", manifestsConfigRelPath)

	envNames := make([]string, 0, len(manifestsCfg.ImageOverrides))
	for envName := range manifestsCfg.ImageOverrides {
		envNames = append(envNames, envName)
	}
	sort.Strings(envNames)

	for _, envName := range envNames {
		override := manifestsCfg.ImageOverrides[envName]
		t.Run(envName, func(t *testing.T) {
			if err := manifestsCfg.CheckImageOverride(envName, override); err != nil {
				t.Errorf("manifests-config.yaml imageOverrides.%s invalid: %v", envName, err)
			}
		})
	}
}

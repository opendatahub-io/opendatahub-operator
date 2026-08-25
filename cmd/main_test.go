package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/pkg/scoperules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
)

// repoRoot resolves the repository root regardless of the test binary's
// working directory (`go test` runs with cwd set to the package directory,
// not the repo root), using this file's own location via runtime.Caller.
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

func TestAllComponentsHaveExplicitRunlevel(t *testing.T) {
	t.Parallel()

	for name := range existingComponents {
		_, ok := componentRunlevels[name]
		assert.True(t, ok, "component %q is registered but has no entry in componentRunlevels — add an explicit runlevel assignment", name)
	}
}

func TestAllModulesHaveExplicitRunlevel(t *testing.T) {
	t.Parallel()

	for name := range existingModules {
		_, ok := moduleRunlevels[name]
		assert.True(t, ok, "module %q is registered but has no entry in moduleRunlevels — add an explicit runlevel assignment", name)
	}
}

func TestComponentRunlevelsOnlyReferenceRegisteredComponents(t *testing.T) {
	t.Parallel()

	for name := range componentRunlevels {
		_, ok := existingComponents[name]
		assert.True(t, ok, "componentRunlevels has entry %q but no matching handler in existingComponents — stale entry?", name)
	}
}

func TestModuleRunlevelsOnlyReferenceRegisteredModules(t *testing.T) {
	t.Parallel()

	for name := range moduleRunlevels {
		_, ok := existingModules[name]
		assert.True(t, ok, "moduleRunlevels has entry %q but no matching handler in existingModules — stale entry?", name)
	}
}

func TestNoComponentUsesRunlevelDefault(t *testing.T) {
	t.Parallel()

	for name, rl := range componentRunlevels {
		assert.NotEqual(t, dag.RL(99), rl,
			"component %q uses Runlevel99 — assign an explicit runlevel", name)
	}
}

func TestNoModuleUsesRunlevelDefault(t *testing.T) {
	t.Parallel()

	for name, rl := range moduleRunlevels {
		assert.NotEqual(t, dag.RL(99), rl,
			"module %q uses Runlevel99 — assign an explicit runlevel", name)
	}
}

const e2eScopeRulesPath = scoperules.DefaultPath

func loadE2EScopeRules(t *testing.T) *scoperules.Rules {
	t.Helper()

	rules, err := scoperules.Load(filepath.Join(repoRoot(t), e2eScopeRulesPath))
	require.NoError(t, err, "loading %s", e2eScopeRulesPath)

	return rules
}

// TestAllComponentsAndModulesHaveScopeRulesEntry checks that
// tests/e2e/scripts/e2e-scope-rules.yaml stays in sync with the real
// component/module registry. Every registered name needs an entry, even
// an empty one, so the resolver never meets a name it can't classify.
// Component-modules live under components.<name>; a modularized service
// (handler in existingModules, e2e coverage in the Services TestGroup)
// may live under services.<name> instead.
func TestAllComponentsAndModulesHaveScopeRulesEntry(t *testing.T) {
	t.Parallel()

	rules := loadE2EScopeRules(t)

	for name := range existingComponents {
		_, ok := rules.Components[name]
		assert.True(t, ok, "component %q is registered but has no components.%s entry in %s", name, name, e2eScopeRulesPath)
	}
	for name := range existingModules {
		_, inComponents := rules.Components[name]
		_, inServices := rules.Services[name]
		assert.True(t, inComponents || inServices,
			"module %q is registered but has no components.%s or services.%s entry in %s",
			name, name, name, e2eScopeRulesPath)
	}
}

// TestAllServicesHaveScopeRulesEntry is the services-side equivalent of
// TestAllComponentsAndModulesHaveScopeRulesEntry.
func TestAllServicesHaveScopeRulesEntry(t *testing.T) {
	t.Parallel()

	rules := loadE2EScopeRules(t)

	for name := range existingServices {
		_, ok := rules.Services[name]
		assert.True(t, ok, "service %q is registered but has no services.%s entry in %s", name, name, e2eScopeRulesPath)
	}
}

// depsTargetsOfRegisteredEntries is every name in the deps list of a
// components.<name> entry that itself maps to a real cmd/main.go component
// or module. It lets a deps-only name (e.g. "kserve-modelcache", an e2e
// scenario reached only via kserve's deps, never registered on its own in
// cmd/main.go) count as legitimate without its own registration. It still
// has to be a documented dependency of a real entry; separately,
// TestScopeRulesComponentsMatchRealTestGroup in
// tests/e2e/e2e_scope_rules_registry_test.go checks it's an actual
// selectable e2e TestGroup name.
func depsTargetsOfRegisteredEntries(components map[string]scoperules.Entry) map[string]bool {
	registered := map[string]scoperules.Entry{}
	for name, entry := range components {
		_, isComponent := existingComponents[name]
		_, isModule := existingModules[name]
		if isComponent || isModule {
			registered[name] = entry
		}
	}
	return scoperules.DepsTargets(registered)
}

// TestScopeRulesComponentsReferenceRegisteredHandlers is the reverse of
// TestAllComponentsAndModulesHaveScopeRulesEntry: it catches a stale
// components.<name> entry left behind after a component/module is
// deregistered. Without this check, that entry would only surface in
// production, the first time something resolved to it, by hard-failing
// the e2e job at TestGroup.Validate().
func TestScopeRulesComponentsReferenceRegisteredHandlers(t *testing.T) {
	t.Parallel()

	rules := loadE2EScopeRules(t)
	vouchedFor := depsTargetsOfRegisteredEntries(rules.Components)

	for name := range rules.Components {
		_, isComponent := existingComponents[name]
		_, isModule := existingModules[name]
		assert.True(t, isComponent || isModule || vouchedFor[name],
			"%s has components.%s but no matching handler in existingComponents or existingModules, "+
				"and it isn't a dependency of any entry that does — stale entry?", e2eScopeRulesPath, name)
	}
}

// TestScopeRulesServicesReferenceRegisteredHandlers is the reverse of
// TestAllServicesHaveScopeRulesEntry: it catches a stale services.<name>
// entry left behind after a service is deregistered. Modularized services
// (registered in existingModules rather than existingServices) are also
// valid here.
func TestScopeRulesServicesReferenceRegisteredHandlers(t *testing.T) {
	t.Parallel()

	rules := loadE2EScopeRules(t)

	for name := range rules.Services {
		_, isService := existingServices[name]
		_, isModule := existingModules[name]
		assert.True(t, isService || isModule,
			"%s has services.%s but no matching handler in existingServices or existingModules — stale entry?",
			e2eScopeRulesPath, name)
	}
}

// TestScopeRulesPatternsCaptureRegisteredNames closes a gap the entry-
// completeness tests above don't: they only check that a config entry
// exists for each real name, not that any path pattern actually resolves
// to it. A new directory convention with no matching pattern would
// silently fall through to "shared" forever. This test compiles each
// configured pattern and checks it against each component/module/
// service's real, on-disk directory.
func TestScopeRulesPatternsCaptureRegisteredNames(t *testing.T) {
	t.Parallel()

	rules := loadE2EScopeRules(t)
	root := repoRoot(t)

	patterns, err := rules.CompilePatterns()
	require.NoError(t, err, "compiling patterns from %s", e2eScopeRulesPath)

	for name := range existingComponents {
		assertNameIsCapturable(t, root, patterns.Components, "internal/controller/components", name, rules.Components[name].Aliases)
	}
	for name := range existingModules {
		aliases := rules.Components[name].Aliases
		if len(aliases) == 0 {
			aliases = rules.Services[name].Aliases
		}
		assertNameIsCapturable(t, root, patterns.Components, "internal/controller/modules", name, aliases)
	}
	for name := range existingServices {
		assertNameIsCapturable(t, root, patterns.Services, "internal/controller/services", name, rules.Services[name].Aliases)
	}
}

// assertNameIsCapturable checks that a registered name is reachable by some
// pattern, either through its own directory or through an alias's
// directory (a separate alias-redirection step maps that back to the
// registered name; see e2e-scope-rules.yaml's aliases fields). The
// registered name and its directory name aren't always the same string --
// e.g. the "setupcontroller" service lives under
// internal/controller/services/setup/ -- so this doesn't assume they match.
//
// It matches against the directory path with a trailing slash and no
// filename, since every directory-based pattern anchors up to that slash
// and doesn't care what follows.
func assertNameIsCapturable(t *testing.T, root string, patterns []*regexp.Regexp, dirPrefix, registeredName string, aliases []string) {
	t.Helper()

	candidates := append([]string{registeredName}, aliases...)
	for _, candidate := range candidates {
		dir := filepath.Join(dirPrefix, candidate)
		if _, err := os.Stat(filepath.Join(root, dir)); err != nil {
			continue
		}
		if patternCaptures(patterns, dir+"/", candidate) {
			return
		}
		t.Errorf("no pattern in %s captures %q from %s/ (registered name %q)", e2eScopeRulesPath, candidate, dir, registeredName)
		return
	}
	t.Errorf("registered name %q (aliases: %v) has no matching directory under %s — checked %v", registeredName, aliases, dirPrefix, candidates)
}

func patternCaptures(patterns []*regexp.Regexp, path, wantCapture string) bool {
	for _, re := range patterns {
		if m := re.FindStringSubmatch(path); len(m) > 1 && m[1] == wantCapture {
			return true
		}
	}
	return false
}

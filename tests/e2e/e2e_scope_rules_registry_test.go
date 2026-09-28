package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/pkg/scoperules"
)

// e2eScopeRulesRelPath is scoperules.DefaultPath made relative to this
// package's own directory, since `go test` runs with cwd set to
// tests/e2e, not the repo root.
var e2eScopeRulesRelPath = strings.TrimPrefix(scoperules.DefaultPath, "tests/e2e/")

func loadMinimalScopeRules(t *testing.T) *scoperules.Rules {
	t.Helper()

	rules, err := scoperules.Load(e2eScopeRulesRelPath)
	require.NoError(t, err, "loading %s", e2eScopeRulesRelPath)

	return rules
}

// TestScopeRulesComponentsMatchRealTestGroup checks something
// cmd/main_test.go's completeness tests can't: every e2e-scope-rules.yaml
// components.<name> entry that isn't covered:false must be a name
// Components.Validate() actually accepts, since that's what feeds into
// E2E_TEST_COMPONENT. cmd/main.go's existingComponents and this TestGroup
// happen to agree today, but nothing ties them together -- cmd/main.go is
// package main and can't be imported here, so this is the only place both
// sides are visible together. A selectable name that isn't a real
// TestGroup value would pass every other check and only surface in
// production by hard-failing the e2e binary at TestGroup.Validate().
func TestScopeRulesComponentsMatchRealTestGroup(t *testing.T) {
	rules := loadMinimalScopeRules(t)

	realNames := map[string]bool{}
	for _, n := range Components.Names() {
		realNames[n] = true
	}

	for name, entry := range rules.Components {
		if !entry.IsCovered() {
			continue
		}
		if !realNames[name] {
			t.Errorf("e2e-scope-rules.yaml components.%s is selectable but %q is not a real name in tests/e2e's Components TestGroup -- TestGroup.Validate() would reject it", name, name)
		}
	}

	// A name reachable only via some other entry's deps -- e.g.
	// "kserve-modelcache", a dependency of kserve's own test scenario, not
	// a separately-registered component -- is already correctly
	// selectable without a dedicated entry of its own.
	deps := scoperules.DepsTargets(rules.Components)
	for _, name := range Components.Names() {
		_, hasEntry := rules.Components[name]
		if !hasEntry && !deps[name] {
			t.Errorf("tests/e2e's Components TestGroup has %q but e2e-scope-rules.yaml has no components.%s entry "+
				"and no entry depends on it -- the resolver can never select it, forcing a full-suite run for any change to it", name, name)
		}
	}
}

// TestScopeRulesServicesMatchRealTestGroup is the services-side equivalent
// of TestScopeRulesComponentsMatchRealTestGroup.
func TestScopeRulesServicesMatchRealTestGroup(t *testing.T) {
	rules := loadMinimalScopeRules(t)

	realNames := map[string]bool{}
	for _, n := range Services.Names() {
		realNames[n] = true
	}

	for name, entry := range rules.Services {
		if !entry.IsCovered() {
			continue
		}
		if !realNames[name] {
			t.Errorf("e2e-scope-rules.yaml services.%s is selectable but %q is not a real name in tests/e2e's Services TestGroup -- TestGroup.Validate() would reject it", name, name)
		}
	}

	deps := scoperules.DepsTargets(rules.Services)
	for _, name := range Services.Names() {
		_, hasEntry := rules.Services[name]
		if !hasEntry && !deps[name] {
			t.Errorf("tests/e2e's Services TestGroup has %q but e2e-scope-rules.yaml has no services.%s entry "+
				"and no entry depends on it -- the resolver can never select it, forcing a full-suite run for any change to it", name, name)
		}
	}
}

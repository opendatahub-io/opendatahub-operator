package e2e_test

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScopeRulesEnvVarFormatIsSpaceSeparated proves the format
// resolve-e2e-scope's output must match. When E2E_TEST_COMPONENT/
// E2E_TEST_SERVICE come from an env var rather than a CLI flag, viper
// reads the raw string and splits a StringSlice flag with strings.Fields,
// on whitespace, not on commas. A comma-joined value collapses into one
// malformed name, which TestGroup.Validate then rejects, hard-failing the
// e2e binary before any test runs.
//
// Named with the TestScopeRules prefix, like its two siblings in
// e2e_scope_rules_registry_test.go, so the Makefile can select every
// cluster-independent check for this feature with one prefix match instead
// of an exact, per-test name list that has to be kept in sync by hand.
//
// Uses a fresh viper instance, not the package-level one TestMain
// configures, so this runs independently of the rest of the suite's setup.
func TestScopeRulesEnvVarFormatIsSpaceSeparated(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{
			name:  "space-separated matches the real contract",
			value: "kserve trustyai dashboard",
			want:  []string{"kserve", "trustyai", "dashboard"},
		},
		{
			name:  "comma-separated collapses into one malformed name -- this is the bug, not the fix",
			value: "kserve,trustyai,dashboard",
			want:  []string{"kserve,trustyai,dashboard"},
		},
		{
			name:  "single name has no separator to get wrong",
			value: "kserve",
			want:  []string{"kserve"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("E2E_TEST_COMPONENT", tt.value)

			v := viper.New()
			v.SetEnvPrefix("E2E_TEST")
			require.NoError(t, v.BindEnv("test-component", v.GetEnvPrefix()+"_COMPONENT"))

			assert.Equal(t, tt.want, v.GetStringSlice("test-component"))
		})
	}
}

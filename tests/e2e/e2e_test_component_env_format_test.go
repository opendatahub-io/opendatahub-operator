package e2e_test

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/pflag"
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

func TestScopeRulesEnvIsSet(t *testing.T) {
	const name = "E2E_TEST_ENV_IS_SET"
	tests := []struct {
		name  string
		value string
		unset bool
		want  bool
	}{
		{name: "unset", unset: true, want: false},
		{name: "empty", value: "", want: false},
		{name: "non-empty false value", value: "false", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.unset {
				t.Setenv(name, "temporary")
				require.NoError(t, os.Unsetenv(name))
			} else {
				t.Setenv(name, tt.value)
			}

			require.Equal(t, tt.want, envIsSet(name))
		})
	}
}

func TestTestGroupParallelismEnvVars(t *testing.T) {
	originalComponentsParallel := Components.parallel
	originalServicesParallel := Services.parallel
	t.Cleanup(func() {
		Components.parallel = originalComponentsParallel
		Services.parallel = originalServicesParallel
	})

	tests := []struct {
		name           string
		componentsEnv  string
		servicesEnv    string
		wantComponents bool
		wantServices   bool
	}{
		{
			name:           "defaults preserve parallel execution",
			wantComponents: true,
			wantServices:   true,
		},
		{
			name:           "components can be enabled independently",
			componentsEnv:  "true",
			wantComponents: true,
			wantServices:   true,
		},
		{
			name:           "services can be enabled independently",
			servicesEnv:    "true",
			wantComponents: true,
			wantServices:   true,
		},
		{
			name:           "components can be disabled independently",
			componentsEnv:  "false",
			wantComponents: false,
			wantServices:   true,
		},
		{
			name:           "services can be disabled independently",
			servicesEnv:    "false",
			wantComponents: true,
			wantServices:   false,
		},
		{
			name:           "both groups can be disabled",
			componentsEnv:  "false",
			servicesEnv:    "false",
			wantComponents: false,
			wantServices:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("E2E_TEST_COMPONENTS_PARALLEL", tt.componentsEnv)
			t.Setenv("E2E_TEST_SERVICES_PARALLEL", tt.servicesEnv)

			v := viper.New()
			v.SetEnvPrefix("E2E_TEST")
			flags := pflag.NewFlagSet("test-group-parallelism", pflag.ContinueOnError)
			flags.Bool("test-components-parallel", true, "")
			flags.Bool("test-services-parallel", true, "")
			require.NoError(t, v.BindEnv("test-components-parallel", v.GetEnvPrefix()+"_COMPONENTS_PARALLEL"))
			require.NoError(t, v.BindEnv("test-services-parallel", v.GetEnvPrefix()+"_SERVICES_PARALLEL"))
			require.NoError(t, v.BindPFlags(flags))

			Components.parallel = v.GetBool("test-components-parallel")
			Services.parallel = v.GetBool("test-services-parallel")

			assert.Equal(t, tt.wantComponents, Components.parallel)
			assert.Equal(t, tt.wantServices, Services.parallel)
		})
	}
}

func TestTestGroupParallelismEnvVarsEnableFromFalseFlagDefaults(t *testing.T) {
	t.Setenv("E2E_TEST_COMPONENTS_PARALLEL", "true")
	t.Setenv("E2E_TEST_SERVICES_PARALLEL", "true")

	originalComponentsParallel := Components.parallel
	originalServicesParallel := Services.parallel
	t.Cleanup(func() {
		Components.parallel = originalComponentsParallel
		Services.parallel = originalServicesParallel
	})

	config := viper.New()
	config.SetEnvPrefix("E2E_TEST")
	flags := pflag.NewFlagSet("test-group-parallelism", pflag.ContinueOnError)
	flags.Bool("test-components-parallel", false, "")
	flags.Bool("test-services-parallel", false, "")
	require.NoError(t, config.BindEnv("test-components-parallel", config.GetEnvPrefix()+"_COMPONENTS_PARALLEL"))
	require.NoError(t, config.BindEnv("test-services-parallel", config.GetEnvPrefix()+"_SERVICES_PARALLEL"))
	require.NoError(t, config.BindPFlags(flags))

	Components.parallel = config.GetBool("test-components-parallel")
	Services.parallel = config.GetBool("test-services-parallel")

	assert.True(t, Components.parallel)
	assert.True(t, Services.parallel)
}

func TestDumpResolvedFlags(t *testing.T) {
	t.Setenv("E2E_TEST_COMPONENTS_PARALLEL", "true")

	config := viper.New()
	config.SetEnvPrefix("E2E_TEST")
	require.NoError(t, config.BindEnv("test-components-parallel", config.GetEnvPrefix()+"_COMPONENTS_PARALLEL"))

	flags := pflag.NewFlagSet("resolved-flags", pflag.ContinueOnError)
	flags.Bool("test-components-parallel", false, "")
	flags.String("api-token", "sensitive-value", "")
	require.NoError(t, config.BindPFlags(flags))

	var output strings.Builder
	require.NoError(t, dumpResolvedFlags(&output, config, flags))

	assert.Contains(t, output.String(), "--test-components-parallel=true")
	assert.Contains(t, output.String(), "--api-token=[REDACTED]")
	assert.NotContains(t, output.String(), "sensitive-value")
}

func TestTestGroupParallelScheduling(t *testing.T) {
	parallelLimit := 1
	if parallelFlag := flag.Lookup("test.parallel"); parallelFlag != nil {
		if _, err := fmt.Sscan(parallelFlag.Value.String(), &parallelLimit); err != nil {
			t.Fatalf("parse test.parallel: %v", err)
		}
	}

	tests := []struct {
		name     string
		parallel bool
		wantMax  int32
	}{
		{name: "serial", parallel: false, wantMax: 1},
		{name: "parallel", parallel: true, wantMax: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.parallel && parallelLimit < 2 {
				t.Skip("Go test parallel limit is less than two")
			}

			var active atomic.Int32
			var maxActive atomic.Int32
			started := make(chan struct{}, 2)
			release := make(chan struct{})
			go func() {
				defer close(release)
				select {
				case <-started:
					select {
					case <-started:
					case <-time.After(250 * time.Millisecond):
					}
				case <-time.After(250 * time.Millisecond):
				}
			}()

			suite := func(t *testing.T) {
				t.Helper()
				current := active.Add(1)
				for previous := maxActive.Load(); current > previous && !maxActive.CompareAndSwap(previous, current); previous = maxActive.Load() {
				}
				started <- struct{}{}
				<-release
				active.Add(-1)
			}

			group := TestGroup{
				name:     "scheduling-test",
				enabled:  true,
				parallel: tt.parallel,
				scenarios: []map[string]TestFn{{
					"first":  suite,
					"second": suite,
				}},
			}
			group.Run(t)

			assert.Equal(t, tt.wantMax, maxActive.Load())
		})
	}
}

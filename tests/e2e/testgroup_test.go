package e2e_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveEnabledTests(t *testing.T) {
	tg := TestGroup{
		name:    "test",
		enabled: true,
		scenarios: []map[string]TestFn{
			{"alpha": noop, "beta": noop, "gamma": noop},
		},
	}

	t.Run("all enabled when no flags", func(t *testing.T) {
		tg.flags = nil
		got := tg.resolveEnabledTests()
		require.Len(t, got, 3)
		require.True(t, got["alpha"])
		require.True(t, got["beta"])
		require.True(t, got["gamma"])
	})

	t.Run("explicit inclusion limits to named tests", func(t *testing.T) {
		tg.flags = arrayFlags{"alpha"}
		got := tg.resolveEnabledTests()
		require.Len(t, got, 1)
		require.True(t, got["alpha"])
	})

	t.Run("exclusion removes from full set", func(t *testing.T) {
		tg.flags = arrayFlags{"!beta"}
		got := tg.resolveEnabledTests()
		require.Len(t, got, 2)
		require.False(t, got["beta"])
		require.True(t, got["alpha"])
		require.True(t, got["gamma"])
	})

	t.Run("inclusion and exclusion combined", func(t *testing.T) {
		tg.flags = arrayFlags{"alpha", "beta", "!beta"}
		got := tg.resolveEnabledTests()
		require.Len(t, got, 1)
		require.True(t, got["alpha"])
	})
}

func TestRunSingle(t *testing.T) {
	callLog := &callLog{}
	tg := TestGroup{
		name:    "services",
		enabled: true,
		scenarios: []map[string]TestFn{
			{
				"monitoring": callLog.recorder("monitoring"),
				"auth":       callLog.recorder("auth"),
				"gateway":    callLog.recorder("gateway"),
			},
		},
	}

	t.Run("runs only the named test", func(t *testing.T) {
		callLog.reset()
		fn := tg.RunSingle("monitoring")
		t.Run("monitoring", fn)
		require.Equal(t, []string{"monitoring"}, callLog.calls())
	})

	t.Run("skips when group is disabled", func(t *testing.T) {
		callLog.reset()
		disabled := tg
		disabled.enabled = false
		fn := disabled.RunSingle("monitoring")
		t.Run("inner", fn)
		require.Empty(t, callLog.calls())
	})

	t.Run("skips when test not found", func(t *testing.T) {
		callLog.reset()
		fn := tg.RunSingle("nonexistent")
		t.Run("inner", fn)
		require.Empty(t, callLog.calls())
	})

	t.Run("respects flags disabling the test", func(t *testing.T) {
		callLog.reset()
		flagged := tg
		flagged.flags = arrayFlags{"!monitoring"}
		fn := flagged.RunSingle("monitoring")
		t.Run("inner", fn)
		require.Empty(t, callLog.calls())
	})
}

func TestRunExcluding(t *testing.T) {
	callLog := &callLog{}
	tg := TestGroup{
		name:    "services",
		enabled: true,
		scenarios: []map[string]TestFn{
			{
				"monitoring": callLog.recorder("monitoring"),
				"auth":       callLog.recorder("auth"),
				"gateway":    callLog.recorder("gateway"),
			},
		},
	}

	t.Run("runs all except excluded", func(t *testing.T) {
		callLog.reset()
		fn := tg.RunExcluding("monitoring")
		t.Run("services", fn)
		got := callLog.calls()
		require.NotContains(t, got, "monitoring")
		require.Contains(t, got, "auth")
		require.Contains(t, got, "gateway")
	})

	t.Run("skips when group is disabled", func(t *testing.T) {
		callLog.reset()
		disabled := tg
		disabled.enabled = false
		fn := disabled.RunExcluding("monitoring")
		t.Run("inner", fn)
		require.Empty(t, callLog.calls())
	})

	t.Run("skips entirely when excluding only remaining test", func(t *testing.T) {
		callLog.reset()
		single := TestGroup{
			name:    "single",
			enabled: true,
			scenarios: []map[string]TestFn{
				{"only": callLog.recorder("only")},
			},
		}
		fn := single.RunExcluding("only")
		t.Run("inner", fn)
		require.Empty(t, callLog.calls())
	})
}

func TestIsTestEnabled(t *testing.T) {
	tg := TestGroup{
		name:    "test",
		enabled: true,
		scenarios: []map[string]TestFn{
			{"alpha": noop, "beta": noop},
		},
	}

	t.Run("enabled by default", func(t *testing.T) {
		tg.flags = nil
		require.True(t, tg.isTestEnabled("alpha"))
		require.True(t, tg.isTestEnabled("beta"))
	})

	t.Run("disabled by exclusion flag", func(t *testing.T) {
		tg.flags = arrayFlags{"!alpha"}
		require.False(t, tg.isTestEnabled("alpha"))
		require.True(t, tg.isTestEnabled("beta"))
	})
}

// --- helpers ---

func noop(t *testing.T) { t.Helper() }

type callLog struct {
	mu      sync.Mutex
	entries []string
}

func (c *callLog) recorder(name string) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()
		c.mu.Lock()
		defer c.mu.Unlock()
		c.entries = append(c.entries, name)
	}
}

func (c *callLog) calls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.entries))
	copy(out, c.entries)
	return out
}

func (c *callLog) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = nil
}

package quarantine_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/quarantine"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	t.Run("missing file returns empty config", func(t *testing.T) {
		t.Parallel()
		cfg, err := quarantine.LoadConfig(filepath.Join(t.TempDir(), "missing.json"))
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, 1, cfg.Version)
		assert.Empty(t, cfg.Tests)
	})

	t.Run("valid file", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "q.json")
		data := `{
			"version": 1,
			"tests": [
				{"name": "TestOdhOperator/services/monitoring", "reason": "flaky", "quarantined_at": "2026-01-01T00:00:00Z"}
			]
		}`
		require.NoError(t, os.WriteFile(path, []byte(data), 0600))

		cfg, err := quarantine.LoadConfig(path)
		require.NoError(t, err)
		require.Len(t, cfg.Tests, 1)
		assert.Equal(t, "TestOdhOperator/services/monitoring", cfg.Tests[0].Name)
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "bad.json")
		require.NoError(t, os.WriteFile(path, []byte("{invalid"), 0600))

		_, err := quarantine.LoadConfig(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse")
	})
}

func TestSaveConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "out.json")
	cfg := &quarantine.Config{
		Version: 1,
		Tests: []quarantine.Entry{
			{Name: "TestA", Reason: "flaky", QuarantinedAt: "2026-01-01T00:00:00Z"},
		},
	}

	require.NoError(t, quarantine.SaveConfig(path, cfg))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var loaded quarantine.Config
	require.NoError(t, json.Unmarshal(data, &loaded))
	assert.Equal(t, 1, loaded.Version)
	assert.Len(t, loaded.Tests, 1)
	assert.NotEmpty(t, loaded.Updated)
}

func TestActiveEntries(t *testing.T) {
	t.Parallel()

	past := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	future := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)

	cfg := &quarantine.Config{
		Tests: []quarantine.Entry{
			{Name: "expired", ReEnableAfter: past},
			{Name: "still-active", ReEnableAfter: future},
			{Name: "no-expiry"},
		},
	}

	active := cfg.ActiveEntries()
	require.Len(t, active, 2)

	names := make([]string, 0, len(active))
	for _, e := range active {
		names = append(names, e.Name)
	}
	assert.Contains(t, names, "still-active")
	assert.Contains(t, names, "no-expiry")
	assert.NotContains(t, names, "expired")
}

func TestIsQuarantined(t *testing.T) {
	t.Parallel()

	cfg := &quarantine.Config{
		Tests: []quarantine.Entry{
			{Name: "TestOdhOperator/services/monitoring", Reason: "flaky"},
		},
	}

	t.Run("exact match", func(t *testing.T) {
		t.Parallel()
		q, entry := cfg.IsQuarantined("TestOdhOperator/services/monitoring")
		assert.True(t, q)
		assert.Equal(t, "flaky", entry.Reason)
	})

	t.Run("child match", func(t *testing.T) {
		t.Parallel()
		q, _ := cfg.IsQuarantined("TestOdhOperator/services/monitoring/subtest")
		assert.True(t, q)
	})

	t.Run("no match", func(t *testing.T) {
		t.Parallel()
		q, _ := cfg.IsQuarantined("TestOdhOperator/services/auth")
		assert.False(t, q)
	})
}

func TestBuildSkipRegex(t *testing.T) {
	t.Parallel()

	t.Run("no entries", func(t *testing.T) {
		t.Parallel()
		cfg := &quarantine.Config{}
		assert.Empty(t, cfg.BuildSkipRegex())
	})

	t.Run("single entry", func(t *testing.T) {
		t.Parallel()
		cfg := &quarantine.Config{
			Tests: []quarantine.Entry{
				{Name: "TestOdhOperator/services/monitoring"},
			},
		}
		regex := cfg.BuildSkipRegex()
		assert.Equal(t, "^TestOdhOperator$/^services$/^monitoring$", regex)
	})

	t.Run("multiple entries sorted", func(t *testing.T) {
		t.Parallel()
		cfg := &quarantine.Config{
			Tests: []quarantine.Entry{
				{Name: "TestB"},
				{Name: "TestA"},
			},
		}
		regex := cfg.BuildSkipRegex()
		assert.Equal(t, "^TestA$|^TestB$", regex)
	})
}

func TestAddOrUpdate(t *testing.T) {
	t.Parallel()

	t.Run("update existing", func(t *testing.T) {
		t.Parallel()
		cfg := &quarantine.Config{
			Tests: []quarantine.Entry{
				{Name: "TestA", Reason: "old reason"},
			},
		}
		cfg.AddOrUpdate(quarantine.Entry{Name: "TestA", Reason: "new reason"})
		require.Len(t, cfg.Tests, 1)
		assert.Equal(t, "new reason", cfg.Tests[0].Reason)
	})

	t.Run("add new", func(t *testing.T) {
		t.Parallel()
		cfg := &quarantine.Config{
			Tests: []quarantine.Entry{
				{Name: "TestA", Reason: "existing"},
			},
		}
		cfg.AddOrUpdate(quarantine.Entry{Name: "TestB", Reason: "new"})
		require.Len(t, cfg.Tests, 2)
	})
}

func TestRemove(t *testing.T) {
	t.Parallel()

	cfg := &quarantine.Config{
		Tests: []quarantine.Entry{
			{Name: "TestA"},
			{Name: "TestB"},
		},
	}

	assert.True(t, cfg.Remove("TestA"))
	assert.Len(t, cfg.Tests, 1)
	assert.Equal(t, "TestB", cfg.Tests[0].Name)
	assert.False(t, cfg.Remove("TestC"))
}

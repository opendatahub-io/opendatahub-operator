package quarantine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const configFilePerms = 0600

// Config holds the quarantine configuration loaded from a JSON file.
type Config struct {
	Version int              `json:"version"`
	Updated string           `json:"updated"`
	Tests   map[string]Entry `json:"tests"`
}

// Entry represents a single quarantined test.
type Entry struct {
	Name          string  `json:"name"`
	Reason        string  `json:"reason"`
	Jira          string  `json:"jira,omitempty"`
	QuarantinedAt string  `json:"quarantined_at"`
	ReEnableAfter string  `json:"re_enable_after,omitempty"`
	FlakeRate     float64 `json:"flake_rate,omitempty"`
	TotalRuns     int     `json:"total_runs,omitempty"`
	FailedRuns    int     `json:"failed_runs,omitempty"`
	WindowDays    int     `json:"window_days,omitempty"`
}

// LoadConfig reads a quarantine config from the given path.
// Returns an empty config (not an error) if the file does not exist,
// so callers can treat a missing file as "no quarantined tests".
// Accepts both the current map format and the legacy array format for tests.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Version: 1, Tests: make(map[string]Entry)}, nil
		}
		return nil, fmt.Errorf("failed to read quarantine config %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Try legacy array format: {"tests": [{...}, ...]}
		var legacy struct {
			Version int     `json:"version"`
			Updated string  `json:"updated"`
			Tests   []Entry `json:"tests"`
		}
		if legacyErr := json.Unmarshal(data, &legacy); legacyErr != nil {
			return nil, fmt.Errorf("failed to parse quarantine config %s: %w", path, err)
		}
		cfg.Version = legacy.Version
		cfg.Updated = legacy.Updated
		cfg.Tests = make(map[string]Entry, len(legacy.Tests))
		for _, e := range legacy.Tests {
			cfg.Tests[e.Name] = e
		}
		return &cfg, nil
	}

	if cfg.Tests == nil {
		cfg.Tests = make(map[string]Entry)
	}

	// Populate Name from map key for convenience.
	for name, e := range cfg.Tests {
		e.Name = name
		cfg.Tests[name] = e
	}

	return &cfg, nil
}

// SaveConfig writes the quarantine config to the given path atomically
// (write to temp file + rename) to avoid partial writes on crash.
func SaveConfig(path string, cfg *Config) error {
	cfg.Updated = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal quarantine config: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".quarantine-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("failed to write temp quarantine config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to close temp quarantine config: %w", err)
	}
	if err := os.Chmod(tmpName, configFilePerms); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to set permissions on temp quarantine config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to rename temp quarantine config to %s: %w", path, err)
	}

	return nil
}

// ActiveEntries returns entries that are currently quarantined (not past their
// re-enable date). Entries without a re_enable_after are always active.
func (c *Config) ActiveEntries() map[string]Entry {
	now := time.Now().UTC()
	active := make(map[string]Entry, len(c.Tests))

	for name, e := range c.Tests {
		if e.ReEnableAfter != "" {
			reEnable, err := time.Parse(time.RFC3339, e.ReEnableAfter)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: invalid re_enable_after for %q: %v\n", name, err)
			} else if now.After(reEnable) {
				continue
			}
		}
		active[name] = e
	}

	return active
}

// IsQuarantined checks whether the given test name matches any active
// quarantine entry (exact match or parent prefix).
func (c *Config) IsQuarantined(testName string) (bool, *Entry) {
	active := c.ActiveEntries()

	// Exact match (O(1) lookup).
	if e, ok := active[testName]; ok {
		return true, &e
	}

	// Prefix match: check if any active entry is a parent of testName.
	for _, e := range active {
		if strings.HasPrefix(testName, e.Name+"/") {
			return true, &e
		}
	}
	return false, nil
}

// QuarantinedNames returns sorted names of all actively quarantined tests.
func (c *Config) QuarantinedNames() []string {
	active := c.ActiveEntries()
	names := make([]string, 0, len(active))
	for name := range active {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// BuildSkipRegex builds a Go test -skip regex that matches all actively
// quarantined tests. Returns "" if no tests are quarantined.
func (c *Config) BuildSkipRegex() string {
	names := c.QuarantinedNames()
	if len(names) == 0 {
		return ""
	}

	patterns := make([]string, 0, len(names))
	for _, name := range names {
		parts := strings.Split(name, "/")
		anchored := make([]string, 0, len(parts))
		for _, p := range parts {
			anchored = append(anchored, fmt.Sprintf("^%s$", regexp.QuoteMeta(p)))
		}
		patterns = append(patterns, strings.Join(anchored, "/"))
	}

	return strings.Join(patterns, "|")
}

// AddOrUpdate inserts a new quarantine entry or updates an existing one
// with the same test name.
func (c *Config) AddOrUpdate(entry Entry) {
	c.Tests[entry.Name] = entry
}

// Remove deletes a quarantine entry by test name. Returns true if found.
func (c *Config) Remove(testName string) bool {
	if _, ok := c.Tests[testName]; !ok {
		return false
	}
	delete(c.Tests, testName)
	return true
}

// RemoveExpired removes entries whose re_enable_after date has passed.
// Returns the number of entries removed.
func (c *Config) RemoveExpired() int {
	now := time.Now().UTC()
	removed := 0

	for name, e := range c.Tests {
		if e.ReEnableAfter != "" {
			reEnable, err := time.Parse(time.RFC3339, e.ReEnableAfter)
			if err == nil && now.After(reEnable) {
				delete(c.Tests, name)
				removed++
			}
		}
	}

	return removed
}

// SetJiraKey updates the Jira field for the named entry.
func (c *Config) SetJiraKey(testName, jiraKey string) {
	if e, ok := c.Tests[testName]; ok {
		e.Jira = jiraKey
		c.Tests[testName] = e
	}
}

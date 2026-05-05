package quarantine

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const configFilePerms = 0600

// Config holds the quarantine configuration loaded from a JSON file.
type Config struct {
	Version int     `json:"version"`
	Updated string  `json:"updated"`
	Tests   []Entry `json:"tests"`
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
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Version: 1}, nil
		}
		return nil, fmt.Errorf("failed to read quarantine config %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse quarantine config %s: %w", path, err)
	}

	return &cfg, nil
}

// SaveConfig writes the quarantine config to the given path.
func SaveConfig(path string, cfg *Config) error {
	cfg.Updated = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal quarantine config: %w", err)
	}

	if err := os.WriteFile(path, data, configFilePerms); err != nil {
		return fmt.Errorf("failed to write quarantine config to %s: %w", path, err)
	}

	return nil
}

// ActiveEntries returns entries that are currently quarantined (not past their
// re-enable date). Entries without a re_enable_after are always active.
func (c *Config) ActiveEntries() []Entry {
	now := time.Now().UTC()
	active := make([]Entry, 0, len(c.Tests))

	for _, e := range c.Tests {
		if e.ReEnableAfter != "" {
			reEnable, err := time.Parse(time.RFC3339, e.ReEnableAfter)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: invalid re_enable_after for %q: %v\n", e.Name, err)
			} else if now.After(reEnable) {
				continue
			}
		}
		active = append(active, e)
	}

	return active
}

// IsQuarantined checks whether the given test name matches any active
// quarantine entry.
func (c *Config) IsQuarantined(testName string) (bool, *Entry) {
	for _, e := range c.ActiveEntries() {
		if e.Name == testName || strings.HasPrefix(testName, e.Name+"/") {
			return true, &e
		}
	}
	return false, nil
}

// QuarantinedNames returns sorted names of all actively quarantined tests.
func (c *Config) QuarantinedNames() []string {
	active := c.ActiveEntries()
	names := make([]string, 0, len(active))
	for _, e := range active {
		names = append(names, e.Name)
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
	for i, e := range c.Tests {
		if e.Name == entry.Name {
			c.Tests[i] = entry
			return
		}
	}
	c.Tests = append(c.Tests, entry)
}

// Remove deletes a quarantine entry by test name. Returns true if found.
func (c *Config) Remove(testName string) bool {
	for i, e := range c.Tests {
		if e.Name == testName {
			c.Tests = append(c.Tests[:i], c.Tests[i+1:]...)
			return true
		}
	}
	return false
}

package types

import (
	"time"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/config"
)

// TestResult represents the result of test run
type TestResult struct {
	PassedTest []TestCase
	FailedTest []TestCase
}

// PROptions holds PR notification configuration for git providers (GitHub, GitLab, etc.)
type PROptions struct {
	// Authentication token for the git provider
	Token    string
	Owner    string
	Repo     string
	PRNumber int
	// Label to add on test failures (optional)
	Label string
	// Comment to add on test failures (optional)
	Comment string
}

// E2ETestOptions holds options for e2e test execution
type E2ETestOptions struct {
	MaxRetries int
	TestFilter string
	TestFlags  string
	TestPath   string
	WorkingDir string
	Config     *config.Config
	// test prefixes that should never be skipped (always run)
	NeverSkipPrefixes []string
	// prefixes where tests should be extracted at prefix + 1 level
	SkipAtPrefixes  []string
	PROptions       PROptions
	JUnitOutputPath string // Path to JUnit XML output file (optional)
}

// TestCase represents a single test case (passed or failed)
type TestCase struct {
	ID            int
	Name          string
	Package       string
	Duration      time.Duration
	FailureOutput string    `json:",omitempty"`
	Time          time.Time `json:",omitempty"`
}

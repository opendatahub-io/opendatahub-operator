package types

import (
	"time"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/config"
)

// FailureClassification categorizes test failures based on cluster diagnostics.
// The E2E test framework emits classification data as JSON to stdout when tests fail.
// This type is used to parse and store that classification data.
//
// The classification framework emits JSON in the format:
// FAILURE_CLASSIFICATION: {"category":"infrastructure","subcategory":"image-pull",...}
//
// Error code ranges:
//   - 1000-1999: Infrastructure failures (image-pull, pod-startup, network, quota, node, storage)
//   - 2000-2999: Test logic failures
//   - 3000+: Unknown/unclassifiable
type FailureClassification struct {
	Category    string   `json:"category"`    // "infrastructure", "test", "unknown"
	Subcategory string   `json:"subcategory"` // e.g., "image-pull", "pod-startup", "test-failure"
	ErrorCode   int      `json:"error_code"`  // numeric error identifier
	Evidence    []string `json:"evidence"`    // supporting diagnostic messages
	Confidence  string   `json:"confidence"`  // "high", "medium", "low"
}

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
	Command    string // Custom command to run tests (defaults to "go test" if empty)
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
	ID             int
	Name           string
	Package        string
	Duration       time.Duration
	FailureOutput  string                  `json:",omitempty"`
	Time           time.Time               `json:",omitempty"`
	Classification *FailureClassification `json:",omitempty"` // Parsed from FAILURE_CLASSIFICATION: JSON output
}

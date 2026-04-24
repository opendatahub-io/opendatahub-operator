package flakerate

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/quarantine"
)

// FailurePattern classifies how a test fails across multiple CI runs.
type FailurePattern string

const (
	PatternHealthy    FailurePattern = "healthy"    // never fails
	PatternFlaky      FailurePattern = "flaky"      // intermittent failures scattered across runs
	PatternRegression FailurePattern = "regression" // was passing, now consistently failing
	PatternPersistent FailurePattern = "persistent" // fails in every observed run
)

// minRecentFailsForRegression is the minimum consecutive trailing failures
// required before a test is classified as a regression rather than flaky.
const minRecentFailsForRegression = 3

// maxPreTransitionFailRate is the maximum failure rate in the "before"
// window for a transition to count as a regression. If the test was already
// flaky before the transition, it stays classified as flaky.
const maxPreTransitionFailRate = 0.2

// RunOutcome records the result of a single test in one CI run.
type RunOutcome struct {
	Timestamp time.Time
	Failed    bool
	CommitSHA string
}

// TestRecord tracks pass/fail counts for a single test across multiple CI runs.
type TestRecord struct {
	Name       string
	TotalRuns  int
	FailedRuns int
	PassedRuns int
	LastSeen   time.Time
	LastFailed time.Time
	History    []RunOutcome
}

// FlakeRate returns the failure rate as a fraction [0.0, 1.0].
func (r *TestRecord) FlakeRate() float64 {
	if r.TotalRuns == 0 {
		return 0
	}
	return float64(r.FailedRuns) / float64(r.TotalRuns)
}

// ClassifyPattern analyzes the run history to determine whether failures
// are flaky (scattered) or a regression (step-function transition).
func (r *TestRecord) ClassifyPattern() FailurePattern {
	if r.TotalRuns == 0 || r.FailedRuns == 0 {
		return PatternHealthy
	}
	if r.PassedRuns == 0 {
		return PatternPersistent
	}

	sorted := r.sortedHistory()
	if len(sorted) < 2 {
		return PatternFlaky
	}

	trailingFails := countTrailingFailures(sorted)

	if trailingFails < minRecentFailsForRegression {
		return PatternFlaky
	}

	preTransitionEnd := len(sorted) - trailingFails
	if preTransitionEnd == 0 {
		return PatternPersistent
	}

	preFails := 0
	for _, run := range sorted[:preTransitionEnd] {
		if run.Failed {
			preFails++
		}
	}
	preFailRate := float64(preFails) / float64(preTransitionEnd)

	if preFailRate <= maxPreTransitionFailRate {
		return PatternRegression
	}

	return PatternFlaky
}

// TransitionCommit returns the commit SHA of the first failure in the
// trailing failure streak, if available. Returns "" when the pattern is
// not a regression or commit SHAs are not recorded.
func (r *TestRecord) TransitionCommit() string {
	if r.ClassifyPattern() != PatternRegression {
		return ""
	}
	sorted := r.sortedHistory()
	trailingFails := countTrailingFailures(sorted)
	transitionIdx := len(sorted) - trailingFails
	if transitionIdx < len(sorted) {
		return sorted[transitionIdx].CommitSHA
	}
	return ""
}

func (r *TestRecord) sortedHistory() []RunOutcome {
	sorted := make([]RunOutcome, len(r.History))
	copy(sorted, r.History)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})
	return sorted
}

func countTrailingFailures(sorted []RunOutcome) int {
	count := 0
	for i := len(sorted) - 1; i >= 0; i-- {
		if !sorted[i].Failed {
			break
		}
		count++
	}
	return count
}

// Report holds analysis results for a set of JUnit XML files.
type Report struct {
	TotalFiles int
	Tests      map[string]*TestRecord
}

// junitTestSuite mirrors the JUnit XML <testsuite> element for parsing.
type junitTestSuite struct {
	XMLName    xml.Name         `xml:"testsuite"`
	Name       string           `xml:"name,attr"`
	Properties *junitProperties `xml:"properties,omitempty"`
	TestCases  []junitTestCase  `xml:"testcase"`
}

// junitTestSuites supports files wrapped in <testsuites>.
type junitTestSuites struct {
	XMLName xml.Name         `xml:"testsuites"`
	Suites  []junitTestSuite `xml:"testsuite"`
}

type junitTestCase struct {
	Name    string        `xml:"name,attr"`
	Time    string        `xml:"time,attr"`
	Failure *junitFailure `xml:"failure,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr,omitempty"`
	Content string `xml:",chardata"`
}

type junitProperties struct {
	Property []junitProperty `xml:"property"`
}

type junitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// AnalyzeDir reads all JUnit XML files in dir and computes per-test flake rates.
func AnalyzeDir(dir string) (*Report, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	report := &Report{
		Tests: make(map[string]*TestRecord),
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".xml") {
			continue
		}

		path := filepath.Join(dir, name)
		if err := report.ingestFile(path); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", path, err)
		}
	}

	return report, nil
}

// ingestFile parses a single JUnit XML file and merges results into the report.
func (r *Report) ingestFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}
	fileTime := info.ModTime()

	suites, err := parseSuites(data)
	if err != nil {
		return err
	}

	r.TotalFiles++

	for _, suite := range suites {
		commitSHA := extractSuiteProperty(suite, "commit.sha")

		for _, tc := range suite.TestCases {
			record, ok := r.Tests[tc.Name]
			if !ok {
				record = &TestRecord{Name: tc.Name}
				r.Tests[tc.Name] = record
			}

			failed := tc.Failure != nil

			record.TotalRuns++
			if fileTime.After(record.LastSeen) {
				record.LastSeen = fileTime
			}

			if failed {
				record.FailedRuns++
				if fileTime.After(record.LastFailed) {
					record.LastFailed = fileTime
				}
			} else {
				record.PassedRuns++
			}

			record.History = append(record.History, RunOutcome{
				Timestamp: fileTime,
				Failed:    failed,
				CommitSHA: commitSHA,
			})
		}
	}

	return nil
}

func extractSuiteProperty(suite junitTestSuite, name string) string {
	if suite.Properties == nil {
		return ""
	}
	for _, p := range suite.Properties.Property {
		if p.Name == name {
			return p.Value
		}
	}
	return ""
}

func parseSuites(data []byte) ([]junitTestSuite, error) {
	// Try <testsuites> wrapper first
	var suites junitTestSuites
	if err := xml.Unmarshal(data, &suites); err == nil && len(suites.Suites) > 0 {
		return suites.Suites, nil
	}

	// Fall back to single <testsuite>
	var suite junitTestSuite
	if err := xml.Unmarshal(data, &suite); err != nil {
		return nil, fmt.Errorf("failed to parse JUnit XML: %w", err)
	}

	return []junitTestSuite{suite}, nil
}

// ExceedingThreshold returns test records whose flake rate exceeds the
// given threshold, sorted by flake rate descending.
func (r *Report) ExceedingThreshold(threshold float64) []*TestRecord {
	var results []*TestRecord
	for _, record := range r.Tests {
		if record.FlakeRate() > threshold {
			results = append(results, record)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].FlakeRate() > results[j].FlakeRate()
	})
	return results
}

// Regressions returns test records classified as regressions, sorted by
// last-failed time descending (most recent first).
func (r *Report) Regressions() []*TestRecord {
	var results []*TestRecord
	for _, record := range r.Tests {
		if record.ClassifyPattern() == PatternRegression {
			results = append(results, record)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].LastFailed.After(results[j].LastFailed)
	})
	return results
}

// FlakyTests returns test records classified as flaky (not regressions,
// not persistent, not healthy) that exceed the given threshold.
func (r *Report) FlakyTests(threshold float64) []*TestRecord {
	var results []*TestRecord
	for _, record := range r.Tests {
		if record.ClassifyPattern() == PatternFlaky && record.FlakeRate() > threshold {
			results = append(results, record)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].FlakeRate() > results[j].FlakeRate()
	})
	return results
}

// AutoQuarantine returns quarantine entries for tests that are flaky
// (not regressions) and exceed the threshold. Regressions are excluded
// because they indicate a real code change broke the test — quarantining
// would hide the regression.
func (r *Report) AutoQuarantine(threshold float64, windowDays int) []quarantine.Entry {
	flaky := r.FlakyTests(threshold)
	now := time.Now().UTC()
	entries := make([]quarantine.Entry, 0, len(flaky))

	reEnableAt := now.AddDate(0, 0, windowDays).Format(time.RFC3339)

	for _, rec := range flaky {
		entries = append(entries, quarantine.Entry{
			Name:          rec.Name,
			Reason:        fmt.Sprintf("Flake rate %.0f%% over %dd (%d/%d runs failed)", rec.FlakeRate()*100, windowDays, rec.FailedRuns, rec.TotalRuns),
			QuarantinedAt: now.Format(time.RFC3339),
			ReEnableAfter: reEnableAt,
			FlakeRate:     rec.FlakeRate(),
			TotalRuns:     rec.TotalRuns,
			FailedRuns:    rec.FailedRuns,
			WindowDays:    windowDays,
		})
	}

	return entries
}

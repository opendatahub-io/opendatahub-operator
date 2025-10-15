package runner

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/formatter"
	github "github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/github"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/parser"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/types"
)

// E2ETestRunner handles e2e test execution with retries
type E2ETestRunner struct {
	opts         types.E2ETestOptions
	githubClient github.GitHubClient
}

// NewE2ETestRunner creates a new e2e test runner
func NewE2ETestRunner(opts types.E2ETestOptions) *E2ETestRunner {
	return &E2ETestRunner{
		opts:         opts,
		githubClient: github.NewClient(opts.PROptions.Token),
	}
}

// Run executes e2e tests with retry logic
func (r *E2ETestRunner) Run() error {
	if r.opts.Config.Verbose {
		fmt.Println("Starting e2e test execution with retry functionality...")
	}

	// Aggregate results to collect all test attempts for JUnit export
	aggregateResult := &types.TestResult{
		PassedTest: make([]types.TestCase, 0),
		FailedTest: make([]types.TestCase, 0),
	}

	// Run initial test execution
	testResult, err := r.runE2ETests("")
	if err != nil {
		return fmt.Errorf("failed to run initial e2e tests: %w", err)
	}

	// Add initial results to aggregate
	aggregateResult.FailedTest = append(aggregateResult.FailedTest, testResult.FailedTest...)
	aggregateResult.PassedTest = append(aggregateResult.PassedTest, testResult.PassedTest...)

	if r.opts.Config.Verbose {
		fmt.Printf("Initial run: %d passed, %d failed, %d skipped\n",
			len(testResult.PassedTest), len(testResult.FailedTest), 0)
	}

	hasFirstRunFailedTests := len(testResult.FailedTest) > 0
	lastTestResult := testResult

	// Retry tests, skipping the ones that already passed
	for attempt := 1; attempt <= r.opts.MaxRetries && hasFirstRunFailedTests; attempt++ {
		if r.opts.Config.Verbose {
			fmt.Printf("Retry attempt %d\n", attempt)
		}

		// Run tests again, skipping the ones that passed
		retrySummary, err := r.runE2ETests(r.buildSkipFilter(aggregateResult, lastTestResult))
		if err != nil {
			if r.opts.Config.Verbose {
				fmt.Printf("Error in retry attempt %d: %v\n", attempt, err)
			}
			continue
		}

		// Add retry results to aggregate
		aggregateResult.FailedTest = append(aggregateResult.FailedTest, retrySummary.FailedTest...)
		aggregateResult.PassedTest = append(aggregateResult.PassedTest, retrySummary.PassedTest...)

		lastTestResult = retrySummary

		if r.opts.Config.Verbose {
			fmt.Printf("Retry %d: %d passed, %d failed, %d skipped\n",
				attempt, len(retrySummary.PassedTest), len(retrySummary.FailedTest), 0)
		}

		if len(retrySummary.FailedTest) == 0 {
			break
		}

	}

	// Export JUnit XML if path is specified
	if r.opts.JUnitOutputPath != "" {
		if err := r.exportJUnit(aggregateResult); err != nil {
			fmt.Printf("Warning: failed to export JUnit XML: %v\n", err)
		} else if r.opts.Config.Verbose {
			fmt.Printf("JUnit XML exported to %s\n", r.opts.JUnitOutputPath)
		}
	}

	// Final summary
	if len(lastTestResult.FailedTest) > 0 {
		fmt.Printf("❌ Final result: %d tests still failing after %d retries\n",
			len(lastTestResult.FailedTest), r.opts.MaxRetries)
		// Show which tests are still failing
		for _, failedTest := range lastTestResult.FailedTest {
			fmt.Printf("  - %s\n", failedTest.Name)
		}

		return fmt.Errorf("%d tests failed after retries", len(lastTestResult.FailedTest))
	}

	if hasFirstRunFailedTests {
		fmt.Println("⚠️  All tests passed, but some tests were flaky (failed initially but passed on retry)")

		// Notify PR if GitHub info is provided
		r.notifyPROnFailure()

		return nil
	}

	fmt.Println("✅ All tests passed!")
	return nil
}

// runE2ETests executes e2e tests using go test
func (r *E2ETestRunner) runE2ETests(skipTestFilter string) (*types.TestResult, error) {
	// Build go test command
	args := []string{"test"}

	// Add test path
	args = append(args, r.opts.TestPath)

	// Add test filter
	if r.opts.TestFilter != "" {
		args = append(args, "-run", r.opts.TestFilter)
	}

	if skipTestFilter != "" {
		args = append(args, "-skip", skipTestFilter)
	}

	// Add verbose output, json output, and count flag to avoid test caching
	args = append(args, "-v", "-json", "-count=1")

	// Add custom test flags
	if r.opts.TestFlags != "" {
		customFlags := strings.Fields(r.opts.TestFlags)
		args = append(args, customFlags...)
	}

	if r.opts.Config.Verbose {
		fmt.Printf("Running: go %s\n", strings.Join(args, " "))
	}
	// Execute command
	cmd := exec.Command("go", args...)

	// Set working directory if explicitly provided, otherwise use current directory
	if r.opts.WorkingDir != "" {
		cmd.Dir = r.opts.WorkingDir
	}

	var stdout, stderr io.Reader
	var err error
	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err = cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to run %s: %w", strings.Join(cmd.Args, " "), err)
	}

	// Parse JSON output for final summary (without duplicate output)
	testResult, err := parser.ParseGoTestJSON(parser.ParseConfig{
		Stdout: stdout,
		Stderr: stderr,
	})
	if err != nil {
		return nil, err
	}

	if exitErr := cmd.Wait(); isExitError(exitErr) {
		return nil, exitErr
	}
	return testResult, err
}

func isExitError(err error) bool {
	if err == nil {
		return false
	}
	if exiterr, ok := err.(*exec.ExitError); ok {
		// We consider exit code of 0 and 1 as expected
		if code := exiterr.ExitCode(); code > 1 {
			return true
		}
	}
	return false
}

// buildSkipFilter creates a regex pattern to skip all passed tests at their appropriate levels
func (r *E2ETestRunner) buildSkipFilter(aggregateResult, lastTestResult *types.TestResult) string {
	if lastTestResult == nil {
		lastTestResult = &types.TestResult{}
	}
	if aggregateResult == nil {
		aggregateResult = &types.TestResult{}
	}
	totalPassedTests := aggregateResult.PassedTest
	lastFailedTests := lastTestResult.FailedTest
	if len(totalPassedTests) == 0 {
		return ""
	}

	// Extract normalized test levels for both passed and failed tests
	passedLevels := make(map[string]bool)
	for _, passedTest := range totalPassedTests {
		if level, shouldSkip := r.extractTestLevel(passedTest.Name); shouldSkip {
			passedLevels[level] = true
		}
	}

	// Track which test groups have failures
	failedGroups := make(map[string]bool)
	for _, failedTest := range lastFailedTests {
		if level, shouldSkip := r.extractTestLevel(failedTest.Name); shouldSkip {
			failedGroups[level] = true
		}
	}

	if len(passedLevels) == 0 {
		return ""
	}

	// Build a regex pattern that matches any test starting with the normalized levels
	// BUT exclude groups that have any failures (to avoid skipping failed siblings)
	var filters []string
	for level := range passedLevels {
		// Only skip this level if no tests in the same group failed
		if !failedGroups[level] {
			// Escape special regex characters in test names
			escapedName := regexp.QuoteMeta(level)

			// Match tests that start with this level
			filters = append(filters, buildGoTestSkipFilter(escapedName)...)
		}
	}

	if len(filters) == 0 {
		return ""
	}

	sort.Strings(filters)

	return strings.Join(filters, "|")
}

func buildGoTestSkipFilter(testName string) []string {
	var skipFilter []string
	testNameParts := strings.Split(testName, "/")
	for _, part := range testNameParts {
		skipFilter = append(skipFilter, fmt.Sprintf("^%s$", part))
	}
	return []string{strings.Join(skipFilter, "/")}
}

// extractTestLevel extracts the appropriate level from test names based on configuration rules
func (r *E2ETestRunner) extractTestLevel(testName string) (string, bool) {
	// Check if test should never be skipped
	for _, prefix := range r.opts.NeverSkipPrefixes {
		normalizedPrefix := normalizePrefix(prefix)
		if strings.HasPrefix(testName, normalizedPrefix) || prefix == testName {
			return "", false
		}
	}

	// Find the longest matching skip-at-prefix
	var longestMatch string
	for _, prefix := range r.opts.SkipAtPrefixes {
		normalizedPrefix := normalizePrefix(prefix)
		if strings.HasPrefix(testName, normalizedPrefix) || prefix == testName {
			if strings.Contains(testName, "/") {
				longestMatch = getLongestMatch(longestMatch, normalizedPrefix)
			} else {
				longestMatch = getLongestMatch(longestMatch, prefix)
			}
		}
	}

	// If no match found, don't skip
	if longestMatch == "" {
		return "", false
	}

	remainder := strings.TrimPrefix(testName, longestMatch)
	if remainder == "" {
		return longestMatch, true
	}

	// Return prefix + first part of remainder
	return normalizePrefix(longestMatch) + strings.Split(remainder, "/")[0], true
}

// Normalize prefix to end with /
func normalizePrefix(prefix string) string {
	if strings.HasSuffix(prefix, "/") {
		return prefix
	}
	return prefix + "/"
}

func getLongestMatch(actualLongestMatch string, testName string) string {
	actualLongestMatchParts := strings.Split(actualLongestMatch, "/")
	testNameParts := strings.Split(testName, "/")
	if len(actualLongestMatchParts) <= len(testNameParts) {
		return testName
	}
	return actualLongestMatch
}

// exportJUnit exports test results to JUnit XML format
func (r *E2ETestRunner) exportJUnit(result *types.TestResult) error {
	return formatter.ExportToJUnit(result, formatter.JUnitExportOptions{
		OutputPath: r.opts.JUnitOutputPath,
		SuiteName:  "e2e-test",
	})
}

// notifyPROnFailure adds a label and/or comment to the GitHub PR if configured
func (r *E2ETestRunner) notifyPROnFailure() {
	// Only proceed if basic GitHub options are configured
	if r.opts.PROptions.Token == "" || r.opts.PROptions.Owner == "" || r.opts.PROptions.Repo == "" || r.opts.PROptions.PRNumber == 0 {
		return
	}

	// Check if either label or comment is configured
	if r.opts.PROptions.Label == "" && r.opts.PROptions.Comment == "" {
		return
	}

	ctx := context.Background()

	// Add label if configured
	if r.opts.PROptions.Label != "" {
		label := r.opts.PROptions.Label

		if r.opts.Config.Verbose {
			fmt.Printf("Adding label '%s' to PR #%d in %s/%s\n",
				label, r.opts.PROptions.PRNumber, r.opts.PROptions.Owner, r.opts.PROptions.Repo)
		}

		err := r.githubClient.AddLabel(ctx, r.opts.PROptions.Owner, r.opts.PROptions.Repo, r.opts.PROptions.PRNumber, label)
		if err != nil {
			fmt.Printf("Warning: failed to add label to PR: %v\n", err)
		} else {
			fmt.Printf("✓ Successfully added label '%s' to PR #%d\n", label, r.opts.PROptions.PRNumber)
		}
	}

	// Add comment if configured
	if r.opts.PROptions.Comment != "" {
		comment := r.opts.PROptions.Comment

		if r.opts.Config.Verbose {
			fmt.Printf("Adding comment to PR #%d in %s/%s\n",
				r.opts.PROptions.PRNumber, r.opts.PROptions.Owner, r.opts.PROptions.Repo)
		}

		err := r.githubClient.AddComment(ctx, r.opts.PROptions.Owner, r.opts.PROptions.Repo, r.opts.PROptions.PRNumber, comment)
		if err != nil {
			fmt.Printf("Warning: failed to add comment to PR: %v\n", err)
		} else {
			fmt.Printf("✓ Successfully added comment to PR #%d\n", r.opts.PROptions.PRNumber)
		}
	}
}

package parser

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"gotest.tools/gotestsum/testjson"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/types"
)

const (
	// ClassificationPrefix is the prefix for classification JSON lines in test output
	ClassificationPrefix = "FAILURE_CLASSIFICATION: "
)

type ParseConfig struct {
	Stdout io.Reader
	Stderr io.Reader
}

// classificationTracker monitors test execution and extracts classification data
// from FAILURE_CLASSIFICATION: JSON lines in test output.
type classificationTracker struct {
	mu              sync.Mutex
	currentTest     string                                    // currently running test name
	classifications map[string]*types.FailureClassification // test name → classification
}

// newClassificationTracker creates a new classification tracker
func newClassificationTracker() *classificationTracker {
	return &classificationTracker{
		classifications: make(map[string]*types.FailureClassification),
	}
}

// parseClassificationLine extracts FailureClassification from a FAILURE_CLASSIFICATION: JSON line.
// Returns nil if the line is not a classification line or if parsing fails.
func parseClassificationLine(line string) (*types.FailureClassification, error) {
	if !strings.HasPrefix(line, ClassificationPrefix) {
		return nil, nil // not a classification line
	}

	jsonStr := strings.TrimPrefix(line, ClassificationPrefix)
	var fc types.FailureClassification

	if err := json.Unmarshal([]byte(jsonStr), &fc); err != nil {
		return nil, fmt.Errorf("failed to parse classification JSON: %w", err)
	}

	return &fc, nil
}

// handleEvent processes test events and extracts classifications from output.
// This tracks which test is currently running and associates classification
// output with the correct test name.
func (t *classificationTracker) handleEvent(event testjson.TestEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	switch event.Action {
	case testjson.ActionRun:
		// Test started - remember the test name
		// event.Test is the test name (string)
		t.currentTest = event.Test

	case testjson.ActionOutput:
		// Test produced output - check for classification
		fc, err := parseClassificationLine(event.Output)
		if err != nil {
			// Log error but don't fail - classification is optional enhancement
			fmt.Fprintf(os.Stderr, "Warning: failed to parse classification: %v\n", err)
			return
		}
		if fc != nil {
			// Associate classification with the current test
			// Use event.Test if available, otherwise fall back to currentTest
			testName := event.Test
			if testName == "" {
				testName = t.currentTest
			}
			if testName != "" {
				t.classifications[testName] = fc
			}
		}

	case testjson.ActionFail, testjson.ActionPass, testjson.ActionSkip:
		// Test finished - clear current test
		if event.Test == t.currentTest {
			t.currentTest = ""
		}
	}
}

// getClassification retrieves the classification for a test name.
// Returns nil if no classification exists for this test.
func (t *classificationTracker) getClassification(testName string) *types.FailureClassification {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.classifications[testName]
}

// ParseGoTestJSON parses go test -json output using testjson library
func ParseGoTestJSON(cfg ParseConfig) (*types.TestResult, error) {
	formatter := testjson.NewEventFormatter(os.Stdout, "standard-verbose", testjson.FormatOptions{})

	// Create classification tracker to extract FAILURE_CLASSIFICATION: lines
	tracker := newClassificationTracker()

	handler := &eventHandler{
		formatter: formatter,
		tracker:   tracker,
	}

	// Use ScanTestOutput to parse and get execution results
	execution, err := testjson.ScanTestOutput(testjson.ScanConfig{
		Stdout:                   cfg.Stdout,
		Stderr:                   cfg.Stderr,
		IgnoreNonJSONOutputLines: true,
		Handler:                  handler,
	})
	if err != nil {
		return nil, fmt.Errorf("error scanning test output: %w", err)
	}

	if len(execution.Errors()) > 0 {
		return nil, fmt.Errorf("errors found in execution: %v", execution.Errors())
	}

	testResult := &types.TestResult{
		PassedTest: make([]types.TestCase, 0),
		FailedTest: make([]types.TestCase, 0),
	}

	packages := execution.Packages()
	for _, pkg := range packages {
		pkg := execution.Package(pkg)

		if len(pkg.TestCases()) == 0 && pkg.Result() == testjson.ActionFail {
			return nil, fmt.Errorf("package failed")
		}

		for _, test := range pkg.Failed {
			testCase := types.TestCase{
				ID:            test.ID,
				Name:          test.Test.Name(),
				Package:       test.Package,
				Duration:      test.Elapsed,
				FailureOutput: strings.Join(pkg.OutputLines(test), ""),
				Time:          test.Time,
			}

			// Attach classification if available for this test
			if classification := tracker.getClassification(test.Test.Name()); classification != nil {
				testCase.Classification = classification
			}

			testResult.FailedTest = append(testResult.FailedTest, testCase)
		}

		for _, test := range pkg.Passed {
			testResult.PassedTest = append(testResult.PassedTest, types.TestCase{
				ID:       test.ID,
				Name:     test.Test.Name(),
				Package:  test.Package,
				Duration: test.Elapsed,
				Time:     test.Time,
			})
		}
	}

	return testResult, nil
}

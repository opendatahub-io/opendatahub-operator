package formatter

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/tests/tagging"
)

// unmarshalJUnit parses JUnit XML that uses either <testsuites> or bare <testsuite> as root.
func unmarshalJUnit(content []byte) (TestSuites, error) {
	var suites TestSuites
	if err := xml.Unmarshal(content, &suites); err == nil {
		return suites, nil
	}

	var single TestSuite
	if err := xml.Unmarshal(content, &single); err != nil {
		return TestSuites{}, fmt.Errorf("parse junit xml: %w", err)
	}

	return TestSuites{
		Tests:    single.Tests,
		Failures: single.Failures,
		Skipped:  single.Skipped,
		Time:     single.Time,
		Suites:   []TestSuite{single},
	}, nil
}

// FilterGateSkippedTestsFile filters gate-skipped cases in path and overwrites the file.
func FilterGateSkippedTestsFile(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read junit file %q: %w", path, err)
	}

	filtered, err := FilterGateSkippedTests(content)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "junit-filtered-*.xml")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(filtered); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp junit file: %w", err)
	}
	
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
        return fmt.Errorf("rename temp file: %w", err)
    }

    return nil
}

// FilterGateSkippedTests removes testcases skipped only by tag-gate mismatch.
// Other skipped, passed, and failed cases are preserved; suite counters are recomputed.
// Handles both <testsuites> (gotestsum) and bare <testsuite> (test-retry) root elements.
func FilterGateSkippedTests(content []byte) ([]byte, error) {
	suites, err := unmarshalJUnit(content)
	if err != nil {
		return nil, err
	}

	totalTests := 0
	totalFailures := 0
	totalSkipped := 0

	for i := range suites.Suites {
		kept := make([]TestCase, 0, len(suites.Suites[i].TestCases))
		failures := 0
		skipped := 0

		for _, tc := range suites.Suites[i].TestCases {
			if tc.Skipped != nil && strings.Contains(tc.Skipped.Message, tagging.GateSkipPrefix) {
				continue
			}

			kept = append(kept, tc)

			switch {
			case tc.Failure != nil:
				failures++
			case tc.Skipped != nil:
				skipped++
			}
		}

		suites.Suites[i].TestCases = kept
		suites.Suites[i].Tests = len(kept)
		suites.Suites[i].Failures = failures
		suites.Suites[i].Skipped = skipped

		totalTests += suites.Suites[i].Tests
		totalFailures += suites.Suites[i].Failures
		totalSkipped += suites.Suites[i].Skipped
	}

	suites.Tests = totalTests
	suites.Failures = totalFailures
	suites.Skipped = totalSkipped

	out, err := xml.MarshalIndent(suites, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal junit xml: %w", err)
	}
	return append([]byte(xml.Header), out...), nil
}

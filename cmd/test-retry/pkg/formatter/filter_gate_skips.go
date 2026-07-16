package formatter

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

// gateSkipPrefix matches skip messages emitted by e2e tag gating (skipUnless).
const gateSkipPrefix = "Skipping test: passed tag:"

// JUnit structs used only for gate-skip filtering (gotestsum XML shape).

type filterTestSuites struct {
	XMLName  xml.Name          `xml:"testsuites"`
	Tests    int               `xml:"tests,attr"`
	Failures int               `xml:"failures,attr"`
	Skipped  int               `xml:"skipped,attr"`
	Time     string            `xml:"time,attr,omitempty"`
	Suites   []filterTestSuite `xml:"testsuite"`
}

type filterTestSuite struct {
	XMLName   xml.Name         `xml:"testsuite"`
	Name      string           `xml:"name,attr"`
	Tests     int              `xml:"tests,attr"`
	Failures  int              `xml:"failures,attr"`
	Skipped   int              `xml:"skipped,attr"`
	Time      string           `xml:"time,attr,omitempty"`
	TestCases []filterTestCase `xml:"testcase"`
}

type filterTestCase struct {
	Classname string         `xml:"classname,attr,omitempty"`
	Name      string         `xml:"name,attr"`
	Time      string         `xml:"time,attr,omitempty"`
	Skipped   *filterSkipped `xml:"skipped,omitempty"`
	Failure   *filterFailure `xml:"failure,omitempty"`
}

type filterSkipped struct {
	Message string `xml:"message,attr,omitempty"`
}

type filterFailure struct {
	Message string `xml:"message,attr,omitempty"`
	Content string `xml:",chardata"`
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

	if err := os.WriteFile(path, filtered, 0o644); err != nil {
		return fmt.Errorf("write junit file %q: %w", path, err)
	}
	return nil
}

// FilterGateSkippedTests removes testcases skipped only by tag-gate mismatch.
// Other skipped, passed, and failed cases are preserved; suite counters are recomputed.
func FilterGateSkippedTests(content []byte) ([]byte, error) {
	var suites filterTestSuites
	if err := xml.Unmarshal(content, &suites); err != nil {
		return nil, fmt.Errorf("parse junit xml: %w", err)
	}

	totalTests := 0
	totalFailures := 0
	totalSkipped := 0

	for i := range suites.Suites {
		kept := make([]filterTestCase, 0, len(suites.Suites[i].TestCases))
		failures := 0
		skipped := 0

		for _, tc := range suites.Suites[i].TestCases {
			if tc.Skipped != nil && strings.Contains(tc.Skipped.Message, gateSkipPrefix) {
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

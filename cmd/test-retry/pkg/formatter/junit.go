package formatter

import (
	"encoding/xml"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/types"
)

// TestSuite represents a JUnit XML test suite
type TestSuite struct {
	XMLName   xml.Name   `xml:"testsuite"`
	Name      string     `xml:"name,attr"`
	Tests     int        `xml:"tests,attr"`
	Failures  int        `xml:"failures,attr"`
	TestCases []TestCase `xml:"testcase"`
}

// TestCase represents a JUnit XML test case
type TestCase struct {
	Name     string    `xml:"name,attr"`
	Duration string    `xml:"time,attr"`
	Failure  *Failure  `xml:"failure,omitempty"`
	Time     time.Time `xml:"-"`
}

// Failure represents a JUnit XML test failure
type Failure struct {
	Message string `xml:"message,attr,omitempty"`
	Content string `xml:",chardata"`
}

// JUnitExportOptions holds configuration for JUnit export
type JUnitExportOptions struct {
	OutputPath string
	SuiteName  string
}

// ExportToJUnit exports test results to JUnit XML format
// For flaky tests (failed then passed), this creates multiple test case entries:
// - One entry for each failed attempt with <failure> element
// - One entry for the successful attempt without <failure>
func ExportToJUnit(result *types.TestResult, opts JUnitExportOptions) error {
	if opts.OutputPath == "" {
		return fmt.Errorf("output path is required")
	}

	if opts.SuiteName == "" {
		return fmt.Errorf("suite name is required")
	}

	suite := convertToJUnitSuite(result, opts.SuiteName)

	// Marshal to XML with indentation
	output, err := xml.MarshalIndent(suite, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JUnit XML: %w", err)
	}

	// Add XML header
	xmlContent := []byte(xml.Header + string(output))

	// Write to file
	if err := os.WriteFile(opts.OutputPath, xmlContent, 0644); err != nil {
		return fmt.Errorf("failed to write JUnit XML file: %w", err)
	}

	return nil
}

// convertToJUnitSuite converts TestResult to JUnit TestSuite
func convertToJUnitSuite(result *types.TestResult, suiteName string) TestSuite {
	suite := TestSuite{
		Name:      suiteName,
		TestCases: make([]TestCase, 0),
	}

	unorderedTestCases := make([]TestCase, 0)

	// Add failed test cases
	for _, test := range result.FailedTest {
		tc := TestCase{
			Name:     test.Name,
			Duration: formatDuration(test.Duration),
			Failure: &Failure{
				Message: fmt.Sprintf("Test %s failed", test.Name),
				Content: test.FailureOutput,
			},
			Time: test.Time,
		}

		unorderedTestCases = append(unorderedTestCases, tc)
		suite.Failures++
	}

	// Add passed test cases
	for _, test := range result.PassedTest {
		tc := TestCase{
			Name:     test.Name,
			Duration: formatDuration(test.Duration),
			Time:     test.Time,
		}

		unorderedTestCases = append(unorderedTestCases, tc)
	}

	// Sort test cases by time
	sort.Slice(unorderedTestCases, func(i, j int) bool {
		return unorderedTestCases[i].Time.Before(unorderedTestCases[j].Time)
	})
	suite.TestCases = unorderedTestCases

	// Calculate totals
	suite.Tests = len(suite.TestCases)

	return suite
}

// formatDuration formats a duration to a string in seconds with decimals
func formatDuration(d time.Duration) string {
	return fmt.Sprintf("%.3f", d.Seconds())
}

package formatter

import (
	"encoding/xml"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/types"
)

// TestSuite represents a JUnit XML test suite
type TestSuite struct {
	XMLName    xml.Name    `xml:"testsuite"`
	Name       string      `xml:"name,attr"`
	Timestamp  string      `xml:"timestamp,attr,omitempty"`
	Tests      int         `xml:"tests,attr"`
	Failures   int         `xml:"failures,attr"`
	Properties *Properties `xml:"properties,omitempty"`
	TestCases  []TestCase  `xml:"testcase"`
}

// TestCase represents a JUnit XML test case
type TestCase struct {
	Name       string      `xml:"name,attr"`
	Duration   string      `xml:"time,attr"`
	Failure    *Failure    `xml:"failure,omitempty"`
	Properties *Properties `xml:"properties,omitempty"`
	Time       time.Time   `xml:"-"`
}

// Failure represents a JUnit XML test failure
type Failure struct {
	Message string `xml:"message,attr,omitempty"`
	Content string `xml:",chardata"`
}

// Properties represents JUnit XML <properties> element
type Properties struct {
	Property []Property `xml:"property"`
}

// Property represents a single <property> element with name/value attributes
type Property struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// JUnitExportOptions holds configuration for JUnit export
type JUnitExportOptions struct {
	OutputPath string
	SuiteName  string
	CommitSHA  string // Git commit SHA to embed as a suite-level property (optional)
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

	suite := convertToJUnitSuite(result, opts)

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

// convertToJUnitSuite converts TestResult to JUnit TestSuite.
func convertToJUnitSuite(result *types.TestResult, opts JUnitExportOptions) TestSuite {
	suite := TestSuite{
		Name:       opts.SuiteName,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Properties: buildSuiteProperties(opts),
		TestCases:  make([]TestCase, 0),
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
			Properties: buildClassificationProperties(test.Classification),
			Time:       test.Time,
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

// buildSuiteProperties creates suite-level properties (e.g. commit SHA).
// Returns nil if no suite-level metadata is configured.
func buildSuiteProperties(opts JUnitExportOptions) *Properties {
	if opts.CommitSHA == "" {
		return nil
	}
	return &Properties{
		Property: []Property{
			{Name: "commit.sha", Value: opts.CommitSHA},
		},
	}
}

// buildClassificationProperties creates JUnit properties from FailureClassification.
// Returns nil if the classification is nil (test has no classification).
//
// Property naming convention:
//   - failure.category: "infrastructure", "test", "unknown"
//   - failure.subcategory: specific cause (e.g., "image-pull", "pod-startup")
//   - failure.error_code: numeric error identifier
//   - failure.confidence: "high", "medium", "low"
//   - failure.evidence: semicolon-separated evidence strings
func buildClassificationProperties(fc *types.FailureClassification) *Properties {
	if fc == nil {
		return nil
	}

	// Format evidence: join array with semicolons for single property value
	evidenceStr := strings.Join(fc.Evidence, "; ")

	return &Properties{
		Property: []Property{
			{Name: "failure.category", Value: fc.Category},
			{Name: "failure.subcategory", Value: fc.Subcategory},
			{Name: "failure.error_code", Value: strconv.Itoa(fc.ErrorCode)},
			{Name: "failure.confidence", Value: fc.Confidence},
			{Name: "failure.evidence", Value: evidenceStr},
		},
	}
}

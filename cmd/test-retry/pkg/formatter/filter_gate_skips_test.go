package formatter

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterGateSkippedTests(t *testing.T) {
	input := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="6" failures="1" skipped="3" time="25.000">
  <testsuite name="odh-operator-e2e" tests="6" failures="1" skipped="3" time="25.000">
    <testcase classname="e2e" name="passed" time="2.100"></testcase>
    <testcase classname="e2e" name="gate-skip-kserve" time="0.001">
      <skipped message="Skipping test: passed tag: Smoke, test tags: [Tier2 Tier3]"></skipped>
    </testcase>
    <testcase classname="e2e" name="gate-skip-ray" time="0.001">
      <skipped message="Skipping test: passed tag: Smoke, test tags: [Tier3]"></skipped>
    </testcase>
    <testcase classname="e2e" name="real-skip-crd" time="0.002">
      <skipped message="cluster does not have required CRDs"></skipped>
    </testcase>
    <testcase classname="e2e" name="real-skip-breaker" time="0.001">
      <skipped message="circuit breaker open"></skipped>
    </testcase>
    <testcase classname="e2e" name="failed" time="3.400">
      <failure message="Test failed">expected Ready, got Failed</failure>
    </testcase>
  </testsuite>
</testsuites>
`)

	out, err := FilterGateSkippedTests(input)
	require.NoError(t, err)

	var suites filterTestSuites
	require.NoError(t, xml.Unmarshal(out, &suites))

	require.Len(t, suites.Suites, 1)
	suite := suites.Suites[0]

	require.Equal(t, 4, suite.Tests)
	require.Equal(t, 1, suite.Failures)
	require.Equal(t, 2, suite.Skipped)
	require.Equal(t, 4, suites.Tests)
	require.Equal(t, 1, suites.Failures)
	require.Equal(t, 2, suites.Skipped)

	names := make([]string, 0, len(suite.TestCases))
	for _, tc := range suite.TestCases {
		names = append(names, tc.Name)
	}
	require.Equal(t, []string{
		"passed",
		"real-skip-crd",
		"real-skip-breaker",
		"failed",
	}, names)

	require.NotContains(t, string(out), "Skipping test: passed tag:")
	require.Contains(t, string(out), "cluster does not have required CRDs")
	require.Contains(t, string(out), "circuit breaker open")
}

func TestFilterGateSkippedTests_NoGateSkipsUnchangedCounts(t *testing.T) {
	input := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="2" failures="0" skipped="1" time="1.000">
  <testsuite name="odh-operator-e2e" tests="2" failures="0" skipped="1" time="1.000">
    <testcase classname="e2e" name="passed" time="0.500"></testcase>
    <testcase classname="e2e" name="real-skip" time="0.001">
      <skipped message="known issue RHOAIENG-123"></skipped>
    </testcase>
  </testsuite>
</testsuites>
`)

	out, err := FilterGateSkippedTests(input)
	require.NoError(t, err)

	var suites filterTestSuites
	require.NoError(t, xml.Unmarshal(out, &suites))
	require.Equal(t, 2, suites.Tests)
	require.Equal(t, 1, suites.Skipped)
	require.Len(t, suites.Suites[0].TestCases, 2)
}

func TestFilterGateSkippedTestsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "xunit_report.xml")

	input := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="2" failures="0" skipped="1" time="1.000">
  <testsuite name="odh-operator-e2e" tests="2" failures="0" skipped="1" time="1.000">
    <testcase classname="e2e" name="gate-skip" time="0.001">
      <skipped message="Skipping test: passed tag: Tier1, test tags: [Smoke]"></skipped>
    </testcase>
    <testcase classname="e2e" name="passed" time="0.500"></testcase>
  </testsuite>
</testsuites>
`)
	require.NoError(t, os.WriteFile(path, input, 0o644))

	require.NoError(t, FilterGateSkippedTestsFile(path))

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	var suites filterTestSuites
	require.NoError(t, xml.Unmarshal(content, &suites))
	require.Equal(t, 1, suites.Tests)
	require.Equal(t, 0, suites.Skipped)
	require.Equal(t, "passed", suites.Suites[0].TestCases[0].Name)
}

func TestFilterGateSkippedTests_InvalidXML(t *testing.T) {
	_, err := FilterGateSkippedTests([]byte(`not xml`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse junit xml")
}

func TestFilterGateSkippedTests_AllGateSkipsRemoved(t *testing.T) {
	input := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="2" failures="0" skipped="2" time="0.002">
  <testsuite name="odh-operator-e2e" tests="2" failures="0" skipped="2" time="0.002">
    <testcase classname="e2e" name="gate-a" time="0.001">
      <skipped message="Skipping test: passed tag: Smoke, test tags: [Tier2]"></skipped>
    </testcase>
    <testcase classname="e2e" name="gate-b" time="0.001">
      <skipped message="Skipping test: passed tag: Tier1, test tags: [Tier3]"></skipped>
    </testcase>
  </testsuite>
</testsuites>
`)

	out, err := FilterGateSkippedTests(input)
	require.NoError(t, err)

	var suites filterTestSuites
	require.NoError(t, xml.Unmarshal(out, &suites))
	require.Equal(t, 0, suites.Tests)
	require.Equal(t, 0, suites.Skipped)
	require.Equal(t, 0, suites.Failures)
	require.Empty(t, suites.Suites[0].TestCases)
}

func TestFilterGateSkippedTests_MultipleSuites(t *testing.T) {
	input := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="4" failures="0" skipped="2" time="1.000">
  <testsuite name="suite-a" tests="2" failures="0" skipped="1" time="0.500">
    <testcase classname="e2e" name="pass-a" time="0.400"></testcase>
    <testcase classname="e2e" name="gate-a" time="0.001">
      <skipped message="Skipping test: passed tag: Smoke, test tags: [Tier2]"></skipped>
    </testcase>
  </testsuite>
  <testsuite name="suite-b" tests="2" failures="0" skipped="1" time="0.500">
    <testcase classname="e2e" name="real-skip-b" time="0.001">
      <skipped message="feature disabled"></skipped>
    </testcase>
    <testcase classname="e2e" name="gate-b" time="0.001">
      <skipped message="Skipping test: passed tag: Tier1, test tags: [Smoke]"></skipped>
    </testcase>
  </testsuite>
</testsuites>
`)

	out, err := FilterGateSkippedTests(input)
	require.NoError(t, err)

	var suites filterTestSuites
	require.NoError(t, xml.Unmarshal(out, &suites))
	require.Len(t, suites.Suites, 2)

	require.Equal(t, 2, suites.Tests)
	require.Equal(t, 1, suites.Skipped)

	require.Equal(t, "suite-a", suites.Suites[0].Name)
	require.Equal(t, 1, suites.Suites[0].Tests)
	require.Equal(t, 0, suites.Suites[0].Skipped)
	require.Equal(t, "pass-a", suites.Suites[0].TestCases[0].Name)

	require.Equal(t, "suite-b", suites.Suites[1].Name)
	require.Equal(t, 1, suites.Suites[1].Tests)
	require.Equal(t, 1, suites.Suites[1].Skipped)
	require.Equal(t, "real-skip-b", suites.Suites[1].TestCases[0].Name)
}

func TestFilterGateSkippedTests_KeepsSimilarButNonGateSkipMessage(t *testing.T) {
	input := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="2" failures="0" skipped="2" time="0.002">
  <testsuite name="odh-operator-e2e" tests="2" failures="0" skipped="2" time="0.002">
    <testcase classname="e2e" name="similar-skip" time="0.001">
      <skipped message="Skipping because feature flag disabled"></skipped>
    </testcase>
    <testcase classname="e2e" name="empty-skip" time="0.001">
      <skipped message=""></skipped>
    </testcase>
  </testsuite>
</testsuites>
`)

	out, err := FilterGateSkippedTests(input)
	require.NoError(t, err)

	var suites filterTestSuites
	require.NoError(t, xml.Unmarshal(out, &suites))
	require.Equal(t, 2, suites.Tests)
	require.Equal(t, 2, suites.Skipped)
	require.Len(t, suites.Suites[0].TestCases, 2)
}

func TestFilterGateSkippedTests_EmptySuite(t *testing.T) {
	input := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="0" failures="0" skipped="0" time="0.000">
  <testsuite name="odh-operator-e2e" tests="0" failures="0" skipped="0" time="0.000">
  </testsuite>
</testsuites>
`)

	out, err := FilterGateSkippedTests(input)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(out), xml.Header))

	var suites filterTestSuites
	require.NoError(t, xml.Unmarshal(out, &suites))
	require.Equal(t, 0, suites.Tests)
	require.Empty(t, suites.Suites[0].TestCases)
}

func TestFilterGateSkippedTestsFile_MissingFile(t *testing.T) {
	err := FilterGateSkippedTestsFile(filepath.Join(t.TempDir(), "missing.xml"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "read junit file")
}

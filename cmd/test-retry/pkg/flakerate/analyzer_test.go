package flakerate_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/flakerate"
)

//nolint:gosec // G101 false positive: XML test fixture, not credentials.
const junitPass = `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="e2e-test" tests="2" failures="0">
  <testcase name="TestOdhOperator/components/dashboard" time="10.000"></testcase>
  <testcase name="TestOdhOperator/services/monitoring" time="20.000"></testcase>
</testsuite>`

const junitFail = `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="e2e-test" tests="2" failures="1">
  <testcase name="TestOdhOperator/components/dashboard" time="10.000"></testcase>
  <testcase name="TestOdhOperator/services/monitoring" time="20.000">
    <failure message="timed out">timeout after 10m</failure>
  </testcase>
</testsuite>`

const junitAllFail = `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="e2e-test" tests="1" failures="1">
  <testcase name="TestOdhOperator/services/monitoring" time="5.000">
    <failure message="flaky">intermittent</failure>
  </testcase>
</testsuite>`

func writeXML(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0600))
}

func TestAnalyzeDir(t *testing.T) {
	t.Parallel()

	t.Run("empty directory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		report, err := flakerate.AnalyzeDir(dir)
		require.NoError(t, err)
		assert.Equal(t, 0, report.TotalFiles)
		assert.Empty(t, report.Tests)
	})

	t.Run("skips non-xml files", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0600))

		report, err := flakerate.AnalyzeDir(dir)
		require.NoError(t, err)
		assert.Equal(t, 0, report.TotalFiles)
	})

	t.Run("single passing run", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeXML(t, dir, "run1.xml", junitPass)

		report, err := flakerate.AnalyzeDir(dir)
		require.NoError(t, err)
		assert.Equal(t, 1, report.TotalFiles)
		assert.Len(t, report.Tests, 2)

		dashboard := report.Tests["TestOdhOperator/components/dashboard"]
		require.NotNil(t, dashboard)
		assert.Equal(t, 1, dashboard.TotalRuns)
		assert.Equal(t, 0, dashboard.FailedRuns)
		assert.InDelta(t, 0.0, dashboard.FlakeRate(), 0.001)
	})

	t.Run("mixed pass and fail across runs", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeXML(t, dir, "run1.xml", junitPass)
		writeXML(t, dir, "run2.xml", junitFail)

		report, err := flakerate.AnalyzeDir(dir)
		require.NoError(t, err)
		assert.Equal(t, 2, report.TotalFiles)

		monitoring := report.Tests["TestOdhOperator/services/monitoring"]
		require.NotNil(t, monitoring)
		assert.Equal(t, 2, monitoring.TotalRuns)
		assert.Equal(t, 1, monitoring.FailedRuns)
		assert.InDelta(t, 0.5, monitoring.FlakeRate(), 0.001)

		dashboard := report.Tests["TestOdhOperator/components/dashboard"]
		require.NotNil(t, dashboard)
		assert.Equal(t, 2, dashboard.TotalRuns)
		assert.Equal(t, 0, dashboard.FailedRuns)
		assert.InDelta(t, 0.0, dashboard.FlakeRate(), 0.001)
	})

	t.Run("builds run history", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeXML(t, dir, "run1.xml", junitPass)
		writeXML(t, dir, "run2.xml", junitFail)

		report, err := flakerate.AnalyzeDir(dir)
		require.NoError(t, err)

		monitoring := report.Tests["TestOdhOperator/services/monitoring"]
		require.NotNil(t, monitoring)
		assert.Len(t, monitoring.History, 2)
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		t.Parallel()
		_, err := flakerate.AnalyzeDir("/nonexistent/path")
		require.Error(t, err)
	})
}

func TestExceedingThreshold(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeXML(t, dir, "run1.xml", junitPass)
	writeXML(t, dir, "run2.xml", junitFail)
	writeXML(t, dir, "run3.xml", junitAllFail)

	report, err := flakerate.AnalyzeDir(dir)
	require.NoError(t, err)

	t.Run("threshold 0.3 catches monitoring", func(t *testing.T) {
		t.Parallel()
		exceeding := report.ExceedingThreshold(0.3)
		require.Len(t, exceeding, 1)
		assert.Equal(t, "TestOdhOperator/services/monitoring", exceeding[0].Name)
	})

	t.Run("threshold 0.0 catches everything with any failure", func(t *testing.T) {
		t.Parallel()
		exceeding := report.ExceedingThreshold(0.0)
		require.GreaterOrEqual(t, len(exceeding), 1)
	})

	t.Run("threshold 1.0 catches nothing", func(t *testing.T) {
		t.Parallel()
		exceeding := report.ExceedingThreshold(1.0)
		assert.Empty(t, exceeding)
	})
}

func TestAutoQuarantine(t *testing.T) {
	t.Parallel()

	t.Run("quarantines flaky tests", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeXML(t, dir, "run1.xml", junitPass)
		writeXML(t, dir, "run2.xml", junitFail)

		report, err := flakerate.AnalyzeDir(dir)
		require.NoError(t, err)

		entries := report.AutoQuarantine(0.3, 30)
		require.Len(t, entries, 1)
		assert.Equal(t, "TestOdhOperator/services/monitoring", entries[0].Name)
		assert.InDelta(t, 0.5, entries[0].FlakeRate, 0.001)
		assert.Equal(t, 30, entries[0].WindowDays)
		assert.Contains(t, entries[0].Reason, "50%")
	})

	t.Run("excludes regressions from quarantine", func(t *testing.T) {
		t.Parallel()
		rec := &flakerate.TestRecord{
			Name:       "TestRegression",
			TotalRuns:  6,
			FailedRuns: 3,
			PassedRuns: 3,
		}
		now := time.Now()
		for i := 0; i < 3; i++ {
			rec.History = append(rec.History, flakerate.RunOutcome{
				Timestamp: now.Add(time.Duration(i) * time.Hour),
				Failed:    false,
			})
		}
		for i := 3; i < 6; i++ {
			rec.History = append(rec.History, flakerate.RunOutcome{
				Timestamp: now.Add(time.Duration(i) * time.Hour),
				Failed:    true,
			})
		}

		assert.Equal(t, flakerate.PatternRegression, rec.ClassifyPattern())

		report := &flakerate.Report{
			TotalFiles: 6,
			Tests:      map[string]*flakerate.TestRecord{"TestRegression": rec},
		}
		entries := report.AutoQuarantine(0.2, 30)
		assert.Empty(t, entries, "regressions should not be quarantined")
	})
}

func TestFlakeRate(t *testing.T) {
	t.Parallel()

	t.Run("zero runs", func(t *testing.T) {
		t.Parallel()
		r := &flakerate.TestRecord{}
		assert.InDelta(t, 0.0, r.FlakeRate(), 0.001)
	})

	t.Run("all pass", func(t *testing.T) {
		t.Parallel()
		r := &flakerate.TestRecord{TotalRuns: 10, PassedRuns: 10}
		assert.InDelta(t, 0.0, r.FlakeRate(), 0.001)
	})

	t.Run("all fail", func(t *testing.T) {
		t.Parallel()
		r := &flakerate.TestRecord{TotalRuns: 5, FailedRuns: 5}
		assert.InDelta(t, 1.0, r.FlakeRate(), 0.001)
	})

	t.Run("mixed", func(t *testing.T) {
		t.Parallel()
		r := &flakerate.TestRecord{TotalRuns: 4, FailedRuns: 1, PassedRuns: 3}
		assert.InDelta(t, 0.25, r.FlakeRate(), 0.001)
	})
}

func TestClassifyPattern(t *testing.T) {
	t.Parallel()
	now := time.Now()

	t.Run("healthy - no failures", func(t *testing.T) {
		t.Parallel()
		r := &flakerate.TestRecord{TotalRuns: 5, PassedRuns: 5}
		assert.Equal(t, flakerate.PatternHealthy, r.ClassifyPattern())
	})

	t.Run("healthy - zero runs", func(t *testing.T) {
		t.Parallel()
		r := &flakerate.TestRecord{}
		assert.Equal(t, flakerate.PatternHealthy, r.ClassifyPattern())
	})

	t.Run("persistent - all fail", func(t *testing.T) {
		t.Parallel()
		r := &flakerate.TestRecord{
			TotalRuns:  5,
			FailedRuns: 5,
			History: []flakerate.RunOutcome{
				{Timestamp: now, Failed: true},
				{Timestamp: now.Add(time.Hour), Failed: true},
				{Timestamp: now.Add(2 * time.Hour), Failed: true},
				{Timestamp: now.Add(3 * time.Hour), Failed: true},
				{Timestamp: now.Add(4 * time.Hour), Failed: true},
			},
		}
		assert.Equal(t, flakerate.PatternPersistent, r.ClassifyPattern())
	})

	t.Run("regression - passes then fails", func(t *testing.T) {
		t.Parallel()
		r := &flakerate.TestRecord{
			TotalRuns:  7,
			FailedRuns: 3,
			PassedRuns: 4,
			History: []flakerate.RunOutcome{
				{Timestamp: now, Failed: false},
				{Timestamp: now.Add(1 * time.Hour), Failed: false},
				{Timestamp: now.Add(2 * time.Hour), Failed: false},
				{Timestamp: now.Add(3 * time.Hour), Failed: false},
				{Timestamp: now.Add(4 * time.Hour), Failed: true},
				{Timestamp: now.Add(5 * time.Hour), Failed: true},
				{Timestamp: now.Add(6 * time.Hour), Failed: true},
			},
		}
		assert.Equal(t, flakerate.PatternRegression, r.ClassifyPattern())
	})

	t.Run("flaky - scattered failures", func(t *testing.T) {
		t.Parallel()
		r := &flakerate.TestRecord{
			TotalRuns:  6,
			FailedRuns: 2,
			PassedRuns: 4,
			History: []flakerate.RunOutcome{
				{Timestamp: now, Failed: false},
				{Timestamp: now.Add(1 * time.Hour), Failed: true},
				{Timestamp: now.Add(2 * time.Hour), Failed: false},
				{Timestamp: now.Add(3 * time.Hour), Failed: false},
				{Timestamp: now.Add(4 * time.Hour), Failed: true},
				{Timestamp: now.Add(5 * time.Hour), Failed: false},
			},
		}
		assert.Equal(t, flakerate.PatternFlaky, r.ClassifyPattern())
	})

	t.Run("flaky - not enough trailing failures for regression", func(t *testing.T) {
		t.Parallel()
		r := &flakerate.TestRecord{
			TotalRuns:  5,
			FailedRuns: 2,
			PassedRuns: 3,
			History: []flakerate.RunOutcome{
				{Timestamp: now, Failed: false},
				{Timestamp: now.Add(1 * time.Hour), Failed: false},
				{Timestamp: now.Add(2 * time.Hour), Failed: false},
				{Timestamp: now.Add(3 * time.Hour), Failed: true},
				{Timestamp: now.Add(4 * time.Hour), Failed: true},
			},
		}
		// Only 2 trailing failures, min is 3 → flaky, not regression.
		assert.Equal(t, flakerate.PatternFlaky, r.ClassifyPattern())
	})

	t.Run("flaky - was already flaky before transition", func(t *testing.T) {
		t.Parallel()
		r := &flakerate.TestRecord{
			TotalRuns:  8,
			FailedRuns: 5,
			PassedRuns: 3,
			History: []flakerate.RunOutcome{
				{Timestamp: now, Failed: false},
				{Timestamp: now.Add(1 * time.Hour), Failed: true},
				{Timestamp: now.Add(2 * time.Hour), Failed: true},
				{Timestamp: now.Add(3 * time.Hour), Failed: false},
				{Timestamp: now.Add(4 * time.Hour), Failed: false},
				{Timestamp: now.Add(5 * time.Hour), Failed: true},
				{Timestamp: now.Add(6 * time.Hour), Failed: true},
				{Timestamp: now.Add(7 * time.Hour), Failed: true},
			},
		}
		// 3 trailing failures, but pre-transition fail rate is 2/5 = 40% > 20% → still flaky.
		assert.Equal(t, flakerate.PatternFlaky, r.ClassifyPattern())
	})

	t.Run("single run failure", func(t *testing.T) {
		t.Parallel()
		r := &flakerate.TestRecord{
			TotalRuns:  1,
			FailedRuns: 1,
			History: []flakerate.RunOutcome{
				{Timestamp: now, Failed: true},
			},
		}
		// Single run, all fails, no passes → persistent.
		assert.Equal(t, flakerate.PatternPersistent, r.ClassifyPattern())
	})
}

func TestTransitionCommit(t *testing.T) {
	t.Parallel()
	now := time.Now()

	t.Run("returns commit SHA at transition point", func(t *testing.T) {
		t.Parallel()
		r := &flakerate.TestRecord{
			TotalRuns:  6,
			FailedRuns: 3,
			PassedRuns: 3,
			History: []flakerate.RunOutcome{
				{Timestamp: now, Failed: false, CommitSHA: "aaa111"},
				{Timestamp: now.Add(1 * time.Hour), Failed: false, CommitSHA: "bbb222"},
				{Timestamp: now.Add(2 * time.Hour), Failed: false, CommitSHA: "ccc333"},
				{Timestamp: now.Add(3 * time.Hour), Failed: true, CommitSHA: "ddd444"},
				{Timestamp: now.Add(4 * time.Hour), Failed: true, CommitSHA: "eee555"},
				{Timestamp: now.Add(5 * time.Hour), Failed: true, CommitSHA: "fff666"},
			},
		}
		assert.Equal(t, flakerate.PatternRegression, r.ClassifyPattern())
		assert.Equal(t, "ddd444", r.TransitionCommit())
	})

	t.Run("returns empty for non-regression", func(t *testing.T) {
		t.Parallel()
		r := &flakerate.TestRecord{
			TotalRuns:  3,
			PassedRuns: 3,
		}
		assert.Empty(t, r.TransitionCommit())
	})

	t.Run("returns empty when no commit SHA recorded", func(t *testing.T) {
		t.Parallel()
		r := &flakerate.TestRecord{
			TotalRuns:  6,
			FailedRuns: 3,
			PassedRuns: 3,
			History: []flakerate.RunOutcome{
				{Timestamp: now, Failed: false},
				{Timestamp: now.Add(1 * time.Hour), Failed: false},
				{Timestamp: now.Add(2 * time.Hour), Failed: false},
				{Timestamp: now.Add(3 * time.Hour), Failed: true},
				{Timestamp: now.Add(4 * time.Hour), Failed: true},
				{Timestamp: now.Add(5 * time.Hour), Failed: true},
			},
		}
		assert.Equal(t, flakerate.PatternRegression, r.ClassifyPattern())
		assert.Empty(t, r.TransitionCommit())
	})
}

func TestReportRegressions(t *testing.T) {
	t.Parallel()
	now := time.Now()

	report := &flakerate.Report{
		Tests: map[string]*flakerate.TestRecord{
			"TestHealthy": {
				Name: "TestHealthy", TotalRuns: 5, PassedRuns: 5,
			},
			"TestRegression": {
				Name: "TestRegression", TotalRuns: 6, FailedRuns: 3, PassedRuns: 3,
				LastFailed: now,
				History: []flakerate.RunOutcome{
					{Timestamp: now.Add(-5 * time.Hour), Failed: false},
					{Timestamp: now.Add(-4 * time.Hour), Failed: false},
					{Timestamp: now.Add(-3 * time.Hour), Failed: false},
					{Timestamp: now.Add(-2 * time.Hour), Failed: true},
					{Timestamp: now.Add(-1 * time.Hour), Failed: true},
					{Timestamp: now, Failed: true},
				},
			},
			"TestFlaky": {
				Name: "TestFlaky", TotalRuns: 6, FailedRuns: 2, PassedRuns: 4,
				History: []flakerate.RunOutcome{
					{Timestamp: now.Add(-5 * time.Hour), Failed: true},
					{Timestamp: now.Add(-4 * time.Hour), Failed: false},
					{Timestamp: now.Add(-3 * time.Hour), Failed: false},
					{Timestamp: now.Add(-2 * time.Hour), Failed: true},
					{Timestamp: now.Add(-1 * time.Hour), Failed: false},
					{Timestamp: now, Failed: false},
				},
			},
		},
	}

	regressions := report.Regressions()
	require.Len(t, regressions, 1)
	assert.Equal(t, "TestRegression", regressions[0].Name)

	flaky := report.FlakyTests(0.1)
	require.Len(t, flaky, 1)
	assert.Equal(t, "TestFlaky", flaky[0].Name)
}

func TestCommitSHAFromSuiteProperties(t *testing.T) {
	t.Parallel()

	junitWithCommit := `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="e2e-test" tests="1" failures="1">
  <properties>
    <property name="commit.sha" value="abc123def"/>
  </properties>
  <testcase name="TestA" time="1.000">
    <failure message="fail">failed</failure>
  </testcase>
</testsuite>`

	dir := t.TempDir()
	writeXML(t, dir, "run1.xml", junitWithCommit)

	report, err := flakerate.AnalyzeDir(dir)
	require.NoError(t, err)

	testA := report.Tests["TestA"]
	require.NotNil(t, testA)
	require.Len(t, testA.History, 1)
	assert.Equal(t, "abc123def", testA.History[0].CommitSHA)
}

func TestTestSuitesWrapper(t *testing.T) {
	t.Parallel()

	junitWrapped := `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="suite1" tests="1" failures="0">
    <testcase name="TestA" time="1.000"></testcase>
  </testsuite>
  <testsuite name="suite2" tests="1" failures="1">
    <testcase name="TestB" time="2.000">
      <failure message="fail">failed</failure>
    </testcase>
  </testsuite>
</testsuites>`

	dir := t.TempDir()
	writeXML(t, dir, "wrapped.xml", junitWrapped)

	report, err := flakerate.AnalyzeDir(dir)
	require.NoError(t, err)
	assert.Equal(t, 1, report.TotalFiles)
	assert.Len(t, report.Tests, 2)

	assert.Equal(t, 0, report.Tests["TestA"].FailedRuns)
	assert.Equal(t, 1, report.Tests["TestB"].FailedRuns)
}

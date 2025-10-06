package runner

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/config"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/formatter"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/types"
	"github.com/stretchr/testify/require"
)

func TestAllTestsPassFirstAttempt(t *testing.T) {
	testPath := "./testdata/passing"
	opts := types.E2ETestOptions{
		MaxRetries:        3,
		TestPath:          testPath,
		Config:            &config.Config{Verbose: false},
		NeverSkipPrefixes: []string{},
		SkipAtPrefixes:    []string{},
	}

	runner := NewE2ETestRunner(opts)
	err := runner.Run()

	require.NoError(t, err, "Expected all tests to pass on first attempt")
}

func TestNotExistsFolderShouldFail(t *testing.T) {
	testPath := "./testdata/not-exists"
	opts := types.E2ETestOptions{
		MaxRetries:        3,
		TestPath:          testPath,
		Config:            &config.Config{Verbose: false},
		NeverSkipPrefixes: []string{},
		SkipAtPrefixes:    []string{},
	}

	runner := NewE2ETestRunner(opts)
	err := runner.Run()

	require.Error(t, err)
}

func TestRunFlakyTestsPassAfterRetry(t *testing.T) {
	testPath := "./testdata/flaky"
	opts := types.E2ETestOptions{
		MaxRetries:        3,
		TestPath:          testPath,
		Config:            &config.Config{Verbose: false},
		NeverSkipPrefixes: []string{},
		SkipAtPrefixes:    []string{},
	}

	runner := NewE2ETestRunner(opts)

	// The flaky tests will fail on first run, but pass on retry
	err := runner.Run()

	// After retries, all tests should pass
	require.NoError(t, err, "Expected flaky tests to pass after retry")
}

func TestFailAfterMaxRetries(t *testing.T) {
	testPath := "./testdata/failing"
	opts := types.E2ETestOptions{
		MaxRetries:        2,
		TestPath:          testPath,
		Config:            &config.Config{Verbose: false},
		NeverSkipPrefixes: []string{},
		SkipAtPrefixes:    []string{},
	}

	runner := NewE2ETestRunner(opts)
	err := runner.Run()

	require.Error(t, err, "Expected tests to fail after max retries")
	require.Contains(t, err.Error(), "tests failed after retries")
}

func TestRunSkipFilterApplication(t *testing.T) {
	testPath := "./testdata/skipfilter"
	opts := types.E2ETestOptions{
		MaxRetries:        2,
		TestPath:          testPath,
		Config:            &config.Config{Verbose: false},
		NeverSkipPrefixes: []string{},
		SkipAtPrefixes:    []string{"TestSkipFilter_Pass2", "TestSkipFilter_Pass1"},
	}

	runner := NewE2ETestRunner(opts)
	err := runner.Run()

	require.NoError(t, err, "Expected all tests to eventually pass")
}

func TestRunSkipFilterSiblingApplication(t *testing.T) {
	testPath := "./testdata/siblings"
	opts := types.E2ETestOptions{
		MaxRetries:        2,
		TestPath:          testPath,
		Config:            &config.Config{Verbose: false},
		NeverSkipPrefixes: []string{},
		SkipAtPrefixes:    []string{"TestSiblings/"},
	}

	runner := NewE2ETestRunner(opts)
	err := runner.Run()

	require.EqualError(t, err, "3 tests failed after retries")
}

func TestGitHubNotificationOnFlakyTests(t *testing.T) {
	testPath := "./testdata/flaky"

	opts := types.E2ETestOptions{
		MaxRetries:        1,
		TestPath:          testPath,
		Config:            &config.Config{Verbose: false},
		NeverSkipPrefixes: []string{},
		SkipAtPrefixes:    []string{},
		PROptions: types.PROptions{
			Token:    "test-token",
			Owner:    "test-owner",
			Repo:     "test-repo",
			PRNumber: 123,
			Label:    "flaky-tests",
			Comment:  "Tests failed after retries",
		},
	}

	runner := NewE2ETestRunner(opts)

	mockClient := &mockGitHubClient{}
	runner.githubClient = mockClient

	err := runner.Run()

	require.NoError(t, err)

	require.True(t, mockClient.addLabelCalled)
	require.True(t, mockClient.addCommentCalled)
	require.Equal(t, "flaky-tests", mockClient.lastLabel)
	require.Equal(t, "Tests failed after retries", mockClient.lastComment)
}

func TestGitHubNotificationNotCalledOnSuccess(t *testing.T) {
	testPath := "./testdata/passing"

	opts := types.E2ETestOptions{
		MaxRetries:        1,
		TestPath:          testPath,
		Config:            &config.Config{Verbose: false},
		NeverSkipPrefixes: []string{},
		SkipAtPrefixes:    []string{},
		PROptions: types.PROptions{
			Token:    "test-token",
			Owner:    "test-owner",
			Repo:     "test-repo",
			PRNumber: 123,
			Label:    "flaky-tests",
			Comment:  "Tests failed",
		},
	}

	runner := NewE2ETestRunner(opts)
	mockClient := &mockGitHubClient{}
	runner.githubClient = mockClient

	err := runner.Run()

	require.NoError(t, err, "Expected tests to pass")

	require.False(t, mockClient.addLabelCalled, "Expected AddLabel NOT to be called on success")
	require.False(t, mockClient.addCommentCalled, "Expected AddComment NOT to be called on success")
}

func TestGitHubNotificationNotCalledOnFailure(t *testing.T) {
	testPath := "./testdata/failing"

	opts := types.E2ETestOptions{
		MaxRetries:        1,
		TestPath:          testPath,
		Config:            &config.Config{Verbose: false},
		NeverSkipPrefixes: []string{},
		SkipAtPrefixes:    []string{},
		PROptions: types.PROptions{
			Token:    "test-token",
			Owner:    "test-owner",
			Repo:     "test-repo",
			PRNumber: 123,
			Label:    "flaky-tests",
			Comment:  "Tests failed",
		},
	}

	runner := NewE2ETestRunner(opts)
	mockClient := &mockGitHubClient{}
	runner.githubClient = mockClient

	err := runner.Run()

	require.EqualError(t, err, "2 tests failed after retries")

	require.False(t, mockClient.addLabelCalled, "Expected AddLabel NOT to be called on success")
	require.False(t, mockClient.addCommentCalled, "Expected AddComment NOT to be called on success")
}

func TestJUnitExport_FlakyTests(t *testing.T) {
	testPath := "./testdata/flaky"
	tempDir := t.TempDir()
	junitPath := filepath.Join(tempDir, "junit.xml")

	opts := types.E2ETestOptions{
		MaxRetries:        3,
		TestPath:          testPath,
		Config:            &config.Config{Verbose: false},
		NeverSkipPrefixes: []string{},
		SkipAtPrefixes:    []string{"TestNonFlaky"},
		JUnitOutputPath:   junitPath,
	}

	runner := NewE2ETestRunner(opts)

	err := runner.Run()
	require.NoError(t, err)

	suite := readJUnitFile(t, junitPath)

	require.Equal(t, 5, suite.Tests, "Should have multiple test case entries (failed + passed)")
	require.Equal(t, 2, suite.Failures, "Should have failures from initial attempts")

	require.Equal(t, "TestFlaky1", suite.TestCases[0].Name)
	require.NotNil(t, suite.TestCases[0].Failure)

	require.Equal(t, "TestFlaky2", suite.TestCases[1].Name)
	require.NotNil(t, suite.TestCases[1].Failure)

	require.Equal(t, "TestNonFlaky", suite.TestCases[2].Name)
	require.Nil(t, suite.TestCases[2].Failure)

	require.Equal(t, "TestFlaky1", suite.TestCases[3].Name)
	require.Nil(t, suite.TestCases[3].Failure)

	require.Equal(t, "TestFlaky2", suite.TestCases[4].Name)
	require.Nil(t, suite.TestCases[4].Failure)
}

func readJUnitFile(t *testing.T, path string) formatter.TestSuite {
	t.Helper()

	_, err := os.Stat(path)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var suite formatter.TestSuite
	err = xml.Unmarshal(data, &suite)
	require.NoError(t, err)

	return suite
}

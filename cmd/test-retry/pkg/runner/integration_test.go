package runner

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/config"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/formatter"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/types"
)

type testResult struct {
	Failures  int
	TestCases []testCaseOutput
}

type testCaseOutput struct {
	Name       string
	HasFailure bool
}

func TestRunnerIntegration(t *testing.T) {
	testCases := []struct {
		name           string
		testPath       string
		skipAtPrefixes []string

		expectedError       string
		expectedResults     []testCaseOutput
		resultFileNotExists bool
	}{
		{
			name:     "passes at first attempt",
			testPath: "./testdata/passing",
			expectedResults: []testCaseOutput{
				{Name: "TestAlwaysPass1", HasFailure: false},
				{Name: "TestAlwaysPass2", HasFailure: false},
				{Name: "TestAlwaysPass3", HasFailure: false},
			},
		},
		{
			name:          "fails after max retries",
			testPath:      "./testdata/failing",
			expectedError: "2 tests failed after retries",
			expectedResults: []testCaseOutput{
				{Name: "TestAlwaysFail1", HasFailure: true},
				{Name: "TestAlwaysFail2", HasFailure: true},
				{Name: "TestPass", HasFailure: false},
				{Name: "TestAlwaysFail1", HasFailure: true},
				{Name: "TestAlwaysFail2", HasFailure: true},
				{Name: "TestPass", HasFailure: false},
				{Name: "TestAlwaysFail1", HasFailure: true},
				{Name: "TestAlwaysFail2", HasFailure: true},
				{Name: "TestPass", HasFailure: false},
			},
		},
		{
			name:           "flaky tests pass after retry - with skip at prefix set",
			testPath:       "./testdata/flaky",
			skipAtPrefixes: []string{"TestNonFlaky"},
			expectedResults: []testCaseOutput{
				{Name: "TestFlaky1", HasFailure: true},
				{Name: "TestFlaky2", HasFailure: true},
				{Name: "TestNonFlaky", HasFailure: false},
				{Name: "TestFlaky1", HasFailure: false},
				{Name: "TestFlaky2", HasFailure: false},
			},
		},
		{
			name:     "flaky tests pass after retry - with skip at prefix NOT set",
			testPath: "./testdata/flaky",
			expectedResults: []testCaseOutput{
				{Name: "TestFlaky1", HasFailure: true},
				{Name: "TestFlaky2", HasFailure: true},
				{Name: "TestNonFlaky", HasFailure: false},
				{Name: "TestFlaky1", HasFailure: false},
				{Name: "TestFlaky2", HasFailure: false},
				{Name: "TestNonFlaky", HasFailure: false},
			},
		},
		{
			name:           "skip filter root level",
			testPath:       "./testdata/skipfilter",
			skipAtPrefixes: []string{"TestSkipFilter_Pass2", "TestSkipFilter_Pass1"},
			expectedResults: []testCaseOutput{
				{Name: "TestSkipFilter_Pass1", HasFailure: false},
				{Name: "TestSkipFilter_Pass2", HasFailure: false},
				{Name: "TestSkipFilter_Flaky", HasFailure: true},
				{Name: "TestSkipFilter_Flaky", HasFailure: false},
			},
		},
		{
			name:           "skip filter sibling level",
			testPath:       "./testdata/siblings",
			skipAtPrefixes: []string{"TestSiblings/"},
			expectedError:  "3 tests failed after retries",
			expectedResults: []testCaseOutput{
				{Name: "TestSiblings", HasFailure: true},
				{Name: "TestSiblings/nested_with_same_prefix", HasFailure: true},
				{Name: "TestSiblings/nested_with_same_prefix/sibling_1", HasFailure: true},
				{Name: "TestSiblings/nested_with_same_prefix/sibling_2", HasFailure: false},
				{Name: "TestSiblings/nested", HasFailure: false},
				{Name: "TestSiblings", HasFailure: true},
				{Name: "TestSiblings/nested_with_same_prefix", HasFailure: true},
				{Name: "TestSiblings/nested_with_same_prefix/sibling_1", HasFailure: true},
				{Name: "TestSiblings/nested_with_same_prefix/sibling_2", HasFailure: false},
				{Name: "TestSiblings", HasFailure: true},
				{Name: "TestSiblings/nested_with_same_prefix", HasFailure: true},
				{Name: "TestSiblings/nested_with_same_prefix/sibling_1", HasFailure: true},
				{Name: "TestSiblings/nested_with_same_prefix/sibling_2", HasFailure: false},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			options := types.E2ETestOptions{
				MaxRetries:        2,
				TestPath:          testCase.testPath,
				Config:            &config.Config{Verbose: false},
				NeverSkipPrefixes: []string{},
				SkipAtPrefixes:    testCase.skipAtPrefixes,
				JUnitOutputPath:   filepath.Join(t.TempDir(), "junit.xml"),
			}

			runner := NewE2ETestRunner(options)
			err := runner.Run()

			if testCase.expectedError != "" {
				require.EqualError(t, err, testCase.expectedError)
			} else {
				require.NoError(t, err)
			}

			suite := readJUnitFile(t, runner.opts.JUnitOutputPath)

			testCases := make([]testCaseOutput, 0)
			for _, testCase := range suite.TestCases {
				testCases = append(testCases, testCaseOutput{
					Name:       testCase.Name,
					HasFailure: testCase.Failure != nil,
				})
			}
			require.Equal(t, testCase.expectedResults, testCases)
			require.Equal(t, len(testCase.expectedResults), suite.Tests)

			expectedFailures := 0
			for _, testCase := range testCase.expectedResults {
				if testCase.HasFailure {
					expectedFailures++
				}
			}
			require.Equal(t, expectedFailures, suite.Failures)
		})
	}
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

func readJUnitFile(t *testing.T, path string) formatter.TestSuite {
	t.Helper()

	require.FileExists(t, path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var suite formatter.TestSuite
	err = xml.Unmarshal(data, &suite)
	require.NoError(t, err)

	return suite
}

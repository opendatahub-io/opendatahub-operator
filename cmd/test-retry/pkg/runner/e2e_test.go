package runner

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/config"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/types"
)

func TestNewE2ETestRunner(t *testing.T) {
	opts := types.E2ETestOptions{
		MaxRetries: 3,
		TestFilter: "TestExample",
		TestPath:   "./tests/e2e",
		Config:     &config.Config{Verbose: true},
	}

	runner := NewE2ETestRunner(opts)

	require.NotNil(t, runner)
	require.Equal(t, opts.MaxRetries, runner.opts.MaxRetries)
	require.Equal(t, opts.TestFilter, runner.opts.TestFilter)
	require.Equal(t, opts.TestPath, runner.opts.TestPath)
}

func TestBuildSkipFilter(t *testing.T) {
	defaultOpts := types.E2ETestOptions{
		NeverSkipPrefixes: []string{"TestOdhOperator/DSCInitialization_and_DataScienceCluster_management_E2E_Tests"},
		SkipAtPrefixes:    []string{"TestOdhOperator/services/", "TestOdhOperator/components/", "TestOdhOperator/"},
	}

	tests := []struct {
		name                 string
		opts                 types.E2ETestOptions
		aggregatedTestResult *types.TestResult
		expected             string
	}{
		{
			name:     "no passed tests",
			opts:     defaultOpts,
			expected: "",
		},
		{
			name: "single passed test",
			opts: defaultOpts,
			aggregatedTestResult: &types.TestResult{PassedTest: []types.TestCase{
				{Name: "TestOdhOperator"},
				{Name: "TestOdhOperator/Feature1"},
				{Name: "TestOdhOperator/Feature1/test_case"},
			}},
			expected: "^TestOdhOperator$/^Feature1$",
		},
		{
			name: "multiple passed tests",
			opts: defaultOpts,
			aggregatedTestResult: &types.TestResult{PassedTest: []types.TestCase{
				{Name: "TestOdhOperator"},
				{Name: "TestOdhOperator/Feature1"},
				{Name: "TestOdhOperator/Feature1/test_case"},
				{Name: "TestOdhOperator/Feature2"},
			}},
			expected: "^TestOdhOperator$/^Feature1$|^TestOdhOperator$/^Feature2$",
		},
		{
			name: "service test - should use third level",
			opts: defaultOpts,
			aggregatedTestResult: &types.TestResult{PassedTest: []types.TestCase{
				{Name: "TestOdhOperator"},
				{Name: "TestOdhOperator/services"},
				{Name: "TestOdhOperator/services/auth"},
				{Name: "TestOdhOperator/services/auth/subtest"},
			}},
			expected: "^TestOdhOperator$/^services$|^TestOdhOperator$/^services$/^auth$",
		},
		{
			name: "component test - should use third level",
			opts: defaultOpts,
			aggregatedTestResult: &types.TestResult{PassedTest: []types.TestCase{
				{Name: "TestOdhOperator"},
				{Name: "TestOdhOperator/components"},
				{Name: "TestOdhOperator/components/dashboard"},
				{Name: "TestOdhOperator/components/dashboard/subtest"},
			}},
			expected: "^TestOdhOperator$/^components$|^TestOdhOperator$/^components$/^dashboard$",
		},
		{
			name: "dsc initialization test - should not be skipped",
			opts: defaultOpts,
			aggregatedTestResult: &types.TestResult{PassedTest: []types.TestCase{
				{Name: "TestOdhOperator"},
				{Name: "TestOdhOperator/DSCInitialization_and_DataScienceCluster_management_E2E_Tests"},
				{Name: "TestOdhOperator/DSCInitialization_and_DataScienceCluster_management_E2E_Tests/test1"},
			}},
			expected: "",
		},
		{
			name: "mix of service and component tests",
			opts: defaultOpts,
			aggregatedTestResult: &types.TestResult{PassedTest: []types.TestCase{
				{Name: "TestOdhOperator"},
				{Name: "TestOdhOperator/services"},
				{Name: "TestOdhOperator/services/auth"},
				{Name: "TestOdhOperator/services/auth/subtest1"},
				{Name: "TestOdhOperator/services/auth/subtest2"},
				{Name: "TestOdhOperator/components"},
				{Name: "TestOdhOperator/components/dashboard"},
				{Name: "TestOdhOperator/components/dashboard/subtest"},
			}},
			expected: "^TestOdhOperator$/^components$|^TestOdhOperator$/^components$/^dashboard$|^TestOdhOperator$/^services$|^TestOdhOperator$/^services$/^auth$",
		},
		{
			name: "test with special regex characters",
			opts: defaultOpts,
			aggregatedTestResult: &types.TestResult{PassedTest: []types.TestCase{
				{Name: "TestOdhOperator"},
				{Name: "TestOdhOperator/Feature(with)special[chars]"},
			}},
			expected: `^TestOdhOperator$/^Feature\(with\)special\[chars\]$`,
		},
		{
			name: "mix including dsc initialization",
			opts: defaultOpts,
			aggregatedTestResult: &types.TestResult{PassedTest: []types.TestCase{
				{Name: "TestOdhOperator"},
				{Name: "TestOdhOperator/Feature1"},
				{Name: "TestOdhOperator/DSCInitialization_and_DataScienceCluster_management_E2E_Tests"},
				{Name: "TestOdhOperator/DSCInitialization_and_DataScienceCluster_management_E2E_Tests/test1"},
				{Name: "TestOdhOperator/Feature2"},
			}},
			expected: "^TestOdhOperator$/^Feature1$|^TestOdhOperator$/^Feature2$",
		},
		{
			name: "with multiple test cases",
			opts: defaultOpts,
			aggregatedTestResult: &types.TestResult{PassedTest: []types.TestCase{
				{Name: "TestOdhOperator"},
				{Name: "TestOdhOperator/Feature1"},
				{Name: "TestOdhOperator/Feature1/test_case"},
				{Name: "TestOdhOperator/Feature1/test_case/subtest"},
				{Name: "TestOdhOperator/Feature1/test_case/subtest/subsubtest"},
				{Name: "TestOdhOperator/Feature2"},
				{Name: "TestOdhOperator/Feature3"},
			}},
			expected: "^TestOdhOperator$/^Feature1$|^TestOdhOperator$/^Feature2$|^TestOdhOperator$/^Feature3$",
		},
		{
			name: "sibling tests - one passes, one fails - should not skip group",
			opts: defaultOpts,
			aggregatedTestResult: &types.TestResult{
				PassedTest: []types.TestCase{
					{Name: "TestOdhOperator/Feature1/sibling1"},
				},
			},
			// TestOdhOperator and TestOdhOperator/Feature1 not passed, so we don't skip anything
			expected: "",
		},
		{
			name: "sibling tests - both pass - should skip entire group",
			opts: defaultOpts,
			aggregatedTestResult: &types.TestResult{
				PassedTest: []types.TestCase{
					{Name: "TestOdhOperator"},
					{Name: "TestOdhOperator/Feature1"},
					{Name: "TestOdhOperator/Feature1/sibling1"},
					{Name: "TestOdhOperator/Feature1/sibling2"},
				},
			},
			expected: "^TestOdhOperator$/^Feature1$",
		},
		{
			name: "multiple groups with mixed results",
			opts: defaultOpts,
			aggregatedTestResult: &types.TestResult{
				PassedTest: []types.TestCase{
					{Name: "TestOdhOperator/Feature1/sibling1"},
					{Name: "TestOdhOperator/Feature2"},
					{Name: "TestOdhOperator/Feature2/test1"},
					{Name: "TestOdhOperator/Feature2/test2"},
					{Name: "TestOdhOperator/Feature3/testA"},
				},
			},
			// Feature1: has failures, don't skip (TestOdhOperator/Feature1 not present in passed tests)
			// Feature2: all passed, skip (TestOdhOperator/Feature2 present in passed tests)
			// Feature3: has failures, don't skip (TestOdhOperator/Feature3 not present in passed tests)
			expected: "^TestOdhOperator$/^Feature2$",
		},
		{
			name: "service tests with siblings - one passes, one fails",
			opts: defaultOpts,
			aggregatedTestResult: &types.TestResult{
				PassedTest: []types.TestCase{
					{Name: "TestOdhOperator/services/auth/subtest1"},
				},
			},
			// TestOdhOperator and TestOdhOperator/services not passed, so we don't skip anything
			expected: "",
		},
		{
			name: "second retry, aggregated with previous failed tests and failed tests also in last run",
			opts: defaultOpts,
			aggregatedTestResult: &types.TestResult{
				PassedTest: []types.TestCase{
					{Name: "TestOdhOperator/services/auth"},
					{Name: "TestOdhOperator/services/auth/subtest1"},
					{Name: "TestOdhOperator/services/auth/subtest2"},
					{Name: "TestOdhOperator/services/auth/subtest3"},
				},
			},
			// TestOdhOperator/services/auth present in passed tests,
			// but TestOdhOperator and TestOdhOperator/services not passed
			expected: "^TestOdhOperator$/^services$/^auth$",
		},
		{
			name: "with different order tests and fail fast",
			opts: defaultOpts,
			aggregatedTestResult: &types.TestResult{
				PassedTest: []types.TestCase{
					{Name: "TestOdhOperator/components/component1"},                     // first run
					{Name: "TestOdhOperator/components/component_fail_consistently/t1"}, // first run
				},
				// Just to reference the failed tests for a case like this one
				FailedTest: []types.TestCase{
					{Name: "TestOdhOperator"},                                           // first run
					{Name: "TestOdhOperator/components"},                                // first run
					{Name: "TestOdhOperator/components/component_fail_consistently"},    // first run
					{Name: "TestOdhOperator/components/component_fail_consistently/t2"}, // first run
					{Name: "TestOdhOperator"},                                           // second run
					{Name: "TestOdhOperator/components"},                                // second run
					{Name: "TestOdhOperator/components/component3"},                     // second run
				},
			},
			expected: "^TestOdhOperator$/^components$/^component1$",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &E2ETestRunner{
				opts: tt.opts,
			}

			result := runner.buildSkipFilter(tt.aggregatedTestResult)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractTestLevel(t *testing.T) {
	defaultOpts := types.E2ETestOptions{
		NeverSkipPrefixes: []string{"TestOdhOperator/DSCInitialization_and_DataScienceCluster_management_E2E_Tests"},
		SkipAtPrefixes:    []string{"TestOdhOperator/services/", "TestOdhOperator/components/", "TestOdhOperator/"},
	}

	tests := []struct {
		name       string
		opts       types.E2ETestOptions
		testName   string
		wantLevel  string
		shouldSkip bool
	}{
		{
			name:       "dsc initialization test - should not skip",
			opts:       defaultOpts,
			testName:   "TestOdhOperator/DSCInitialization_and_DataScienceCluster_management_E2E_Tests/test1",
			wantLevel:  "",
			shouldSkip: false,
		},
		{
			name:       "dsc initialization test prefix match",
			opts:       defaultOpts,
			testName:   "TestOdhOperator/DSCInitialization_and_DataScienceCluster_management_E2E_Tests",
			wantLevel:  "",
			shouldSkip: false,
		},
		{
			name:       "service test with subtest",
			opts:       defaultOpts,
			testName:   "TestOdhOperator/services/auth/subtest",
			wantLevel:  "TestOdhOperator/services/auth",
			shouldSkip: true,
		},
		{
			name:       "service test without subtest",
			opts:       defaultOpts,
			testName:   "TestOdhOperator/services/auth",
			wantLevel:  "TestOdhOperator/services/auth",
			shouldSkip: true,
		},
		{
			name:       "component test with subtest",
			opts:       defaultOpts,
			testName:   "TestOdhOperator/components/dashboard/subtest",
			wantLevel:  "TestOdhOperator/components/dashboard",
			shouldSkip: true,
		},
		{
			name:       "component test without subtest",
			opts:       defaultOpts,
			testName:   "TestOdhOperator/components/dashboard",
			wantLevel:  "TestOdhOperator/components/dashboard",
			shouldSkip: true,
		},
		{
			name:       "regular two-level test",
			opts:       defaultOpts,
			testName:   "TestOdhOperator/Feature1",
			wantLevel:  "TestOdhOperator/Feature1",
			shouldSkip: true,
		},
		{
			name:       "single level test - should not skip",
			opts:       defaultOpts,
			testName:   "TestOdhOperator",
			wantLevel:  "",
			shouldSkip: false,
		},
		{
			name:       "empty test name",
			opts:       defaultOpts,
			testName:   "",
			wantLevel:  "",
			shouldSkip: false,
		},
		{
			name:       "service test with deep nesting",
			opts:       defaultOpts,
			testName:   "TestOdhOperator/services/auth/subtest1/subtest2",
			wantLevel:  "TestOdhOperator/services/auth",
			shouldSkip: true,
		},
		{
			name:       "component test with deep nesting",
			opts:       defaultOpts,
			testName:   "TestOdhOperator/components/dashboard/subtest1/subtest2",
			wantLevel:  "TestOdhOperator/components/dashboard",
			shouldSkip: true,
		},
		{
			name:       "non-service/component three-level test",
			opts:       defaultOpts,
			testName:   "TestOdhOperator/other/something",
			wantLevel:  "TestOdhOperator/other",
			shouldSkip: true,
		},
		{
			name: "custom skip prefixes - api at 4th level",
			opts: types.E2ETestOptions{
				NeverSkipPrefixes: []string{},
				SkipAtPrefixes:    []string{"TestOdhOperator/api/v1", "TestOdhOperator"},
			},
			testName:   "TestOdhOperator/api/v1/users/get",
			wantLevel:  "TestOdhOperator/api/v1/users",
			shouldSkip: true,
		},
		{
			name: "no skip prefixes configured - should not skip",
			opts: types.E2ETestOptions{
				NeverSkipPrefixes: []string{},
				SkipAtPrefixes:    []string{},
			},
			testName:   "TestOdhOperator/Feature1",
			wantLevel:  "",
			shouldSkip: false,
		},
		{
			name: "longest match wins",
			opts: types.E2ETestOptions{
				NeverSkipPrefixes: []string{},
				SkipAtPrefixes:    []string{"TestOdhOperator/", "TestOdhOperator/services/"},
			},
			testName:   "TestOdhOperator/services/auth/subtest",
			wantLevel:  "TestOdhOperator/services/auth",
			shouldSkip: true,
		},
		{
			name: "prefix without trailing slash - should still work",
			opts: types.E2ETestOptions{
				NeverSkipPrefixes: []string{},
				SkipAtPrefixes:    []string{"TestOdhOperator/services"},
			},
			testName:   "TestOdhOperator/services/auth/subtest",
			wantLevel:  "TestOdhOperator/services/auth",
			shouldSkip: true,
		},
		{
			name: "prevents partial name match - services vs servicesOther",
			opts: types.E2ETestOptions{
				NeverSkipPrefixes: []string{},
				SkipAtPrefixes:    []string{"TestOdhOperator/services/"},
			},
			testName:   "TestOdhOperator/servicesOther/test",
			wantLevel:  "",
			shouldSkip: false,
		},
		{
			name: "never-skip without trailing slash - should still work",
			opts: types.E2ETestOptions{
				NeverSkipPrefixes: []string{"TestOdhOperator/DSCInit"},
				SkipAtPrefixes:    []string{"TestOdhOperator/"},
			},
			testName:   "TestOdhOperator/DSCInit/test1",
			wantLevel:  "",
			shouldSkip: false,
		},
		{
			name: "prevents partial match in never-skip",
			opts: types.E2ETestOptions{
				NeverSkipPrefixes: []string{"TestOdhOperator/DSC/"},
				SkipAtPrefixes:    []string{"TestOdhOperator/"},
			},
			testName:   "TestOdhOperator/DSCOther/test1",
			wantLevel:  "TestOdhOperator/DSCOther",
			shouldSkip: true,
		},
		{
			name: "skip first level test",
			opts: types.E2ETestOptions{
				NeverSkipPrefixes: []string{},
				SkipAtPrefixes:    []string{"TestFirstLevel"},
			},
			testName:   "TestFirstLevel",
			wantLevel:  "TestFirstLevel",
			shouldSkip: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &E2ETestRunner{opts: tt.opts}
			level, shouldSkip := runner.extractTestLevel(tt.testName)
			require.Equal(t, tt.wantLevel, level, "extracted level mismatch")
			require.Equal(t, tt.shouldSkip, shouldSkip, "shouldSkip mismatch")
		})
	}
}

func TestNotifyPROnFailure(t *testing.T) {
	tests := []struct {
		name               string
		opts               types.E2ETestOptions
		mockSetup          func(*mockGitHubClient)
		expectedAddLabel   bool
		expectedAddComment bool
		verbose            bool
	}{
		{
			name: "no token configured - should not notify",
			opts: types.E2ETestOptions{
				Config: &config.Config{Verbose: false},
				PROptions: types.PROptions{
					Token:    "",
					Owner:    "test-owner",
					Repo:     "test-repo",
					PRNumber: 123,
					Label:    "flaky-tests",
					Comment:  "Tests failed",
				},
			},
			expectedAddLabel:   false,
			expectedAddComment: false,
		},
		{
			name: "no owner configured - should not notify",
			opts: types.E2ETestOptions{
				Config: &config.Config{Verbose: false},
				PROptions: types.PROptions{
					Token:    "test-token",
					Owner:    "",
					Repo:     "test-repo",
					PRNumber: 123,
					Label:    "flaky-tests",
					Comment:  "Tests failed",
				},
			},
			expectedAddLabel:   false,
			expectedAddComment: false,
		},
		{
			name: "no repo configured - should not notify",
			opts: types.E2ETestOptions{
				Config: &config.Config{Verbose: false},
				PROptions: types.PROptions{
					Token:    "test-token",
					Owner:    "test-owner",
					Repo:     "",
					PRNumber: 123,
					Label:    "flaky-tests",
					Comment:  "Tests failed",
				},
			},
			expectedAddLabel:   false,
			expectedAddComment: false,
		},
		{
			name: "no PR number configured - should not notify",
			opts: types.E2ETestOptions{
				Config: &config.Config{Verbose: false},
				PROptions: types.PROptions{
					Token:    "test-token",
					Owner:    "test-owner",
					Repo:     "test-repo",
					PRNumber: 0,
					Label:    "flaky-tests",
					Comment:  "Tests failed",
				},
			},
			expectedAddLabel:   false,
			expectedAddComment: false,
		},
		{
			name: "no label or comment configured - should not notify",
			opts: types.E2ETestOptions{
				Config: &config.Config{Verbose: false},
				PROptions: types.PROptions{
					Token:    "test-token",
					Owner:    "test-owner",
					Repo:     "test-repo",
					PRNumber: 123,
					Label:    "",
					Comment:  "",
				},
			},
			expectedAddLabel:   false,
			expectedAddComment: false,
		},
		{
			name: "only label configured - should add label",
			opts: types.E2ETestOptions{
				Config: &config.Config{Verbose: false},
				PROptions: types.PROptions{
					Token:    "test-token",
					Owner:    "test-owner",
					Repo:     "test-repo",
					PRNumber: 123,
					Label:    "flaky-tests",
					Comment:  "",
				},
			},
			expectedAddLabel:   true,
			expectedAddComment: false,
		},
		{
			name: "only comment configured - should add comment",
			opts: types.E2ETestOptions{
				Config: &config.Config{Verbose: false},
				PROptions: types.PROptions{
					Token:    "test-token",
					Owner:    "test-owner",
					Repo:     "test-repo",
					PRNumber: 456,
					Label:    "",
					Comment:  "Tests failed after retries",
				},
			},
			expectedAddLabel:   false,
			expectedAddComment: true,
		},
		{
			name: "both label and comment configured - should add both",
			opts: types.E2ETestOptions{
				Config: &config.Config{Verbose: false},
				PROptions: types.PROptions{
					Token:    "test-token",
					Owner:    "test-owner",
					Repo:     "test-repo",
					PRNumber: 789,
					Label:    "needs-attention",
					Comment:  "Some tests failed",
				},
			},
			expectedAddLabel:   true,
			expectedAddComment: true,
		},
		{
			name: "label fails - should continue and try comment",
			opts: types.E2ETestOptions{
				Config: &config.Config{Verbose: false},
				PROptions: types.PROptions{
					Token:    "test-token",
					Owner:    "test-owner",
					Repo:     "test-repo",
					PRNumber: 999,
					Label:    "flaky-tests",
					Comment:  "Tests failed",
				},
			},
			mockSetup: func(m *mockGitHubClient) {
				m.addLabelError = fmt.Errorf("API rate limit exceeded")
			},
			expectedAddLabel:   true,
			expectedAddComment: true,
		},
		{
			name: "comment fails - should not affect label",
			opts: types.E2ETestOptions{
				Config: &config.Config{Verbose: false},
				PROptions: types.PROptions{
					Token:    "test-token",
					Owner:    "test-owner",
					Repo:     "test-repo",
					PRNumber: 888,
					Label:    "flaky-tests",
					Comment:  "Tests failed",
				},
			},
			mockSetup: func(m *mockGitHubClient) {
				m.addCommentError = fmt.Errorf("permission denied")
			},
			expectedAddLabel:   true,
			expectedAddComment: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockGitHubClient{}
			if tt.mockSetup != nil {
				tt.mockSetup(mockClient)
			}

			runner := &E2ETestRunner{
				opts:         tt.opts,
				githubClient: mockClient,
			}

			runner.notifyPROnFailure()

			if tt.expectedAddLabel {
				require.True(t, mockClient.addLabelCalled, "expected AddLabel to be called")
				require.Equal(t, tt.opts.PROptions.Owner, mockClient.lastOwner)
				require.Equal(t, tt.opts.PROptions.Repo, mockClient.lastRepo)
				require.Equal(t, tt.opts.PROptions.PRNumber, mockClient.lastPRNumber)
				require.Equal(t, tt.opts.PROptions.Label, mockClient.lastLabel)
			} else {
				require.False(t, mockClient.addLabelCalled, "expected AddLabel not to be called")
			}

			if tt.expectedAddComment {
				require.True(t, mockClient.addCommentCalled, "expected AddComment to be called")
				require.Equal(t, tt.opts.PROptions.Owner, mockClient.lastOwner)
				require.Equal(t, tt.opts.PROptions.Repo, mockClient.lastRepo)
				require.Equal(t, tt.opts.PROptions.PRNumber, mockClient.lastPRNumber)
				require.Equal(t, tt.opts.PROptions.Comment, mockClient.lastComment)
			} else {
				require.False(t, mockClient.addCommentCalled, "expected AddComment not to be called")
			}
		})
	}
}

// mockGitHubClient is a mock implementation of GitHubClient for testing
type mockGitHubClient struct {
	addLabelCalled   bool
	addCommentCalled bool
	addLabelError    error
	addCommentError  error
	lastOwner        string
	lastRepo         string
	lastPRNumber     int
	lastLabel        string
	lastComment      string
}

func (m *mockGitHubClient) AddLabel(ctx context.Context, owner, repo string, prNumber int, label string) error {
	m.addLabelCalled = true
	m.lastOwner = owner
	m.lastRepo = repo
	m.lastPRNumber = prNumber
	m.lastLabel = label
	return m.addLabelError
}

func (m *mockGitHubClient) AddComment(ctx context.Context, owner, repo string, prNumber int, comment string) error {
	m.addCommentCalled = true
	m.lastOwner = owner
	m.lastRepo = repo
	m.lastPRNumber = prNumber
	m.lastComment = comment
	return m.addCommentError
}

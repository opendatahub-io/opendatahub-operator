package formatter

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/types"
)

func TestExportToJUnit(t *testing.T) {
	tests := []struct {
		name        string
		result      *types.TestResult
		outputPath  string
		wantErr     bool
		errContains string
		verifySuite func(t *testing.T, suite TestSuite)
	}{
		{
			name: "empty results",
			result: &types.TestResult{
				PassedTest: []types.TestCase{},
				FailedTest: []types.TestCase{},
			},
			verifySuite: func(t *testing.T, suite TestSuite) {
				t.Helper()

				require.Equal(t, t.Name(), suite.Name)
				require.Equal(t, 0, suite.Tests)
				require.Equal(t, 0, suite.Failures)
				require.Empty(t, suite.TestCases)
			},
		},
		{
			name: "passed tests",
			result: &types.TestResult{
				PassedTest: []types.TestCase{
					{
						ID:       1,
						Name:     "TestExample/test_case_1",
						Package:  "example.com/pkg",
						Duration: 1500 * time.Millisecond,
					},
					{
						ID:       2,
						Name:     "TestExample/test_case_2",
						Package:  "example.com/pkg",
						Duration: 2500 * time.Millisecond,
					},
				},
				FailedTest: []types.TestCase{},
			},
			verifySuite: func(t *testing.T, suite TestSuite) {
				t.Helper()

				require.Equal(t, t.Name(), suite.Name)
				require.Equal(t, 2, suite.Tests)
				require.Equal(t, 0, suite.Failures)
				require.Len(t, suite.TestCases, 2)

				require.Equal(t, []TestCase{
					{
						Name:     "TestExample/test_case_1",
						Duration: "1.500",
					},
					{
						Name:     "TestExample/test_case_2",
						Duration: "2.500",
					},
				}, suite.TestCases)
			},
		},
		{
			name: "correct sort order",
			result: &types.TestResult{
				PassedTest: []types.TestCase{
					{
						ID:       1,
						Name:     "TestExample/test_case_1",
						Package:  "example.com/pkg",
						Duration: 1500 * time.Millisecond,
						Time:     time.Now().Add(-1 * time.Second),
					},
					{
						ID:       2,
						Name:     "TestExample/test_case_2",
						Package:  "example.com/pkg",
						Duration: 2500 * time.Millisecond,
						Time:     time.Now(),
					},
				},
				FailedTest: []types.TestCase{},
			},
			verifySuite: func(t *testing.T, suite TestSuite) {
				t.Helper()

				require.Equal(t, t.Name(), suite.Name)
				require.Equal(t, 2, suite.Tests)
				require.Equal(t, 0, suite.Failures)
				require.Len(t, suite.TestCases, 2)

				require.Equal(t, []TestCase{
					{
						Name:     "TestExample/test_case_1",
						Duration: "1.500",
					},
					{
						Name:     "TestExample/test_case_2",
						Duration: "2.500",
					},
				}, suite.TestCases)
			},
		},
		{
			name: "failed tests",
			result: &types.TestResult{
				PassedTest: []types.TestCase{},
				FailedTest: []types.TestCase{
					{
						ID:            1,
						Name:          "TestExample/test_case_fail",
						Package:       "example.com/pkg",
						Duration:      1000 * time.Millisecond,
						FailureOutput: "Test failed with this error output",
					},
				},
			},
			verifySuite: func(t *testing.T, suite TestSuite) {
				t.Helper()

				require.Equal(t, t.Name(), suite.Name)
				require.Equal(t, 1, suite.Tests)
				require.Equal(t, 1, suite.Failures)
				require.Len(t, suite.TestCases, 1)

				require.Equal(t, []TestCase{
					{
						Name:     "TestExample/test_case_fail",
						Duration: "1.000",
						Failure: &Failure{
							Message: "Test TestExample/test_case_fail failed",
							Content: "Test failed with this error output",
						},
					},
				}, suite.TestCases)
			},
		},
		{
			name: "flaky tests",
			result: &types.TestResult{
				FailedTest: []types.TestCase{
					{
						ID:            1,
						Name:          "TestFlaky/flaky_test",
						Package:       "example.com/pkg",
						Duration:      1000 * time.Millisecond,
						Time:          time.Now().Add(-10 * time.Second),
						FailureOutput: "Test failed first time with this error output",
					},
					{
						ID:            1,
						Name:          "TestFlaky/flaky_test",
						Package:       "example.com/pkg",
						Duration:      1100 * time.Millisecond,
						Time:          time.Now().Add(-5 * time.Second),
						FailureOutput: "Test failed second time with this error output",
					},
				},
				PassedTest: []types.TestCase{
					{
						ID:       1,
						Name:     "TestFlaky/flaky_test",
						Package:  "example.com/pkg",
						Duration: 1200 * time.Millisecond,
						Time:     time.Now(),
					},
					{
						ID:       2,
						Name:     "TestStable/stable_test",
						Package:  "example.com/pkg",
						Duration: 500 * time.Millisecond,
						Time:     time.Now().Add(-20 * time.Second),
					},
				},
			},
			verifySuite: func(t *testing.T, suite TestSuite) {
				t.Helper()

				require.Equal(t, t.Name(), suite.Name)
				require.Equal(t, 4, suite.Tests)
				require.Equal(t, 2, suite.Failures)
				require.Len(t, suite.TestCases, 4)

				require.Equal(t, []TestCase{
					{
						Name:     "TestStable/stable_test",
						Duration: "0.500",
					},
					{
						Name:     "TestFlaky/flaky_test",
						Duration: "1.000",
						Failure: &Failure{
							Message: "Test TestFlaky/flaky_test failed",
							Content: "Test failed first time with this error output",
						},
					},
					{
						Name:     "TestFlaky/flaky_test",
						Duration: "1.100",
						Failure: &Failure{
							Message: "Test TestFlaky/flaky_test failed",
							Content: "Test failed second time with this error output",
						},
					},
					{
						Name:     "TestFlaky/flaky_test",
						Duration: "1.200",
					},
				}, suite.TestCases)
			},
		},
		{
			name: "special characters",
			result: &types.TestResult{
				PassedTest: []types.TestCase{
					{
						ID:       1,
						Name:     "TestSpecial/test_with_<special>&characters\"",
						Package:  "example.com/pkg",
						Duration: 100 * time.Millisecond,
					},
				},
				FailedTest: []types.TestCase{},
			},
			verifySuite: func(t *testing.T, suite TestSuite) {
				t.Helper()

				require.Equal(t, "TestSpecial/test_with_<special>&characters\"", suite.TestCases[0].Name)
			},
		},
		{
			name: "no output path",
			result: &types.TestResult{
				PassedTest: []types.TestCase{},
				FailedTest: []types.TestCase{},
			},
			outputPath:  "",
			wantErr:     true,
			errContains: "output path is required",
		},
		{
			name: "failed test with infrastructure classification",
			result: &types.TestResult{
				PassedTest: []types.TestCase{},
				FailedTest: []types.TestCase{
					{
						ID:            1,
						Name:          "TestDashboard/basic_test",
						Package:       "example.com/e2e",
						Duration:      45 * time.Second,
						FailureOutput: "Dashboard not ready",
						Time:          time.Now(),
						Classification: &types.FailureClassification{
							Category:    "infrastructure",
							Subcategory: "image-pull",
							ErrorCode:   1001,
							Evidence:    []string{"container dashboard/oauth-proxy waiting: ImagePullBackOff", "event Pod/dashboard: BackOff"},
							Confidence:  "medium",
						},
					},
				},
			},
			verifySuite: func(t *testing.T, suite TestSuite) {
				require.Equal(t, 1, suite.Tests)
				require.Equal(t, 1, suite.Failures)
				require.Len(t, suite.TestCases, 1)

				tc := suite.TestCases[0]
				require.Equal(t, "TestDashboard/basic_test", tc.Name)
				require.NotNil(t, tc.Failure)
				require.NotNil(t, tc.Properties)
				require.Len(t, tc.Properties.Property, 5)

				// Build map for easier assertions
				props := make(map[string]string)
				for _, p := range tc.Properties.Property {
					props[p.Name] = p.Value
				}

				require.Equal(t, "infrastructure", props["failure.category"])
				require.Equal(t, "image-pull", props["failure.subcategory"])
				require.Equal(t, "1001", props["failure.error_code"])
				require.Equal(t, "medium", props["failure.confidence"])
				require.Equal(t, "container dashboard/oauth-proxy waiting: ImagePullBackOff; event Pod/dashboard: BackOff", props["failure.evidence"])
			},
		},
		{
			name: "failed test with test classification",
			result: &types.TestResult{
				FailedTest: []types.TestCase{
					{
						Name:     "TestModelMesh/deployment",
						Duration: 30 * time.Second,
						Classification: &types.FailureClassification{
							Category:    "test",
							Subcategory: "test-failure",
							ErrorCode:   2001,
							Evidence:    []string{"cluster appears healthy, failure is test-related"},
							Confidence:  "medium",
						},
					},
				},
			},
			verifySuite: func(t *testing.T, suite TestSuite) {
				tc := suite.TestCases[0]
				require.NotNil(t, tc.Properties)

				props := make(map[string]string)
				for _, p := range tc.Properties.Property {
					props[p.Name] = p.Value
				}

				require.Equal(t, "test", props["failure.category"])
				require.Equal(t, "test-failure", props["failure.subcategory"])
				require.Equal(t, "2001", props["failure.error_code"])
			},
		},
		{
			name: "failed test without classification",
			result: &types.TestResult{
				FailedTest: []types.TestCase{
					{
						Name:           "TestOldTest",
						Duration:       10 * time.Second,
						Classification: nil,
					},
				},
			},
			verifySuite: func(t *testing.T, suite TestSuite) {
				tc := suite.TestCases[0]
				require.Nil(t, tc.Properties) // No properties when no classification
			},
		},
		{
			name: "multiple retries with different classifications",
			result: &types.TestResult{
				FailedTest: []types.TestCase{
					{
						Name:     "TestFlaky",
						Duration: 20 * time.Second,
						Time:     time.Now().Add(-10 * time.Second),
						Classification: &types.FailureClassification{
							Category:    "infrastructure",
							Subcategory: "image-pull",
							ErrorCode:   1001,
							Evidence:    []string{"ImagePullBackOff"},
							Confidence:  "medium",
						},
					},
					{
						Name:     "TestFlaky",
						Duration: 25 * time.Second,
						Time:     time.Now().Add(-5 * time.Second),
						Classification: &types.FailureClassification{
							Category:    "infrastructure",
							Subcategory: "pod-startup",
							ErrorCode:   1002,
							Evidence:    []string{"CrashLoopBackOff"},
							Confidence:  "high",
						},
					},
				},
				PassedTest: []types.TestCase{
					{
						Name:     "TestFlaky",
						Duration: 22 * time.Second,
						Time:     time.Now(),
					},
				},
			},
			verifySuite: func(t *testing.T, suite TestSuite) {
				require.Equal(t, 3, suite.Tests)
				require.Equal(t, 2, suite.Failures)

				// First failure: image-pull
				props1 := make(map[string]string)
				for _, p := range suite.TestCases[0].Properties.Property {
					props1[p.Name] = p.Value
				}
				require.Equal(t, "image-pull", props1["failure.subcategory"])
				require.Equal(t, "1001", props1["failure.error_code"])

				// Second failure: pod-startup
				props2 := make(map[string]string)
				for _, p := range suite.TestCases[1].Properties.Property {
					props2[p.Name] = p.Value
				}
				require.Equal(t, "pod-startup", props2["failure.subcategory"])
				require.Equal(t, "1002", props2["failure.error_code"])

				// Third (passed): no properties
				require.Nil(t, suite.TestCases[2].Properties)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			outputPath := tt.outputPath
			if outputPath == "" && !tt.wantErr {
				outputPath = filepath.Join(tempDir, "junit.xml")
			}

			opts := JUnitExportOptions{
				OutputPath: outputPath,
				SuiteName:  t.Name(),
			}

			err := ExportToJUnit(tt.result, opts)

			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)

			if tt.verifySuite != nil {
				suite := readJUnitFile(t, outputPath)
				tt.verifySuite(t, suite)
			}
		})
	}
}

func readJUnitFile(t *testing.T, path string) TestSuite {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var suite TestSuite
	err = xml.Unmarshal(data, &suite)
	require.NoError(t, err)

	return suite
}

package parser

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/snapshot"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/types"
)

func TestParseGoTestJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *types.TestResult
		wantErr  bool
	}{
		{
			name: "single passing test",
			input: `{"Time":"2023-01-01T00:00:00Z","Action":"run","Package":"example.com/pkg","Test":"TestExample"}
{"Time":"2023-01-01T00:00:01Z","Action":"output","Package":"example.com/pkg","Test":"TestExample","Output":"=== RUN   TestExample\n"}
{"Time":"2023-01-01T00:00:02Z","Action":"output","Package":"example.com/pkg","Test":"TestExample","Output":"--- PASS: TestExample (1.00s)\n"}
{"Time":"2023-01-01T00:00:02Z","Action":"pass","Package":"example.com/pkg","Test":"TestExample","Elapsed":1.0}`,
			expected: &types.TestResult{
				PassedTest: []types.TestCase{
					{
						ID:       1,
						Package:  "example.com/pkg",
						Name:     "TestExample",
						Duration: 1 * time.Second,
						Time:     time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
					},
				},
				FailedTest: []types.TestCase{},
			},
		},
		{
			name: "single failing test",
			input: `{"Time":"2023-01-01T00:00:00Z","Action":"run","Package":"example.com/pkg","Test":"TestFailing"}
			{"Time":"2023-01-01T00:00:01Z","Action":"output","Package":"example.com/pkg","Test":"TestFailing","Output":"=== RUN   TestFailing\n"}
			{"Time":"2023-01-01T00:00:02Z","Action":"output","Package":"example.com/pkg","Test":"TestFailing","Output":"    failing_test.go:10: assertion failed\n"}
			{"Time":"2023-01-01T00:00:02Z","Action":"output","Package":"example.com/pkg","Test":"TestFailing","Output":"--- FAIL: TestFailing (0.50s)\n"}
			{"Time":"2023-01-01T00:00:02Z","Action":"fail","Package":"example.com/pkg","Test":"TestFailing","Elapsed":0.5}`,
			expected: &types.TestResult{
				PassedTest: []types.TestCase{},
				FailedTest: []types.TestCase{
					{
						ID:            1,
						Package:       "example.com/pkg",
						Name:          "TestFailing",
						FailureOutput: "=== RUN   TestFailing\n    failing_test.go:10: assertion failed\n--- FAIL: TestFailing (0.50s)\n",
						Duration:      500 * time.Millisecond,
						Time:          time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseGoTestJSON(ParseConfig{
				Stdout: bytes.NewBuffer([]byte(tt.input)),
			})

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tt.expected, result)
		})
	}
}

func TestParseGoTestJSONRealFixtures(t *testing.T) {
	snapshotTester := snapshot.New(t)

	tests := []struct {
		name        string
		fixturePath string
		expected    *types.TestResult
		wantErr     bool
	}{
		{
			name:        "single test enabled",
			fixturePath: "testdata/e2e-single.txt",
		},
		{
			name:        "components and v2tov3 test enabled",
			fixturePath: "testdata/e2e-components.txt",
		},
		{
			name:        "not exists folder",
			fixturePath: "testdata/not-exists.txt",
			wantErr:     true,
		},
		{
			name:        "failing test",
			fixturePath: "testdata/failing-test.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, err := os.ReadFile(tt.fixturePath)
			require.NoError(t, err)

			result, err := ParseGoTestJSON(ParseConfig{
				Stdout: bytes.NewBuffer(input),
			})

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				snapshotTester.MatchSnapshot(t, result)
			}
		})
	}
}

func TestParseClassificationLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    *types.FailureClassification
		wantErr bool
	}{
		{
			name: "infrastructure classification",
			line: `FAILURE_CLASSIFICATION: {"category":"infrastructure","subcategory":"image-pull","error_code":1001,"evidence":["container dashboard/oauth-proxy waiting: ImagePullBackOff"],"confidence":"medium"}`,
			want: &types.FailureClassification{
				Category:    "infrastructure",
				Subcategory: "image-pull",
				ErrorCode:   1001,
				Evidence:    []string{"container dashboard/oauth-proxy waiting: ImagePullBackOff"},
				Confidence:  "medium",
			},
			wantErr: false,
		},
		{
			name: "test classification",
			line: `FAILURE_CLASSIFICATION: {"category":"test","subcategory":"test-failure","error_code":2001,"evidence":["cluster appears healthy, failure is test-related"],"confidence":"medium"}`,
			want: &types.FailureClassification{
				Category:    "test",
				Subcategory: "test-failure",
				ErrorCode:   2001,
				Evidence:    []string{"cluster appears healthy, failure is test-related"},
				Confidence:  "medium",
			},
			wantErr: false,
		},
		{
			name: "unknown classification",
			line: `FAILURE_CLASSIFICATION: {"category":"unknown","subcategory":"unclassifiable","error_code":3000,"evidence":["no matching classification rule"],"confidence":"low"}`,
			want: &types.FailureClassification{
				Category:    "unknown",
				Subcategory: "unclassifiable",
				ErrorCode:   3000,
				Evidence:    []string{"no matching classification rule"},
				Confidence:  "low",
			},
			wantErr: false,
		},
		{
			name: "multiple evidence items",
			line: `FAILURE_CLASSIFICATION: {"category":"infrastructure","subcategory":"pod-startup","error_code":1002,"evidence":["container foo waiting: CrashLoopBackOff","container bar terminated: Error"],"confidence":"high"}`,
			want: &types.FailureClassification{
				Category:    "infrastructure",
				Subcategory: "pod-startup",
				ErrorCode:   1002,
				Evidence:    []string{"container foo waiting: CrashLoopBackOff", "container bar terminated: Error"},
				Confidence:  "high",
			},
			wantErr: false,
		},
		{
			name:    "not a classification line",
			line:    "=== RUN   TestDashboard",
			want:    nil,
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			line:    `FAILURE_CLASSIFICATION: {invalid json}`,
			want:    nil,
			wantErr: true,
		},
		{
			name: "empty evidence array",
			line: `FAILURE_CLASSIFICATION: {"category":"infrastructure","subcategory":"network","error_code":1003,"evidence":[],"confidence":"low"}`,
			want: &types.FailureClassification{
				Category:    "infrastructure",
				Subcategory: "network",
				ErrorCode:   1003,
				Evidence:    []string{},
				Confidence:  "low",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseClassificationLine(tt.line)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseGoTestJSON_WithClassification(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *types.TestResult
	}{
		{
			name: "failed test with infrastructure classification",
			input: `{"Time":"2026-03-09T10:00:00Z","Action":"run","Package":"example.com/e2e","Test":"TestDashboard"}
{"Time":"2026-03-09T10:00:01Z","Action":"output","Package":"example.com/e2e","Test":"TestDashboard","Output":"=== RUN   TestDashboard\n"}
{"Time":"2026-03-09T10:00:45Z","Action":"output","Package":"example.com/e2e","Test":"TestDashboard","Output":"    dashboard_test.go:123: Dashboard not ready\n"}
{"Time":"2026-03-09T10:00:45.1Z","Action":"output","Package":"example.com/e2e","Test":"TestDashboard","Output":"FAILURE_CLASSIFICATION: {\"category\":\"infrastructure\",\"subcategory\":\"image-pull\",\"error_code\":1001,\"evidence\":[\"container dashboard/oauth-proxy waiting: ImagePullBackOff\"],\"confidence\":\"medium\"}\n"}
{"Time":"2026-03-09T10:00:45.2Z","Action":"fail","Package":"example.com/e2e","Test":"TestDashboard","Elapsed":45.2}`,
			expected: &types.TestResult{
				PassedTest: []types.TestCase{},
				FailedTest: []types.TestCase{
					{
						ID:            1,
						Package:       "example.com/e2e",
						Name:          "TestDashboard",
						Duration:      45200 * time.Millisecond,
						FailureOutput: "=== RUN   TestDashboard\n    dashboard_test.go:123: Dashboard not ready\nFAILURE_CLASSIFICATION: {\"category\":\"infrastructure\",\"subcategory\":\"image-pull\",\"error_code\":1001,\"evidence\":[\"container dashboard/oauth-proxy waiting: ImagePullBackOff\"],\"confidence\":\"medium\"}\n",
						Time:          time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC),
						Classification: &types.FailureClassification{
							Category:    "infrastructure",
							Subcategory: "image-pull",
							ErrorCode:   1001,
							Evidence:    []string{"container dashboard/oauth-proxy waiting: ImagePullBackOff"},
							Confidence:  "medium",
						},
					},
				},
			},
		},
		{
			name: "failed test with test classification",
			input: `{"Time":"2026-03-09T10:00:00Z","Action":"run","Package":"example.com/e2e","Test":"TestModelMesh"}
{"Time":"2026-03-09T10:00:01Z","Action":"output","Package":"example.com/e2e","Test":"TestModelMesh","Output":"=== RUN   TestModelMesh\n"}
{"Time":"2026-03-09T10:00:10Z","Action":"output","Package":"example.com/e2e","Test":"TestModelMesh","Output":"    modelmesh_test.go:89: Assertion failed\n"}
{"Time":"2026-03-09T10:00:10.1Z","Action":"output","Package":"example.com/e2e","Test":"TestModelMesh","Output":"FAILURE_CLASSIFICATION: {\"category\":\"test\",\"subcategory\":\"test-failure\",\"error_code\":2001,\"evidence\":[\"cluster appears healthy\"],\"confidence\":\"medium\"}\n"}
{"Time":"2026-03-09T10:00:10.2Z","Action":"fail","Package":"example.com/e2e","Test":"TestModelMesh","Elapsed":10.2}`,
			expected: &types.TestResult{
				PassedTest: []types.TestCase{},
				FailedTest: []types.TestCase{
					{
						ID:            1,
						Package:       "example.com/e2e",
						Name:          "TestModelMesh",
						Duration:      10200 * time.Millisecond,
						FailureOutput: "=== RUN   TestModelMesh\n    modelmesh_test.go:89: Assertion failed\nFAILURE_CLASSIFICATION: {\"category\":\"test\",\"subcategory\":\"test-failure\",\"error_code\":2001,\"evidence\":[\"cluster appears healthy\"],\"confidence\":\"medium\"}\n",
						Time:          time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC),
						Classification: &types.FailureClassification{
							Category:    "test",
							Subcategory: "test-failure",
							ErrorCode:   2001,
							Evidence:    []string{"cluster appears healthy"},
							Confidence:  "medium",
						},
					},
				},
			},
		},
		{
			name: "failed test without classification",
			input: `{"Time":"2026-03-09T10:00:00Z","Action":"run","Package":"example.com/e2e","Test":"TestOldTest"}
{"Time":"2026-03-09T10:00:01Z","Action":"output","Package":"example.com/e2e","Test":"TestOldTest","Output":"=== RUN   TestOldTest\n"}
{"Time":"2026-03-09T10:00:05Z","Action":"output","Package":"example.com/e2e","Test":"TestOldTest","Output":"    old_test.go:50: Failed\n"}
{"Time":"2026-03-09T10:00:05.1Z","Action":"fail","Package":"example.com/e2e","Test":"TestOldTest","Elapsed":5.1}`,
			expected: &types.TestResult{
				PassedTest: []types.TestCase{},
				FailedTest: []types.TestCase{
					{
						ID:             1,
						Package:        "example.com/e2e",
						Name:           "TestOldTest",
						Duration:       5100 * time.Millisecond,
						FailureOutput:  "=== RUN   TestOldTest\n    old_test.go:50: Failed\n",
						Time:           time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC),
						Classification: nil, // No classification for this test
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseGoTestJSON(ParseConfig{
				Stdout: bytes.NewBuffer([]byte(tt.input)),
			})

			require.NoError(t, err)
			require.Equal(t, tt.expected, result)
		})
	}
}

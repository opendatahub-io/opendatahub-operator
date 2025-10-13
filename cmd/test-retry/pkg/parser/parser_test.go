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

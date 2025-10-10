package parser

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/types"

	"gotest.tools/gotestsum/testjson"
)

type ParseConfig struct {
	Stdout io.Reader
	Stderr io.Reader
}

// ParseGoTestJSON parses go test -json output using testjson library
func ParseGoTestJSON(cfg ParseConfig) (*types.TestResult, error) {
	formatter := testjson.NewEventFormatter(os.Stdout, "standard-verbose", testjson.FormatOptions{})
	handler := &eventHandler{
		formatter: formatter,
	}

	// Use ScanTestOutput to parse and get execution results
	execution, err := testjson.ScanTestOutput(testjson.ScanConfig{
		Stdout:                   cfg.Stdout,
		Stderr:                   cfg.Stderr,
		IgnoreNonJSONOutputLines: true,
		Handler:                  handler,
	})
	if err != nil {
		return nil, fmt.Errorf("error scanning test output: %w", err)
	}

	if len(execution.Errors()) > 0 {
		return nil, fmt.Errorf("errors found in execution: %v", execution.Errors())
	}

	testResult := &types.TestResult{
		PassedTest: make([]types.TestCase, 0),
		FailedTest: make([]types.TestCase, 0),
	}

	packages := execution.Packages()
	for _, pkg := range packages {
		pkg := execution.Package(pkg)

		if len(pkg.TestCases()) == 0 && pkg.Result() == testjson.ActionFail {
			return nil, fmt.Errorf("package failed")
		}

		for _, test := range pkg.Failed {
			testResult.FailedTest = append(testResult.FailedTest, types.TestCase{
				ID:            test.ID,
				Name:          test.Test.Name(),
				Package:       test.Package,
				Duration:      test.Elapsed,
				FailureOutput: strings.Join(pkg.OutputLines(test), ""),
				Time:          test.Time,
			})
		}

		for _, test := range pkg.Passed {
			testResult.PassedTest = append(testResult.PassedTest, types.TestCase{
				ID:       test.ID,
				Name:     test.Test.Name(),
				Package:  test.Package,
				Duration: test.Elapsed,
				Time:     test.Time,
			})
		}
	}

	return testResult, nil
}

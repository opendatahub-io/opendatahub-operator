# Test Retry CLI

A Go CLI tool for running tests with automatic retry functionality. It tracks which tests pass and skips them in subsequent retries using the `-skip` flag, while continuing to retry only the tests that are still failing, making test execution more efficient and reliable.

## Features

- **Automatic Retry**: Automatically retries failed tests up to a configurable number of times
- **Smart Test Skipping**: Tracks which tests pass and skips them in subsequent retries using the `-skip` flag
- **Parallel Execution**: Supports parallel test execution for faster results
- **Detailed Output**: Provides verbose output with test results and retry attempts
- **Flexible Configuration**: Configurable retry count, timeouts, and test filters

## Installation

```bash
# Build the CLI
cd cmd/test-retry
go build -o test-retry .
```

## Usage

### Basic Commands

```bash
# Run e2e tests with retry
./test-retry e2e
```

### Advanced Options

```bash
# Enable verbose output
./test-retry e2e --verbose

# Filter specific tests
./test-retry e2e --filter "^TestOdhOperator.*Dashboard"

# Run with custom test runner options
./test-retry e2e -- "-count=3 -timeout=30m"
```

### Configuration Options

#### Global Flags
- `--verbose`: Enable verbose output

#### E2E Test Flags
- `--filter`: Filter tests to run using regex pattern (default: "^TestOdhOperator/")
- `--path`: Path to e2e tests (default: "./tests/e2e/")
- `--working-dir`: Working directory for running go test (default: current directory)
- `--never-skip`: Test prefixes that should never be skipped (default: ["TestOdhOperator/DSCInitialization_and_DataScienceCluster_management_E2E_Tests/"])
- `--skip-at-prefix`: Test prefixes where tests should be extracted at prefix + 1 level (default: ["TestOdhOperator/services/", "TestOdhOperator/components/", "TestOdhOperator/"])
- `--junit-output`: Path to JUnit XML output file (optional)
- `--github-token`: GitHub token for authentication (can also use GITHUB_TOKEN env var)
- `--github-owner`: GitHub repository owner
- `--github-repo`: GitHub repository name
- `--github-pr`: GitHub pull request number to notify on test failures
- `--failure-label`: Label to add to PR when tests fail (optional)
- `--failure-comment`: Comment to add to PR when tests fail (optional)

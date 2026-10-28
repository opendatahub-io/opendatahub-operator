#!/bin/bash
set -euo pipefail

# Validation functions
validate_bool() {
    local var_name=$1
    local value=${!var_name}
    case "$value" in
        true|false|0|1) return 0 ;;
        *) echo "Error: $var_name must be true/false or 0/1, got '$value'" >&2; exit 1 ;;
    esac
}

validate_namespace() {
    local var_name=$1
    local value=${!var_name}
    # K8s namespace regex: lowercase alphanumeric characters or '-', must start and end with alphanumeric
    if [[ ! "$value" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]]; then
        echo "Error: $var_name must be a valid Kubernetes namespace, got '$value'" >&2; exit 1
    fi
}

validate_deletion_policy() {
    local var_name=$1
    local value=${!var_name}
    case "$value" in
        never|always|on-failure) return 0 ;;
        *) echo "Error: $var_name must be 'never', 'always', or 'on-failure', got '$value'" >&2; exit 1 ;;
    esac
}

validate_tag() {
    local var_name=$1
    local value=${!var_name}
    # Tags limited to All, Smoke, Tier1, Tier2 and Tier3
    case "$value" in
        All|Smoke|Tier1|Tier2|Tier3) return 0 ;;
        *) echo "Error: $var_name must be 'All', 'Smoke', 'Tier1', 'Tier2' or 'Tier3', got '$value'" >&2; exit 1 ;;
    esac
}

# Set defaults and validate
: "${E2E_TEST_DELETION_POLICY:=never}"
validate_deletion_policy E2E_TEST_DELETION_POLICY

: "${E2E_TEST_CLEAN_UP_PREVIOUS_RESOURCES:=false}"
validate_bool E2E_TEST_CLEAN_UP_PREVIOUS_RESOURCES

: "${E2E_TEST_BACKUP_AND_RESTORE_DSCI_AND_DSC:=true}"
validate_bool E2E_TEST_BACKUP_AND_RESTORE_DSCI_AND_DSC

: "${E2E_TEST_OPERATOR_CONTROLLER:=true}"
validate_bool E2E_TEST_OPERATOR_CONTROLLER

: "${E2E_TEST_DEPENDANT_OPERATORS_MANAGEMENT:=false}"
validate_bool E2E_TEST_DEPENDANT_OPERATORS_MANAGEMENT

: "${E2E_TEST_DSC_MANAGEMENT:=false}"
validate_bool E2E_TEST_DSC_MANAGEMENT

: "${E2E_TEST_OPERATOR_RESILIENCE:=true}"
validate_bool E2E_TEST_OPERATOR_RESILIENCE

: "${E2E_TEST_OPERATOR_V2TOV3UPGRADE:=true}"
validate_bool E2E_TEST_OPERATOR_V2TOV3UPGRADE


: "${E2E_TEST_CONVERSION_WEBHOOK:=true}"
validate_bool E2E_TEST_CONVERSION_WEBHOOK

: "${E2E_TEST_CONVERSION_WEBHOOK_DSC:=true}"
validate_bool E2E_TEST_CONVERSION_WEBHOOK_DSC

: "${E2E_TEST_CONVERSION_WEBHOOK_PLATFORM:=true}"
validate_bool E2E_TEST_CONVERSION_WEBHOOK_PLATFORM


: "${E2E_TEST_SERVICES:=true}"
validate_bool E2E_TEST_SERVICES

: "${E2E_TEST_OPERATOR_NAMESPACE:=opendatahub-operators}"
validate_namespace E2E_TEST_OPERATOR_NAMESPACE

: "${E2E_TEST_APPLICATIONS_NAMESPACE:=opendatahub}"
validate_namespace E2E_TEST_APPLICATIONS_NAMESPACE

# Legacy DSC workbenchNamespace (not operand deploy target). Makefile sets
# WORKBENCHES_NAMESPACE per flavor (ODH: opendatahub, RHOAI: rhods-notebooks) and
# exports E2E_TEST_WORKBENCHES_NAMESPACE before invoking this script.
: "${E2E_TEST_WORKBENCHES_NAMESPACE:=${WORKBENCHES_NAMESPACE:-}}"
if [ -z "$E2E_TEST_WORKBENCHES_NAMESPACE" ]; then
    echo "Error: E2E_TEST_WORKBENCHES_NAMESPACE or WORKBENCHES_NAMESPACE must be set (make e2e-test exports both from Makefile)" >&2
    exit 1
fi
validate_namespace E2E_TEST_WORKBENCHES_NAMESPACE

: "${E2E_TEST_DSC_MONITORING_NAMESPACE:=opendatahub}"
validate_namespace E2E_TEST_DSC_MONITORING_NAMESPACE

: "${E2E_TEST_TAG:=All}"
validate_tag E2E_TEST_TAG

: "${E2E_TEST_TIMEOUT:=110m}"

# Toggle for JUnit XML enrichment with failure classification (default: disabled)
: "${USE_TEST_RETRY:=false}"
validate_bool USE_TEST_RETRY

# Label to apply to PRs when tests are flaky (pass on retry)
: "${E2E_FLAKY_LABEL:=ci/flaky-test}"

# Build GitHub PR notification flags when PULL_NUMBER and GITHUB_TOKEN are set
# (both are injected by Prow into presubmit jobs)
GITHUB_PR_FLAGS=""
if [ -n "${PULL_NUMBER:-}" ] && [ -n "${GITHUB_TOKEN:-}" ]; then
  : "${REPO_OWNER:=opendatahub-io}"
  : "${REPO_NAME:=opendatahub-operator}"
  GITHUB_PR_FLAGS="--github-owner=${REPO_OWNER} --github-repo=${REPO_NAME} --github-pr=${PULL_NUMBER} --failure-label=${E2E_FLAKY_LABEL}"
fi

RESULTS_DIR="${E2E_RESULTS_DIR:-results}"
RUN_TIMESTAMP="${E2E_RUN_TIMESTAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
export E2E_RUN_TIMESTAMP="$RUN_TIMESTAMP"
DAG_REPORT_PATH="${E2E_DAG_REPORT_PATH:-${RESULTS_DIR}/dag-report-${RUN_TIMESTAMP}.json}"
SUMMARY_PATH="${E2E_SUMMARY_PATH:-${RESULTS_DIR}/e2e-summary-${RUN_TIMESTAMP}.json}"
mkdir -p "$RESULTS_DIR"

MODE="${E2E_TEST_MODE:-all}"
case "${1:-}" in
  core|dag|all)
    MODE="$1"
    shift
    ;;
esac

case "$MODE" in
  core|dag|all) ;;
  *) echo "Error: E2E test mode must be core, dag, or all; got '$MODE'" >&2; exit 1 ;;
esac

JUNIT_FILE=""

filter_gate_skips_if_present() {
  if [ ! -f "$JUNIT_FILE" ]; then
    echo "No JUnit file at ${JUNIT_FILE}; skipping gate-skip filter"
    return 0
  fi

  if ! command -v test-retry >/dev/null 2>&1; then
    echo "Warning: test-retry not found; leaving ${JUNIT_FILE} unfiltered" >&2
    return 0
  fi

  echo "Filtering tag-gate skips from ${JUNIT_FILE}"
  test-retry filter-gate-skips --junit "$JUNIT_FILE" || {
    echo "Warning: gate-skip filter failed; leaving ${JUNIT_FILE} unfiltered" >&2
    return 0
  }

}

run_profile() {
  local profile=$1
  local artifact_name=$2
  shift 2

  export E2E_TEST_PROFILE="$profile"
  JUNIT_FILE="${RESULTS_DIR}/junit-${artifact_name}-${RUN_TIMESTAMP}.xml"
  echo "Running E2E profile ${profile}; JUnit output: ${JUNIT_FILE}"

# Choose test runner based on USE_TEST_RETRY flag
if [ "$USE_TEST_RETRY" = "true" ] || [ "$USE_TEST_RETRY" = "1" ]; then
  echo "Using test-retry for JUnit enrichment with failure classification"

  test_exit=0
  # shellcheck disable=SC2086
  test-retry e2e \
    --command ./e2e-tests \
    --filter "" \
    --path /e2e \
    --max-retries 3 \
    --junit-output "$JUNIT_FILE" \
    --verbose \
    ${GITHUB_PR_FLAGS} \
    -- --test.parallel=8 --test.timeout="$E2E_TEST_TIMEOUT" \
    --deletion-policy="$E2E_TEST_DELETION_POLICY" \
    --clean-up-previous-resources="$E2E_TEST_CLEAN_UP_PREVIOUS_RESOURCES" \
    --backup-and-restore-dsci-and-dsc="$E2E_TEST_BACKUP_AND_RESTORE_DSCI_AND_DSC" \
    --test-operator-controller="$E2E_TEST_OPERATOR_CONTROLLER" \
    --test-dependant-operators-management="$E2E_TEST_DEPENDANT_OPERATORS_MANAGEMENT" \
    --test-dsc-management="$E2E_TEST_DSC_MANAGEMENT" \
    --test-operator-resilience="$E2E_TEST_OPERATOR_RESILIENCE" \
    --test-operator-v2tov3upgrade="$E2E_TEST_OPERATOR_V2TOV3UPGRADE" \
    --test-conversion-webhook="$E2E_TEST_CONVERSION_WEBHOOK" \
    --test-conversion-webhook-dsc="$E2E_TEST_CONVERSION_WEBHOOK_DSC" \
    --test-conversion-webhook-platform="$E2E_TEST_CONVERSION_WEBHOOK_PLATFORM" \
    --test-services="$E2E_TEST_SERVICES" \
    --operator-namespace="$E2E_TEST_OPERATOR_NAMESPACE" \
    --applications-namespace="$E2E_TEST_APPLICATIONS_NAMESPACE" \
    --workbenches-namespace="$E2E_TEST_WORKBENCHES_NAMESPACE" \
    --dsc-monitoring-namespace="$E2E_TEST_DSC_MONITORING_NAMESPACE" \
    --tag="$E2E_TEST_TAG" \
    "$@" || test_exit=$?

  return "$test_exit"
else
  echo "Using gotestsum with leaf-oriented JUnit XML"

  # Go reports a failed subtest and every failed parent as separate failures.
  # Keep the raw artifacts for diagnostics, then exclude parent results from
  # the JUnit report consumed by the CI failure importer.
  # Keep raw JUnit content outside *.xml globs so CI ingests only final report.
  raw_junit_report="${RESULTS_DIR}/junit-${artifact_name}-${RUN_TIMESTAMP}.unfiltered"
  raw_test_events="${RESULTS_DIR}/test-events-${artifact_name}-${RUN_TIMESTAMP}.json"
  reordered_test_events="${RESULTS_DIR}/test-events-${artifact_name}-${RUN_TIMESTAMP}-reordered.json"

  # 1. Capture output from ./e2e-tests via test2json:
  #    -> results/test-events.json and results/xunit_report.unfiltered.
  test_status=0
  gotestsum --junitfile-project-name odh-operator-e2e \
    --junitfile "$raw_junit_report" --jsonfile "$raw_test_events" \
    --format standard-verbose --raw-command \
    -- test2json -t -p e2e ./e2e-tests --test.run='^TestOdhOperator' --test.v=test2json --test.parallel=8 --test.timeout="$E2E_TEST_TIMEOUT" \
    --deletion-policy="$E2E_TEST_DELETION_POLICY" \
    --clean-up-previous-resources="$E2E_TEST_CLEAN_UP_PREVIOUS_RESOURCES" \
    --backup-and-restore-dsci-and-dsc="$E2E_TEST_BACKUP_AND_RESTORE_DSCI_AND_DSC" \
    --test-operator-controller="$E2E_TEST_OPERATOR_CONTROLLER" \
    --test-dependant-operators-management="$E2E_TEST_DEPENDANT_OPERATORS_MANAGEMENT" \
    --test-dsc-management="$E2E_TEST_DSC_MANAGEMENT" \
    --test-operator-resilience="$E2E_TEST_OPERATOR_RESILIENCE" \
    --test-operator-v2tov3upgrade="$E2E_TEST_OPERATOR_V2TOV3UPGRADE" \
    --test-conversion-webhook="$E2E_TEST_CONVERSION_WEBHOOK" \
    --test-conversion-webhook-dsc="$E2E_TEST_CONVERSION_WEBHOOK_DSC" \
    --test-conversion-webhook-platform="$E2E_TEST_CONVERSION_WEBHOOK_PLATFORM" \
    --test-services="$E2E_TEST_SERVICES" \
    --operator-namespace="$E2E_TEST_OPERATOR_NAMESPACE" \
    --applications-namespace="$E2E_TEST_APPLICATIONS_NAMESPACE" \
    --workbenches-namespace="$E2E_TEST_WORKBENCHES_NAMESPACE" \
    --dsc-monitoring-namespace="$E2E_TEST_DSC_MONITORING_NAMESPACE" \
    --tag="$E2E_TEST_TAG" \
    "$@" || test_status=$?

  # 2. Group events: results/test-events.json -> results/test-events-reordered.json.
  #    Keep each test's skip messages together and not interleaved due to parallel execution,
  #    preserving event order within each test and first-seen group order.
  #    This ensures go-junit-report correctly assigns skip messages to the tests.
  report_status=0
  if jq -sc '
    to_entries
    | group_by([.value.Package, (.value.Test // .key)])
    | sort_by(.[0].key)
    | .[][].value
  ' "$raw_test_events" > "$reordered_test_events"; then
    :
  else
    report_status=$?
    echo "Error: failed to reorder test events" >&2
  fi

  # 3. Convert results/test-events-reordered.json -> results/xunit_report.xml,
  #    ignoring parent results. Their captured output is retained by the
  #    converter as suite-level output. If this leaves no failure/error despite
  #    a failed test process, retain the unfiltered report so parent-only
  #    failures (for example setup, cleanup, or watchdog failures) remain visible.
  if [ "$report_status" -eq 0 ]; then
    if go-junit-report -parser gojson \
      -subtest-mode ignore-parent-results \
      -in "$reordered_test_events" \
      -out "$JUNIT_FILE"; then
      if [ "$test_status" -ne 0 ] && \
        ! grep -Eq '<(failure|error)([[:space:]>])' "$JUNIT_FILE"; then
        echo "Warning: no leaf failure found; retaining unfiltered JUnit report" >&2
        if ! cp -- "$raw_junit_report" "$JUNIT_FILE"; then
          echo "Error: failed to retain unfiltered JUnit report" >&2
          report_status=1
        fi
      fi
    else
      report_status=$?
      echo "Error: failed to generate leaf-oriented JUnit report" >&2
    fi
  fi
  if [ "$report_status" -ne 0 ] && [ -s "$raw_junit_report" ]; then
    echo "Retaining unfiltered JUnit report"
    if ! cp -- "$raw_junit_report" "$JUNIT_FILE"; then
      echo "Error: failed to retain unfiltered JUnit report" >&2
      report_status=1
    fi
  fi

  # 4. Remove tag-gate skips from results/xunit_report.xml, updating it in place.
  #    This is the final report consumed by CI.
  filter_gate_skips_if_present
  if [ "$test_status" -ne 0 ]; then
    return "$test_status"
  fi
  return "$report_status"
fi
}

run_verdict() {
  local core_exit=$1
  local dag_exit=$2
  local verdict_mode=${3:-all}

  if ! command -v test-retry >/dev/null 2>&1; then
    echo "Warning: test-retry not found; policy verdict and summary are unavailable" >&2
    if [ "$core_exit" -ne 0 ]; then
      return "$core_exit"
    fi
    if [ "$dag_exit" -gt 0 ]; then
      return "$dag_exit"
    fi
    if [ "$dag_exit" -lt 0 ] && [ "$verdict_mode" != "core" ]; then
      return 1
    fi
    return 0
  fi

  local -a args=(--core-exit="$core_exit" --dag-exit="$dag_exit" --output="$SUMMARY_PATH")
  if [ "$verdict_mode" = "core" ]; then
    args+=(--core-only)
  fi
  if [ -f "$DAG_REPORT_PATH" ]; then
    args+=(--dag-report="$DAG_REPORT_PATH")
  fi
  test-retry verdict "${args[@]}"
}

core_status=0
dag_status=-1

if [ "$MODE" = "core" ] || [ "$MODE" = "all" ]; then
  run_profile core core "$@" || core_status=$?
  if [ "$core_status" -ne 0 ]; then
    run_verdict "$core_status" -1 "$MODE" || true
    exit "$core_status"
  fi
fi

if [ "$MODE" = "dag" ] || [ "$MODE" = "all" ]; then
  rm -f "$DAG_REPORT_PATH"
  export E2E_DAG_REPORT_PATH="$DAG_REPORT_PATH"
  dag_status=0
  run_profile dag dag "$@" || dag_status=$?
fi

if [ "$MODE" = "core" ]; then
  run_verdict 0 -1 core
  exit $?
fi

run_verdict 0 "$dag_status"

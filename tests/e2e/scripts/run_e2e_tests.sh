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

: "${E2E_TEST_WEBHOOK:=true}"
validate_bool E2E_TEST_WEBHOOK

: "${E2E_TEST_COMPONENTS:=true}"
validate_bool E2E_TEST_COMPONENTS

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

# Choose test runner based on USE_TEST_RETRY flag
if [ "$USE_TEST_RETRY" = "true" ] || [ "$USE_TEST_RETRY" = "1" ]; then
  echo "Using test-retry for JUnit enrichment with failure classification"

  # Run with test-retry (enriched JUnit XML with <properties>)
  # Note: No --filter flag (uses custom e2e flags like --tag, --test-operator-controller instead)
  # shellcheck disable=SC2086
  exec test-retry e2e \
    --command ./e2e-tests \
    --filter "" \
    --path /e2e \
    --max-retries 3 \
    --junit-output results/xunit_report.xml \
    --verbose \
    ${GITHUB_PR_FLAGS} \
    -- --test.parallel=8 \
    --deletion-policy="$E2E_TEST_DELETION_POLICY" \
    --clean-up-previous-resources="$E2E_TEST_CLEAN_UP_PREVIOUS_RESOURCES" \
    --backup-and-restore-dsci-and-dsc="$E2E_TEST_BACKUP_AND_RESTORE_DSCI_AND_DSC" \
    --test-operator-controller="$E2E_TEST_OPERATOR_CONTROLLER" \
    --test-dependant-operators-management="$E2E_TEST_DEPENDANT_OPERATORS_MANAGEMENT" \
    --test-dsc-management="$E2E_TEST_DSC_MANAGEMENT" \
    --test-operator-resilience="$E2E_TEST_OPERATOR_RESILIENCE" \
    --test-operator-v2tov3upgrade="$E2E_TEST_OPERATOR_V2TOV3UPGRADE" \
    --test-webhook="$E2E_TEST_WEBHOOK" \
    --test-components="$E2E_TEST_COMPONENTS" \
    --test-services="$E2E_TEST_SERVICES" \
    --operator-namespace="$E2E_TEST_OPERATOR_NAMESPACE" \
    --applications-namespace="$E2E_TEST_APPLICATIONS_NAMESPACE" \
    --workbenches-namespace="$E2E_TEST_WORKBENCHES_NAMESPACE" \
    --dsc-monitoring-namespace="$E2E_TEST_DSC_MONITORING_NAMESPACE" \
    --tag="$E2E_TEST_TAG" \
    "$@"
else
  echo "Using gotestsum with leaf-oriented JUnit XML"

  # Go reports a failed subtest and every failed parent as separate failures.
  # Keep the raw artifacts for diagnostics, then exclude parent results from
  # the JUnit report consumed by the CI failure importer.
  # Keep raw JUnit content outside *.xml globs so CI ingests only final report.
  raw_junit_report=results/xunit_report.unfiltered
  test_events=results/test-events.json

  set +e
  gotestsum --junitfile-project-name odh-operator-e2e \
    --junitfile "$raw_junit_report" --jsonfile "$test_events" \
    --format standard-verbose --raw-command \
    -- test2json -t -p e2e ./e2e-tests --test.run='^TestOdhOperator' --test.v=test2json --test.parallel=8 \
    --deletion-policy="$E2E_TEST_DELETION_POLICY" \
    --clean-up-previous-resources="$E2E_TEST_CLEAN_UP_PREVIOUS_RESOURCES" \
    --backup-and-restore-dsci-and-dsc="$E2E_TEST_BACKUP_AND_RESTORE_DSCI_AND_DSC" \
    --test-operator-controller="$E2E_TEST_OPERATOR_CONTROLLER" \
    --test-dependant-operators-management="$E2E_TEST_DEPENDANT_OPERATORS_MANAGEMENT" \
    --test-dsc-management="$E2E_TEST_DSC_MANAGEMENT" \
    --test-operator-resilience="$E2E_TEST_OPERATOR_RESILIENCE" \
    --test-operator-v2tov3upgrade="$E2E_TEST_OPERATOR_V2TOV3UPGRADE" \
    --test-webhook="$E2E_TEST_WEBHOOK" \
    --test-components="$E2E_TEST_COMPONENTS" \
    --test-services="$E2E_TEST_SERVICES" \
    --operator-namespace="$E2E_TEST_OPERATOR_NAMESPACE" \
    --applications-namespace="$E2E_TEST_APPLICATIONS_NAMESPACE" \
    --workbenches-namespace="$E2E_TEST_WORKBENCHES_NAMESPACE" \
    --dsc-monitoring-namespace="$E2E_TEST_DSC_MONITORING_NAMESPACE" \
    --tag="$E2E_TEST_TAG" \
    "$@"
  test_status=$?
  set -e

  # Ignore only parent results. Their captured output is retained by the
  # converter as suite-level output. If this leaves no failure/error despite
  # a failed test process, retain the unfiltered report so parent-only
  # failures (for example setup, cleanup, or watchdog failures) remain visible.
  if go-junit-report -parser gojson \
    -subtest-mode ignore-parent-results \
    -in "$test_events" \
    -out results/xunit_report.xml; then
    if [ "$test_status" -ne 0 ] && \
      ! grep -Eq '<(failure|error)([[:space:]>])' results/xunit_report.xml; then
      echo "Warning: no leaf failure found; retaining unfiltered JUnit report" >&2
      cp -- "$raw_junit_report" results/xunit_report.xml
    fi
  else
    report_status=$?
    echo "Error: failed to generate leaf-oriented JUnit report" >&2
    if [ -s "$raw_junit_report" ]; then
      echo "Retaining unfiltered JUnit report"
      cp -- "$raw_junit_report" results/xunit_report.xml
    else
      exit "$report_status"
    fi
  fi

  exit "$test_status"
fi

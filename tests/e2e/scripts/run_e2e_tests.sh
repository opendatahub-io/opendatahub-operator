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

: "${E2E_TEST_WORKBENCHES_NAMESPACE:=opendatahub}"
validate_namespace E2E_TEST_WORKBENCHES_NAMESPACE

: "${E2E_TEST_DSC_MONITORING_NAMESPACE:=opendatahub}"
validate_namespace E2E_TEST_DSC_MONITORING_NAMESPACE

: "${E2E_TEST_TAG:=All}"
validate_tag E2E_TEST_TAG

# Run gotestsum with the environment variables and any additional arguments
exec gotestsum --junitfile-project-name odh-operator-e2e \
  --junitfile results/xunit_report.xml --format testname --raw-command \
  -- test2json -t -p e2e ./e2e-tests --test.v=test2json --test.parallel=8 \
  --deletion-policy="$E2E_TEST_DELETION_POLICY" --clean-up-previous-resources="$E2E_TEST_CLEAN_UP_PREVIOUS_RESOURCES" \
  --test-operator-controller="$E2E_TEST_OPERATOR_CONTROLLER" --test-dependant-operators-management="$E2E_TEST_DEPENDANT_OPERATORS_MANAGEMENT" \
  --test-dsc-management="$E2E_TEST_DSC_MANAGEMENT" --test-operator-resilience="$E2E_TEST_OPERATOR_RESILIENCE" \
  --test-operator-v2tov3upgrade="$E2E_TEST_OPERATOR_V2TOV3UPGRADE" \
  --test-webhook="$E2E_TEST_WEBHOOK" --test-components="$E2E_TEST_COMPONENTS" --test-services="$E2E_TEST_SERVICES" \
  --operator-namespace="$E2E_TEST_OPERATOR_NAMESPACE" --applications-namespace="$E2E_TEST_APPLICATIONS_NAMESPACE" \
  --workbenches-namespace="$E2E_TEST_WORKBENCHES_NAMESPACE" --dsc-monitoring-namespace="$E2E_TEST_DSC_MONITORING_NAMESPACE" \
  --tag="$E2E_TEST_TAG" "$@"

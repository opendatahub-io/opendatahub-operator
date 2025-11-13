#!/bin/bash

PROMETHEUS_RULES_DIR=$1
ALERT_SEVERITY=$2

# Collect all alerts from the PrometheusRule template files
while IFS= read -r ALERT; do
  ALL_ALERTS+=("$ALERT")
done < <(
  find "${PROMETHEUS_RULES_DIR}" -name "*-prometheusrules.tmpl.yaml" -type f | while read -r rule_file; do
    yq -N e '.spec.groups[].rules[]
      | select(.alert != null and .labels.severity == "'${ALERT_SEVERITY}'")
      | .alert' "${rule_file}"
  done
)

# Collect all alerts from the unit test files
while IFS= read -r ALERT; do
  PROMETHEUS_UNIT_TEST_CHECK+=("$ALERT")
done < <(
  find "${PROMETHEUS_RULES_DIR}" -name "*-alerting.unit-tests.yaml" -type f | while read -r alert; do
    yq -N eval-all '.tests[]
    | .alert_rule_test[]
    | .exp_alerts[]
    | .exp_labels
    | select(.severity == "'${ALERT_SEVERITY}'")
    | .alertname' "$alert"
  done
)

# Sorting the PROMETHEUS_UNIT_TEST_CHECK array for comparison
PROMETHEUS_UNIT_TEST_CHECK_SORTED=($(echo "${PROMETHEUS_UNIT_TEST_CHECK[@]}" | sort | uniq))

# Finding items in ALL_ALERTS not in PROMETHEUS_UNIT_TEST_CHECK_SORTED
ALERTS_WITHOUT_UNIT_TESTS=()
for ALERT in "${ALL_ALERTS[@]}"; do
  if [[ ! " ${PROMETHEUS_UNIT_TEST_CHECK_SORTED[@]} " =~ " ${ALERT} " ]]; then
    ALERTS_WITHOUT_UNIT_TESTS+=("$ALERT")
  fi
done

# Printing the alerts without unit tests
echo "Alerts without unit tests:"
for ALERT in "${ALERTS_WITHOUT_UNIT_TESTS[@]}"; do
  echo "$ALERT"
done

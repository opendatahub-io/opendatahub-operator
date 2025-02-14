#!/bin/bash

PROMETHEUS_CONFIG_YAML=$1
UNIT_TEST_DIR=$2
ALERT_SEVERITY=$3

# Collect all alerts from the configuration file
while IFS= read -r ALERT; do
  ALL_ALERTS+=("$ALERT")
done < <(yq -N e '.data[]
  | from_yaml
  | .groups[].rules[]
  | select(.alert != "DeadManSnitch" and .labels.severity == "'${ALERT_SEVERITY}'")
  | .alert' "${PROMETHEUS_CONFIG_YAML}")

# Collect all alerts from the unit test files
while IFS= read -r ALERT; do
  PROMETHEUS_UNIT_TEST_CHECK+=("$ALERT")
done < <(
  for alert in "$UNIT_TEST_DIR"/*.yaml; do
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

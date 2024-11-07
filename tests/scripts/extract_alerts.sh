#!/bin/bash

INPUT_YAML="$1"
OUTPUT_DIR="$2"

mkdir -p "$OUTPUT_DIR"

# Define RULES_FILES and output files
RULES_FILES=(
  "rhods-dashboard-alerting.rules"
  "model-mesh-alerting.rules"
  "trustyai-alerting.rules"
  "odh-model-controller-alerting.rules"
  "workbenches-alerting.rules"
  "data-science-pipelines-operator-alerting.rules"
  "kserve-alerting.rules"
  "kueue-alerting.rules"
  "ray-alerting.rules"
  "codeflare-alerting.rules"
  "trainingoperator-alerting.rules"
)

ALERT_FILES=(
  "dashboard_alerts.yaml"
  "model_mesh_alerts.yaml"
  "trustyai_alerts.yaml"
  "model_controller_alerts.yaml"
  "workbenches_alerts.yaml"
  "data_science_pipelines_operator_alerts.yaml"
  "kserve_alerts.yaml"
  "kueue_alerts.yaml"
  "kuberay_alerts.yaml"
  "codeflare_alerts.yaml"
  "training_operator_alerts.yaml"
)

for i in "${!RULES_FILES[@]}"; do
  RULE_FILE="${RULES_FILES[$i]}"
  ALERT_FILE="${ALERT_FILES[$i]}"
  echo "Extracting $RULE_FILE to $OUTPUT_DIR/$ALERT_FILE"
  yq ".data.\"$RULE_FILE\"" "$INPUT_YAML" > "$OUTPUT_DIR/$ALERT_FILE"
done

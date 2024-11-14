#!/usr/bin/env bash
set -e

INPUT_YAML="$1"
OUTPUT_DIR="$2"

# Define an associative array mapping rule files to alert files
declare -A RULES_TO_ALERTS=(
    [rhods-dashboard-alerting.rules]="dashboard_alerts.yaml"
    [model-mesh-alerting.rules]="model_mesh_alerts.yaml"
    [trustyai-alerting.rules]="trustyai_alerts.yaml"
    [odh-model-controller-alerting.rules]="model_controller_alerts.yaml"
    [workbenches-alerting.rules]="workbenches_alerts.yaml"
    [data-science-pipelines-operator-alerting.rules]="data_science_pipelines_operator_alerts.yaml"
    [kserve-alerting.rules]="kserve_alerts.yaml"
    [kueue-alerting.rules]="kueue_alerts.yaml"
    [ray-alerting.rules]="kuberay_alerts.yaml"
    [codeflare-alerting.rules]="codeflare_alerts.yaml"
    [trainingoperator-alerting.rules]="training_operator_alerts.yaml"
)

for RULE_FILE in "${!RULES_TO_ALERTS[@]}"; do
    ALERT_FILE="${RULES_TO_ALERTS[$RULE_FILE]}"
  
    echo "key: $RULE_FILE"
    echo "value: $ALERT_FILE"
  
    echo "Extracting $RULE_FILE to $OUTPUT_DIR/$ALERT_FILE"
    yq ".data.\"$RULE_FILE\"" "$INPUT_YAML" > "$OUTPUT_DIR/$ALERT_FILE"
done

#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SCENARIOS_DIR="$SCRIPT_DIR/../scenarios"
DATASET_DIR="$SCRIPT_DIR/dataset"

echo "=== Generating Eval Dataset from Scenarios ==="

if [ ! -d "$SCENARIOS_DIR" ]; then
  echo "ERROR: Scenarios directory not found at $SCENARIOS_DIR" >&2
  exit 1
fi

count=0
errors=0

for gt_file in "$SCENARIOS_DIR"/*-ground-truth.json; do
  [ -f "$gt_file" ] || continue

  scenario_id=$(basename "$gt_file" | sed 's/-ground-truth\.json$//')
  scenario_dir="$DATASET_DIR/$scenario_id"

  setup_script="$SCENARIOS_DIR/${scenario_id}-setup.sh"
  teardown_script="$SCENARIOS_DIR/${scenario_id}-teardown.sh"

  if [ ! -f "$setup_script" ]; then
    echo "  INFO: $scenario_id has no setup script (JSON-only scenario)"
    setup_script=""
  fi

  if [ ! -f "$teardown_script" ]; then
    echo "  INFO: $scenario_id has no teardown script (JSON-only scenario)"
    teardown_script=""
  fi

  mkdir -p "$scenario_dir"

  # Validate JSON and extract fields in one pass
  if ! _fields=$(python3 -c "import json, sys; d=json.load(open(sys.argv[1])); print(d['scenario_name']); print(d['description'])" "$gt_file" 2>/dev/null); then
    echo "  ERROR: Invalid JSON or missing fields in $gt_file" >&2
    errors=$((errors + 1))
    continue
  fi
  scenario_name=$(echo "$_fields" | head -1)
  description=$(echo "$_fields" | tail -n +2)

  python3 -c "
import yaml, sys
data = {
    'scenario_id': sys.argv[1],
    'scenario_name': sys.argv[2],
    'description': sys.argv[3],
    'prompt': 'Diagnose any issues with the OpenDataHub platform on this cluster. Provide root cause analysis and actionable remediation steps.',
    'setup_script': sys.argv[4],
    'teardown_script': sys.argv[5],
}
with open(sys.argv[6], 'w') as f:
    yaml.dump(data, f, default_flow_style=False, sort_keys=False)
" "$scenario_id" "$scenario_name" "$description" "$setup_script" "$teardown_script" "$scenario_dir/input.yaml"

  # Create annotations.yaml from ground truth for judge consumption
  if ! python3 -c "
import json, yaml, sys
with open(sys.argv[1]) as f:
    gt = json.load(f)
with open(sys.argv[2], 'w') as f:
    yaml.dump(gt, f, default_flow_style=False, sort_keys=False)
" "$gt_file" "$scenario_dir/annotations.yaml"; then
    echo "  ERROR: Failed to create annotations.yaml for $scenario_id" >&2
    errors=$((errors + 1))
    continue
  fi

  cp "$gt_file" "$scenario_dir/ground-truth.json"

  count=$((count + 1))
  echo "  Created: $scenario_id"
done

echo ""
echo "Generated $count scenario datasets in $DATASET_DIR"

if [ "$errors" -gt 0 ]; then
  echo "WARNING: $errors scenarios had errors and were skipped" >&2
fi

if [ "$count" -eq 0 ]; then
  echo "ERROR: No scenarios generated. Check $SCENARIOS_DIR for *-ground-truth.json files." >&2
  exit 1
fi

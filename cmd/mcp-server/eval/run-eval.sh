#!/bin/bash
set -euo pipefail

# Orchestrator: inject failure -> run configs A/B/C -> teardown -> score -> merge.
# Usage: ./run-eval.sh [--scenarios operator-crash,cascading-failure] [--configs a,b,c]

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
RESULTS_DIR="$SCRIPT_DIR/results"
DATASET_DIR="$SCRIPT_DIR/dataset"
SCENARIOS_DIR="$SCRIPT_DIR/../scenarios"
DIAGNOSTIC_PROMPT="$SCRIPT_DIR/../prompts/diagnostic.md"

INJECT_WAIT="${INJECT_WAIT:-30}"
TEARDOWN_WAIT="${TEARDOWN_WAIT:-60}"
VENV_DIR="$SCRIPT_DIR/.agent-eval-harness/.venv"

cd "$SCRIPT_DIR"

# Activate virtualenv if available
if [ -d "$VENV_DIR" ]; then
  # shellcheck disable=SC1091
  source "$VENV_DIR/bin/activate"
fi

# --- Helpers ---

die() { echo "ERROR: $1" >&2; exit 1; }

yaml_field() {
  python3 -c "
import yaml, sys
try:
    with open(sys.argv[1]) as f:
        d = yaml.safe_load(f) or {}
    for k in sys.argv[2].split('.'):
        d = d.get(k, '') if isinstance(d, dict) else ''
    print(d if d else sys.argv[3])
except Exception as e:
    print(sys.argv[3])
    print('WARNING: %s: %s' % (sys.argv[1], e), file=sys.stderr)
" "$1" "$2" "${3:-}"
}

run_config_a() {
  local prompt="$1" output_dir="$2" model="$3"
  # Raw LLM + MCP tools, no system prompt
  cd "$REPO_ROOT"
  claude --print --verbose \
    --model "$model" \
    --max-turns 20 \
    --output-format stream-json \
    "$prompt" \
    > "$output_dir/stream.jsonl" 2> "$output_dir/claude-stderr.log"
  cd "$SCRIPT_DIR"
  # Extract diagnosis text and tool calls from stream
  python3 "$SCRIPT_DIR/scripts/parse-stream.py" \
    "$output_dir/stream.jsonl" "$output_dir"
}

run_config_b() {
  local prompt="$1" output_dir="$2" model="$3"
  # Diagnostic agent with system prompt + MCP tools
  if [ ! -f "$DIAGNOSTIC_PROMPT" ]; then
    die "Diagnostic prompt not found: $DIAGNOSTIC_PROMPT"
  fi
  cd "$REPO_ROOT"
  claude --print --verbose \
    --model "$model" \
    --max-turns 20 \
    --output-format stream-json \
    --system-prompt-file "$DIAGNOSTIC_PROMPT" \
    "$prompt" \
    > "$output_dir/stream.jsonl" 2> "$output_dir/claude-stderr.log"
  cd "$SCRIPT_DIR"
  python3 "$SCRIPT_DIR/scripts/parse-stream.py" \
    "$output_dir/stream.jsonl" "$output_dir"
}

run_config_c() {
  local output_dir="$1" scenario_id="$2"
  # Deterministic classifier only — no LLM
  cd "$REPO_ROOT/cmd/mcp-server"
  go run . --one-shot --test-name "$scenario_id" \
    > "$output_dir/full-report.json" 2> "$output_dir/classifier-stderr.log"
  # Extract just the classification for scoring
  python3 -c "
import json, sys
with open(sys.argv[1]) as f:
    data = json.load(f)
classification = data.get('classification', {})
if not classification:
    print('ERROR: no classification key in ' + sys.argv[1], file=sys.stderr)
    sys.exit(1)
with open(sys.argv[2], 'w') as f:
    json.dump(classification, f, indent=2)
" "$output_dir/full-report.json" "$output_dir/diagnosis.txt"
  cd "$SCRIPT_DIR"
}

# --- Args ---

FILTER_SCENARIOS=""
FILTER_CONFIGS="a,b,c"
REPEATS=1

while [[ $# -gt 0 ]]; do
  case "$1" in
    --scenarios) FILTER_SCENARIOS="$2"; shift 2 ;;
    --configs)   FILTER_CONFIGS="$2"; shift 2 ;;
    --repeats)   REPEATS="$2"; shift 2 ;;
    --help)
      echo "Usage: $0 [--scenarios s1,s2] [--configs a,b,c] [--repeats N]"
      echo "  --repeats N  Run Config B N times for consistency measurement (default 1)"
      echo "Env: INJECT_WAIT (default 30), TEARDOWN_WAIT (default 60)"
      exit 0 ;;
    *) die "Unknown argument: $1" ;;
  esac
done

[[ "$REPEATS" =~ ^[0-9]+$ ]] && [ "$REPEATS" -ge 1 ] || die "--repeats must be a positive integer"

# --- Preflight ---

[ -f "$REPO_ROOT/.mcp.json" ] || die ".mcp.json not found at repo root: $REPO_ROOT"
command -v claude &>/dev/null || die "claude CLI not found. Config A and B require it."
command -v go &>/dev/null || die "go not found. Config C requires it."
command -v oc &>/dev/null || command -v kubectl &>/dev/null \
  || die "Neither oc nor kubectl found."
[ -d "$DATASET_DIR" ] && [ -n "$(ls -A "$DATASET_DIR" 2>/dev/null)" ] \
  || die "Dataset empty. Run ./generate-dataset.sh first."

if command -v oc &>/dev/null; then
  oc whoami &>/dev/null || die "Not logged into cluster. Run 'oc login' first."
  echo "Cluster: $(oc whoami --show-server 2>/dev/null || echo 'connected')"
fi

# --- Build scenario and config lists ---

scenarios=()
if [ -n "$FILTER_SCENARIOS" ]; then
  IFS=',' read -ra scenarios <<< "$FILTER_SCENARIOS"
else
  for dir in "$DATASET_DIR"/*/; do
    [ -d "$dir" ] && scenarios+=("$(basename "$dir")")
  done
fi
[ ${#scenarios[@]} -gt 0 ] || die "No scenarios found in $DATASET_DIR"

for s in "${scenarios[@]}"; do
  [[ "$s" =~ ^[a-zA-Z0-9_-]+$ ]] || die "Invalid scenario name '$s': only alphanumeric, hyphens, and underscores allowed"
done

IFS=',' read -ra config_letters <<< "$FILTER_CONFIGS"

# --- Run ---

RUN_ID="$(date +%Y-%m-%d-%H%M%S)"
echo ""
echo "=== Eval Run: $RUN_ID ==="
echo "Scenarios: ${scenarios[*]} | Configs: ${config_letters[*]} | Repeats (B): $REPEATS"
echo ""

failed=0

for i in "${!scenarios[@]}"; do
  scenario="${scenarios[$i]}"
  echo "[$((i+1))/${#scenarios[@]}] === $scenario ==="

  setup_script="$SCENARIOS_DIR/${scenario}-setup.sh"
  teardown_script="$SCENARIOS_DIR/${scenario}-teardown.sh"

  has_setup=true
  if [ ! -f "$setup_script" ] || [ ! -f "$teardown_script" ]; then
    echo "  INFO: No setup/teardown scripts — running as JSON-only scenario"
    has_setup=false
  fi

  if [ "$has_setup" = true ]; then
    echo "  Injecting..."
    if ! "$SCRIPT_DIR/scripts/inject-failure.sh" "$setup_script" "$INJECT_WAIT"; then
      echo "  ERROR: Injection failed" >&2
      "$SCRIPT_DIR/scripts/teardown-failure.sh" "$teardown_script" "$TEARDOWN_WAIT" \
        || echo "  WARNING: Teardown after failed injection — may need manual cleanup" >&2
      failed=$((failed + 1))
      continue
    fi
  fi

  prompt=$(yaml_field "$DATASET_DIR/$scenario/input.yaml" "prompt" \
    "Diagnose any issues with the OpenDataHub platform on this cluster. Provide root cause analysis and actionable remediation steps.")

  config_failed=false
  for config_letter in "${config_letters[@]}"; do
    config_name="config-$config_letter"
    output_dir="$RESULTS_DIR/$config_name/$scenario"
    mkdir -p "$output_dir"

    echo "  $config_name..."
    run_start=$(date +%s)
    exit_code=0

    case "$config_letter" in
      a)
        model=$(yaml_field "$SCRIPT_DIR/configs/config-a-baseline.yaml" "models.skill" "claude-opus-4-6")
        run_config_a "$prompt" "$output_dir" "$model" || exit_code=$?
        ;;
      b)
        model=$(yaml_field "$SCRIPT_DIR/configs/config-b-agent.yaml" "models.skill" "claude-opus-4-6")
        if [ "$REPEATS" -gt 1 ]; then
          for r in $(seq 1 "$REPEATS"); do
            repeat_dir="$RESULTS_DIR/config-b-run${r}/$scenario"
            mkdir -p "$repeat_dir"
            echo "    run $r/$REPEATS..."
            r_start=$(date +%s)
            r_exit=0
            run_config_b "$prompt" "$repeat_dir" "$model" || r_exit=$?
            r_dur=$(( $(date +%s) - r_start ))
            python3 -c "
import json, sys
with open(sys.argv[1], 'w') as f:
    json.dump({'exit_code': int(sys.argv[2]), 'duration_s': float(sys.argv[3])}, f, indent=2)
" "$repeat_dir/run_result.json" "$r_exit" "$r_dur"
            [ "$r_exit" -ne 0 ] && exit_code="$r_exit"
          done
        else
          run_config_b "$prompt" "$output_dir" "$model" || exit_code=$?
        fi
        ;;
      c)
        run_config_c "$output_dir" "$scenario" || exit_code=$?
        ;;
      *)
        echo "    SKIP: unknown config '$config_letter'" >&2
        continue
        ;;
    esac

    duration=$(( $(date +%s) - run_start ))

    # Save run metadata
    python3 -c "
import json, sys
with open(sys.argv[1], 'w') as f:
    json.dump({
        'exit_code': int(sys.argv[2]),
        'duration_s': float(sys.argv[3]),
        'config': sys.argv[4],
        'scenario': sys.argv[5],
    }, f, indent=2)
" "$output_dir/run_result.json" "$exit_code" "$duration" "$config_name" "$scenario"

    if [ "$exit_code" -eq 0 ]; then
      echo "    OK (${duration}s)"
    else
      echo "    FAIL (exit $exit_code, ${duration}s)" >&2
      # Show first few lines of error log
      for errlog in "$output_dir/claude-stderr.log" "$output_dir/classifier-stderr.log"; do
        if [ -f "$errlog" ] && [ -s "$errlog" ]; then
          head -3 "$errlog" 2>/dev/null \
            | sed -E 's/(sk-ant-api[0-9A-Za-z_-]+|Bearer [^ "]+|[A-Za-z0-9+/]{40,}={0,2})/[REDACTED]/g' \
            | sed -E 's|https?://[^ ]*\.(internal\|local\|corp\|lan)[^ ]*|[REDACTED-URL]|g' \
            | sed 's/^/    /' >&2
          break
        fi
      done
      config_failed=true
    fi
  done

  if [ "$has_setup" = true ]; then
    echo "  Tearing down..."
    "$SCRIPT_DIR/scripts/teardown-failure.sh" "$teardown_script" "$TEARDOWN_WAIT" \
      || echo "  WARNING: Teardown failed — may need manual cleanup" >&2
  fi

  [ "$config_failed" = true ] && failed=$((failed + 1))
  echo ""
done

# --- Score & Anonymize ---

echo ""
echo "=== Scoring ==="
python3 "$SCRIPT_DIR/scripts/score-results.py" \
  --results-dir "$RESULTS_DIR" \
  --dataset-dir "$DATASET_DIR" \
  || echo "ERROR: Scoring failed" >&2

echo ""
echo "=== Blind Scoring ==="
python3 "$SCRIPT_DIR/scripts/anonymize-outputs.py" \
  --results-dir "$RESULTS_DIR" \
  || echo "ERROR: Anonymization failed" >&2

python3 "$SCRIPT_DIR/scoring/generate-blind-sheets.py" \
  --blind-dir "$RESULTS_DIR/blind-scoring" \
  || echo "ERROR: Blind sheet generation failed" >&2

# --- Done ---

echo ""
echo "=== Complete: $RUN_ID ==="
echo "Scenarios: ${#scenarios[@]} total, $failed failed"
echo "Report: $RESULTS_DIR/eval-report.md"
echo "Results: $RESULTS_DIR/scored-results.json"
echo "Blind scoring: $RESULTS_DIR/blind-scoring/scoring-sheet.csv"

[ "$failed" -gt 0 ] && exit 1
exit 0

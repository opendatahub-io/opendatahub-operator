#!/bin/bash
set -euo pipefail

# Runs a scenario teardown script and waits for cluster recovery.
# Usage: ./teardown-failure.sh <teardown-script-path> [wait-seconds]

if [ $# -lt 1 ]; then
  echo "ERROR: Usage: $0 <teardown-script-path> [wait-seconds]" >&2
  exit 1
fi

TEARDOWN_SCRIPT="$1"
WAIT_SECONDS="${2:-60}"

if [ ! -f "$TEARDOWN_SCRIPT" ]; then
  echo "ERROR: Teardown script not found: $TEARDOWN_SCRIPT" >&2
  exit 1
fi

if [ ! -x "$TEARDOWN_SCRIPT" ]; then
  echo "ERROR: Teardown script not executable: $TEARDOWN_SCRIPT" >&2
  echo "Run: chmod +x $TEARDOWN_SCRIPT" >&2
  exit 1
fi

if ! [[ "$WAIT_SECONDS" =~ ^[0-9]+$ ]]; then
  echo "ERROR: wait-seconds must be a number, got: $WAIT_SECONDS" >&2
  exit 1
fi

SCENARIO_NAME=$(basename "$TEARDOWN_SCRIPT" | sed 's/-teardown\.sh$//')
echo "[teardown] Starting: $SCENARIO_NAME"

if ! "$TEARDOWN_SCRIPT"; then
  echo "ERROR: Teardown failed for $SCENARIO_NAME" >&2
  echo "ERROR: Cluster may need manual cleanup" >&2
  exit 1
fi

echo "[teardown] Waiting ${WAIT_SECONDS}s for cluster recovery..."
sleep "$WAIT_SECONDS"
echo "[teardown] Complete: $SCENARIO_NAME"

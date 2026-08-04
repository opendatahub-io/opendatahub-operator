#!/bin/bash
set -euo pipefail

# Runs a scenario setup script and waits for the failure to propagate.
# Usage: ./inject-failure.sh <setup-script-path> [wait-seconds]

if [ $# -lt 1 ]; then
  echo "ERROR: Usage: $0 <setup-script-path> [wait-seconds]" >&2
  exit 1
fi

SETUP_SCRIPT="$1"
WAIT_SECONDS="${2:-30}"

if [ ! -f "$SETUP_SCRIPT" ]; then
  echo "ERROR: Setup script not found: $SETUP_SCRIPT" >&2
  exit 1
fi

if [ ! -x "$SETUP_SCRIPT" ]; then
  echo "ERROR: Setup script not executable: $SETUP_SCRIPT" >&2
  echo "Run: chmod +x $SETUP_SCRIPT" >&2
  exit 1
fi

if ! [[ "$WAIT_SECONDS" =~ ^[0-9]+$ ]]; then
  echo "ERROR: wait-seconds must be a number, got: $WAIT_SECONDS" >&2
  exit 1
fi

SCENARIO_NAME=$(basename "$SETUP_SCRIPT" | sed 's/-setup\.sh$//')
echo "[inject] Starting: $SCENARIO_NAME"

if ! "$SETUP_SCRIPT"; then
  echo "ERROR: Setup script failed for $SCENARIO_NAME" >&2
  exit 1
fi

echo "[inject] Waiting ${WAIT_SECONDS}s for failure to propagate..."
sleep "$WAIT_SECONDS"
echo "[inject] Ready: $SCENARIO_NAME"

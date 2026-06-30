#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
HARNESS_DIR="$SCRIPT_DIR/.agent-eval-harness"
HARNESS_VERSION="v1.9.1"

echo "=== Eval Harness Setup ==="

if [ ! -d "$HARNESS_DIR" ]; then
  echo "Cloning agent-eval-harness @ $HARNESS_VERSION..."
  if ! git clone --branch "$HARNESS_VERSION" --depth 1 \
    https://github.com/opendatahub-io/agent-eval-harness.git "$HARNESS_DIR"; then
    echo "ERROR: Failed to clone agent-eval-harness" >&2
    exit 1
  fi
else
  echo "agent-eval-harness already present at $HARNESS_DIR"
fi

VENV_DIR="$HARNESS_DIR/.venv"
if [ ! -d "$VENV_DIR" ]; then
  echo "Creating virtualenv at $VENV_DIR..."
  python3 -m venv "$VENV_DIR"
fi
# shellcheck disable=SC1091
source "$VENV_DIR/bin/activate"

echo "Installing agent-eval-harness..."
cd "$HARNESS_DIR"
if ! pip install -e ".[anthropic]" --quiet; then
  echo "ERROR: pip install failed" >&2
  exit 1
fi

echo "Installing eval analysis dependencies..."
pip install scipy --quiet || echo "WARNING: scipy install failed — statistical tests will be skipped" >&2

echo ""
echo "Verifying prerequisites..."
exit_code=0

if command -v claude &>/dev/null; then
  echo "  claude CLI: available"
else
  echo "  WARNING: claude CLI not found. Config A and B require it." >&2
  exit_code=1
fi

if command -v go &>/dev/null; then
  echo "  go: $(go version 2>&1)"
else
  echo "  WARNING: go not found. Config C requires it." >&2
  exit_code=1
fi

if command -v oc &>/dev/null; then
  echo "  oc: available"
elif command -v kubectl &>/dev/null; then
  echo "  kubectl: available"
else
  echo "  WARNING: neither oc nor kubectl found. Cluster access is required." >&2
  exit_code=1
fi

mkdir -p "$SCRIPT_DIR/results"

if [ "$exit_code" -ne 0 ]; then
  echo ""
  echo "Setup completed with warnings. Some configs may not run." >&2
fi

echo ""
echo "Setup complete. Next: run ./generate-dataset.sh"
exit "$exit_code"

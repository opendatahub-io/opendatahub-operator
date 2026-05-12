#!/usr/bin/env bash
set -euo pipefail

SCENARIO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKUP_DIR="${BACKUP_DIR:-$(mktemp -d -t odh-scenario-backups-XXXXXX)}"
trap 'rm -rf "$BACKUP_DIR"' EXIT

# --- Namespace Discovery ---

discover_apps_namespace() {
  local ns=""
  if command -v oc &>/dev/null; then
    ns=$(oc get dsci --all-namespaces -o jsonpath='{.items[0].spec.applicationsNamespace}' 2>/dev/null || true)
  fi
  if [[ -z "$ns" ]]; then
    ns="${ODH_APPS_NAMESPACE:-opendatahub}"
  fi
  echo "$ns"
}

discover_operator_namespace() {
  local ns=""
  if command -v oc &>/dev/null; then
    ns=$(oc get deployment -A -l name=opendatahub-operator \
      -o jsonpath='{.items[0].metadata.namespace}' 2>/dev/null || true)
  fi
  if [[ -z "$ns" ]]; then
    ns="${ODH_OPERATOR_NAMESPACE:-openshift-operators}"
  fi
  echo "$ns"
}

# --- Wait Helpers ---

wait_for_pod_status() {
  local namespace="$1" label="$2" status="$3" timeout="${4:-120}"
  log_step "Waiting for pod ($label) in $namespace to reach status=$status (timeout=${timeout}s)"
  local elapsed=0
  while (( elapsed < timeout )); do
    local current
    current=$(oc get pods -n "$namespace" -l "$label" -o jsonpath='{.items[0].status.phase}' 2>/dev/null || echo "")
    if [[ "$current" == "$status" ]]; then
      log_step "Pod reached status=$status"
      return 0
    fi
    sleep 5
    (( elapsed += 5 ))
  done
  log_step "WARN: Timed out waiting for pod status=$status (current=${current:-unknown})"
  return 1
}

wait_for_deployment_ready() {
  local namespace="$1" name="$2" timeout="${3:-180s}"
  log_step "Waiting for deployment $name in $namespace to be available (timeout=$timeout)"
  oc rollout status deployment/"$name" -n "$namespace" --timeout="$timeout"
}

# --- Backup / Restore ---

backup_resource() {
  local kind="$1" name="$2" namespace="$3" backup_file="${4:-}"
  kind="${kind//\//_}"
  name="${name//\//_}"
  namespace="${namespace//\//_}"
  if [[ -z "$backup_file" ]]; then
    backup_file="${BACKUP_DIR}/${namespace}_${kind}_${name}.json"
  fi
  log_step "Backing up $kind/$name from $namespace to $backup_file"
  oc get "$kind" "$name" -n "$namespace" -o json > "$backup_file"
  echo "$backup_file"
}

restore_resource() {
  local file="$1"
  if [[ ! -f "$file" ]]; then
    log_step "ERROR: Backup file $file not found"
    return 1
  fi
  log_step "Restoring resource from $file"
  oc apply -f "$file"
}

# --- Logging ---

log_step() {
  local msg="$1"
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $msg"
}

# --- Prerequisite Check ---

require_tools() {
  local missing=()
  for tool in "$@"; do
    if ! command -v "$tool" &>/dev/null; then
      missing+=("$tool")
    fi
  done
  if (( ${#missing[@]} > 0 )); then
    echo "ERROR: Missing required tools: ${missing[*]}"
    exit 1
  fi
}

# --- MCP Tool Invocation ---
# The MCP server uses stdio transport (JSON-RPC over stdin/stdout).
# Binary location: set MCP_SERVER_BIN or default to bin/mcp-server
# relative to the repo root.

MCP_SERVER_BIN="${MCP_SERVER_BIN:-$(cd "$SCENARIO_DIR/../../.." && pwd)/bin/mcp-server}"

export E2E_TEST_OPERATOR_NAMESPACE="${E2E_TEST_OPERATOR_NAMESPACE:-$(discover_operator_namespace)}"
export E2E_TEST_APPLICATIONS_NAMESPACE="${E2E_TEST_APPLICATIONS_NAMESPACE:-$(discover_apps_namespace)}"

call_mcp_tool() {
  local tool_name="$1"
  local params_json="${2:-"{}"}"
  if [[ ! -x "$MCP_SERVER_BIN" ]]; then
    log_step "ERROR: MCP server binary not found at $MCP_SERVER_BIN. Run 'make mcp-server' first."
    return 1
  fi
  log_step "Calling MCP tool: $tool_name" >&2
  jq -nc --arg name "$tool_name" --argjson args "$params_json" \
    '{jsonrpc:"2.0",id:1,method:"tools/call",params:{name:$name,arguments:$args}}' \
    | "$MCP_SERVER_BIN" 2>/dev/null \
    | jq -r '.result.content[0].text'
}


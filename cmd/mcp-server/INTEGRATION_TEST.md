# MCP Server Integration Tests

Manual test guide for the 7 MCP server tools against a live cluster.

## Prerequisites

| Requirement | Details |
|---|---|
| Cluster | OpenShift/Kubernetes with `KUBECONFIG` set |
| ODH operator | Installed in `opendatahub-operator-system` |
| CRs | DSCI + DSC created, at least one component `Managed` |
| Tools | `jq` installed |
| Binary | Built via `make mcp-server` |

## Setup

Run all commands from the repository root. Add these helpers to your shell:

```bash
# For tools returning JSON
call_tool() {
  echo "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"$1\",\"arguments\":${2:-\{\}}}}" \
    | ./bin/mcp-server 2>/dev/null | jq -r '.result.content[0].text' | jq .
}

# For tools returning plaintext (pod_logs)
call_tool_text() {
  echo "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"$1\",\"arguments\":${2:-\{\}}}}" \
    | ./bin/mcp-server 2>/dev/null | jq -r '.result.content[0].text'
}

# Get operator pod name for later tests
POD=$(kubectl get pods -n opendatahub-operator-system -o jsonpath='{.items[0].metadata.name}')
```

## Test Cases

| # | Tool | Command | Expected |
|---|------|---------|----------|
| 1a | `platform_health` | `call_tool platform_health '{}'` | JSON with all sections (nodes, deployments, pods, etc.) |
| 1b | `platform_health` | `call_tool platform_health '{"sections":"nodes,operator"}'` | JSON with only `nodes` and `operator` |
| 1c | `platform_health` | `call_tool platform_health '{"layer":"infrastructure"}'` | JSON with infrastructure-layer sections only |
| 2a | `operator_dependencies` | `call_tool operator_dependencies '{}'` | JSON array with status per dependency |
| 2b | `operator_dependencies` | `call_tool operator_dependencies '{"name":"cert-manager"}'` | Single-entry JSON array for cert-manager |
| 3a | `describe_resource` | `call_tool describe_resource '{"apiVersion":"dscinitialization.opendatahub.io/v2","kind":"DSCInitialization","name":"default-dsci"}'` | Full DSCI resource JSON (sensitive data redacted) |
| 3b | `describe_resource` | `call_tool describe_resource "{\"apiVersion\":\"v1\",\"kind\":\"Pod\",\"name\":\"${POD}\",\"namespace\":\"opendatahub-operator-system\"}"` | Full pod resource JSON |
| 4a | `recent_events` | `call_tool recent_events '{}'` | JSON array of events (may be empty if healthy) |
| 4b | `recent_events` | `call_tool recent_events '{"namespace":"opendatahub","since":"1h"}'` | Events from `opendatahub` namespace, last hour |
| 5 | `classify_failure` | `call_tool classify_failure '{}'` | JSON with category, subcategory, error_code, evidence, confidence |
| 6 | `component_status` | `call_tool component_status '{"component":"dashboard"}'` | JSON with CR conditions, pod statuses, deployment readiness |
| 7 | `pod_logs` | `call_tool_text pod_logs "{\"pod_name\":\"${POD}\",\"namespace\":\"opendatahub-operator-system\",\"tail_lines\":10}"` | 10 lines of plaintext log output |

> For test 6, replace `dashboard` with whichever component is `Managed` in your DSC.

## Error Scenarios

| Tool | Command | Expected |
|------|---------|----------|
| `describe_resource` | `call_tool describe_resource '{"apiVersion":"v1","kind":"Pod","name":"does-not-exist","namespace":"opendatahub-operator-system"}'` | Not-found error message |
| `component_status` | `call_tool component_status '{"component":"nonexistent"}'` | Component-not-found error message |
| `pod_logs` | `call_tool_text pod_logs '{"pod_name":"does-not-exist","namespace":"opendatahub-operator-system"}'` | Pod-not-found error message |

## Pass Criteria

1. Valid JSON (or plaintext for `pod_logs`) returned without server crash
2. Expected fields present in responses
3. Error cases return descriptive messages, not stack traces

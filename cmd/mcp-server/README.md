# ODH MCP Server

A [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) server that exposes diagnostic tools for OpenDataHub clusters. It communicates over stdio using JSON-RPC, designed to be called by AI assistants (e.g. Claude Code, VS Code Copilot) or any MCP-compatible client.

## Build & Run

```bash
# Build the binary
make mcp-server

# Run tests
make mcp-server-test
```

The server requires a valid `KUBECONFIG` (or in-cluster config). Namespace defaults can be overridden via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `E2E_TEST_OPERATOR_NAMESPACE` | `opendatahub-operator-system` | Namespace where the ODH operator is deployed |
| `E2E_TEST_APPLICATIONS_NAMESPACE` | `opendatahub` | Namespace where ODH components are deployed |

## Tool Reference

### platform_health

Run cluster health checks and return the full report as JSON. Checks nodes, deployments, pods, events, quotas, operator status, DSCI, and DSC.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `sections` | string | no | all | Comma-separated sections: `nodes`, `deployments`, `pods`, `events`, `quotas`, `operator`, `dsci`, `dsc` |
| `layer` | string | no | all | Comma-separated layers: `infrastructure`, `workload`, `operator`. Ignored if `sections` is set |
| `operator_namespace` | string | no | auto-discover (env → `opendatahub-operator-system`) | Operator namespace |
| `applications_namespace` | string | no | auto-discover (DSCI → env → `opendatahub`) | Applications namespace |

```jsonc
// Example call
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"platform_health","arguments":{"sections":"nodes,operator"}}}

// Example output (truncated)
{
  "nodes": {
    "total": 3,
    "ready": 3,
    "items": [{"name": "node-1", "ready": true, "roles": "control-plane,worker", ...}]
  },
  "operator": {
    "deployment": "opendatahub-operator-controller-manager",
    "ready": true,
    "replicas": 1,
    "readyReplicas": 1
  }
}
```

### operator_dependencies

Check status of dependent operators (cert-manager, Tempo, OpenTelemetry, Kueue, LWS, etc.).

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `operator_namespace` | string | no | auto-discover (env → `opendatahub-operator-system`) | Operator namespace |
| `name` | string | no | all | Filter to a single dependency by name (e.g. `cert-manager`) |

```jsonc
// Example call
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"operator_dependencies","arguments":{}}}

// Example output
[
  {"name": "cert-manager", "installed": true, "healthy": true, "version": "v1.14.0"},
  {"name": "tempo-operator", "installed": false, "healthy": false}
]
```

### describe_resource

Get any Kubernetes resource by apiVersion/kind/name. Returns the full resource as JSON with sensitive data redacted (Secret `.data`, token fields).

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `apiVersion` | string | yes | | API version, e.g. `v1`, `apps/v1`, `datasciencecluster.opendatahub.io/v2` |
| `kind` | string | yes | | Resource kind, e.g. `Pod`, `Deployment`, `DSCInitialization` |
| `name` | string | yes | | Resource name |
| `namespace` | string | no | | Namespace. Omit for cluster-scoped resources |

```jsonc
// Example call
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"describe_resource","arguments":{
  "apiVersion":"dscinitialization.opendatahub.io/v2","kind":"DSCInitialization","name":"default-dsci"
}}}

// Example output (truncated)
{
  "apiVersion": "dscinitialization.opendatahub.io/v2",
  "kind": "DSCInitialization",
  "metadata": {"name": "default-dsci", "creationTimestamp": "2025-01-15T10:00:00Z", ...},
  "spec": {"applicationsNamespace": "opendatahub", ...},
  "status": {"phase": "Ready", "conditions": [...]}
}
```

### recent_events

Warning/error events in ODH namespaces, sorted by last timestamp (most recent first). Auto-discovers ODH namespaces from DSCI if not specified.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `namespace` | string | no | auto-discover | Comma-separated namespaces to query |
| `since` | string | no | `5m` | Go duration for look-back window (e.g. `5m`, `1h`) |
| `event_type` | string | no | all | Filter by type: `Warning`, `Normal` |

```jsonc
// Example call
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"recent_events","arguments":{"since":"1h"}}}

// Example output
[
  {
    "namespace": "opendatahub",
    "name": "dashboard-pod.abc123",
    "kind": "Pod",
    "type": "Warning",
    "reason": "BackOff",
    "message": "Back-off restarting failed container",
    "count": 5,
    "lastTimestamp": "2025-01-15T12:30:00Z"
  }
]
```

### classify_failure

Run cluster health checks and classify the failure deterministically. Returns a structured classification with category, subcategory, error code, evidence, and confidence.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `sections` | string | no | all | Same as `platform_health` |
| `layer` | string | no | all | Same as `platform_health` |
| `operator_namespace` | string | no | auto-discover (env → `opendatahub-operator-system`) | Operator namespace |
| `applications_namespace` | string | no | auto-discover (DSCI → env → `opendatahub`) | Applications namespace |

```jsonc
// Example call
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"classify_failure","arguments":{}}}

// Example output
{
  "category": "component",
  "subcategory": "degraded",
  "error_code": "COMP_DEGRADED",
  "evidence": "Dashboard deployment has 0/1 ready replicas",
  "confidence": 0.9
}
```

### component_status

Get detailed status of a specific ODH component: CR conditions, pod statuses, and deployment readiness.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `component` | string | yes | | Component name: `kserve`, `dashboard`, `workbenches`, `ray`, `trustyai`, `modelregistry`, `datasciencepipelines`, `trainingoperator`, `feastoperator`, `trainer`, `kueue`, `mlflowoperator`, `sparkoperator`, etc. |
| `applications_namespace` | string | no | auto-discover (DSCI → env → `opendatahub`) | Applications namespace |

```jsonc
// Example call
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"component_status","arguments":{"component":"dashboard"}}}

// Example output
{
  "component": "dashboard",
  "crFound": true,
  "conditions": [
    {"type": "Ready", "status": "True", "reason": "Ready", "message": ""}
  ],
  "deployments": [
    {"name": "odh-dashboard", "replicas": 2, "ready": 2}
  ],
  "pods": [
    {"name": "odh-dashboard-abc12", "phase": "Running"},
    {"name": "odh-dashboard-def34", "phase": "Running"}
  ]
}
```

### pod_logs

Retrieve recent logs for a specific pod/container. Returns plaintext (not JSON).

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `pod_name` | string | yes | | Name of the pod |
| `namespace` | string | yes | | Namespace of the pod |
| `container` | string | no | default container | Container name |
| `previous` | boolean | no | `false` | Return logs from the previous container instance |
| `tail_lines` | number | no | `100` | Number of lines from the end of the log |

```jsonc
// Example call
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"pod_logs","arguments":{
  "pod_name":"odh-dashboard-abc12","namespace":"opendatahub","tail_lines":10
}}}
```

```text
// Example output (plaintext, not JSON)
2025-01-15T12:00:01Z INFO  Starting server on :8080
2025-01-15T12:00:02Z INFO  Connected to database
2025-01-15T12:00:03Z INFO  Health check passed
...
```

Output is capped at 50KB. If exceeded, a `[truncated: output exceeded 50KB limit]` marker is appended.

## Client Configuration

**Claude Code:** This repo includes a `.mcp.json` at the project root — no setup needed.

**Cursor / Claude Desktop:** Add to your MCP config (`.cursor/mcp.json` for Cursor, `claude_desktop_config.json` for Claude Desktop):

```json
{
  "mcpServers": {
    "odh-diagnostics": {
      "command": "/absolute/path/to/opendatahub-operator/bin/mcp-server",
      "env": {
        "KUBECONFIG": "/absolute/path/to/.kube/config"
      }
    }
  }
}
```

Build the binary first with `make mcp-server`. The `env` block can be omitted if `KUBECONFIG` is already in your shell environment.

## Integration Testing

For manual testing against a live cluster, see [INTEGRATION_TEST.md](INTEGRATION_TEST.md).

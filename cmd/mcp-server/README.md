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
  "category": "infrastructure",
  "subcategory": "image-pull",
  "error_code": 1001,
  "evidence": ["container xyz waiting: ImagePullBackOff"],
  "confidence": "medium"
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

## Generic Kubernetes Tools (via OpenShift MCP Server)

The following tools are provided by the [OpenShift MCP server](https://github.com/openshift/openshift-mcp-server) (`openshift` server in `.mcp.json`), not the ODH server. They require Node.js (`npx`) or a downloaded binary — see **Client Configuration** below.

> **Security note:** `resources_get` does not redact sensitive fields. Never call it with `kind=Secret`.

### resources_get

Fetch any Kubernetes resource by apiVersion/kind/name.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `apiVersion` | string | yes | e.g. `v1`, `apps/v1`, `datasciencecluster.opendatahub.io/v2` |
| `kind` | string | yes | e.g. `Pod`, `Deployment`, `DSCInitialization` |
| `name` | string | yes | Resource name |
| `namespace` | string | no | Omit for cluster-scoped resources |

### pods_log

Retrieve recent logs for a specific pod/container. Returns plaintext.

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `name` | string | yes | | Name of the pod |
| `namespace` | string | no | | Namespace of the pod |
| `container` | string | no | default container | Container name |
| `previous` | boolean | no | `false` | Return logs from the previous container instance |
| `tail` | number | no | `100` | Number of lines from the end of the log |

## Client Configuration

### Prerequisites

The OpenShift MCP server (`resources_get`, `pods_log`) requires **Node.js** for the `npx` invocation. Alternatively, download a pre-built binary from the [releases page](https://github.com/openshift/openshift-mcp-server/releases) and replace `npx` with the binary path.

### Claude Code

This repo includes a `.mcp.json` at the project root — no setup needed. Both servers (`opendatahub-health` and `openshift`) are pre-configured. The `/diagnose` skill is also pre-configured.

### Claude Desktop

Add the following to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "opendatahub-health": {
      "command": "bash",
      "args": ["-c", "cd /absolute/path/to/opendatahub-operator/cmd/mcp-server && go run ."],
      "env": {
        "KUBECONFIG": "/absolute/path/to/.kube/config"
      }
    },
    "openshift": {
      "command": "npx",
      "args": ["-y", "kubernetes-mcp-server@0.0.63", "--read-only", "--toolsets", "core"],
      "env": {
        "KUBECONFIG": "/absolute/path/to/.kube/config"
      }
    }
  }
}
```

Restart Claude Desktop after saving.

### Cursor

Create or edit `.cursor/mcp.json` in your project root:

```json
{
  "mcpServers": {
    "opendatahub-health": {
      "command": "bash",
      "args": ["-c", "cd /absolute/path/to/opendatahub-operator/cmd/mcp-server && go run ."],
      "env": {
        "KUBECONFIG": "/absolute/path/to/.kube/config"
      }
    },
    "openshift": {
      "command": "npx",
      "args": ["-y", "kubernetes-mcp-server@0.0.63", "--read-only", "--toolsets", "core"],
      "env": {
        "KUBECONFIG": "/absolute/path/to/.kube/config"
      }
    }
  }
}
```

## Troubleshooting

### Server won't start

| Symptom | Cause | Fix |
|---------|-------|-----|
| `kubeconfig: unable to load` | `KUBECONFIG` not set or file missing | Export `KUBECONFIG=/path/to/.kube/config` or add it to the MCP config `env` block |
| `binary not found` | Binary not built | Run `make mcp-server` from the repo root |
| `go: command not found` | Using `.mcp.json` with `go run` but Go not installed | Build the binary with `make mcp-server` and use the binary path instead |

### Tools return errors

| Error message | Cause | Fix |
|---------------|-------|-----|
| `RBAC insufficient` | Kubeconfig user lacks permissions | Ensure the user has a ClusterRole with read access to pods, events, deployments, nodes, and ODH CRDs |
| `CRD not installed` | ODH operator not deployed | Install the ODH operator and create DSCI/DSC CRs |
| `namespace discovery failed` | No DSCI CR on the cluster | Create a `default-dsci` DSCInitialization CR, or set `E2E_TEST_APPLICATIONS_NAMESPACE` env var |

### Agent gives wrong diagnosis

1. **Verify with `oc` commands.** Cross-check the agent's claims manually:
   - `oc get pods -n opendatahub` — are pods actually in the reported state?
   - `oc get dsci default-dsci -o jsonpath='{.status.conditions}'` — is DSCI actually unhealthy?
   - `oc get events -n opendatahub --sort-by=.lastTimestamp` — do events match the agent's evidence?
2. **Check if the component is set to Removed.** If the agent reports a component as failing but it has `managementState: Removed` in the DSC spec, that's expected — not a failure.
3. **Low classifier confidence.** If `classify_failure` returns confidence `low` or category `unknown`, the failure may not match any known pattern. Check the [failure scenarios](scenarios/README.md) for similar patterns, or investigate manually with `pods_log` (OpenShift server) and `recent_events`.
4. **Stale events.** Kubernetes events persist after resources are deleted. The agent may report an event-based issue for a pod that no longer exists. Verify the referenced resource still exists before acting on the diagnosis.

## Integration Testing

For manual testing against a live cluster, see [INTEGRATION_TEST.md](INTEGRATION_TEST.md).

For failure scenario validation, see the [scenarios documentation](scenarios/README.md).

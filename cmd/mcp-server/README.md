# OpenDataHub MCP Health Server

MCP (Model Context Protocol) server that exposes cluster health diagnostic tools for OpenDataHub.

## Tools

### pod_logs

Retrieve recent logs for a specific pod/container.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| pod_name | string | yes | Name of the pod |
| namespace | string | yes | Namespace of the pod |
| container | string | no | Container name. Omit for the default container |
| previous | boolean | no | Return logs from previous container instance. Default: false |
| tail_lines | number | no | Lines from end of log to return. Default: 100 |
| list_containers | boolean | no | Return list of all containers (init, regular, ephemeral) instead of logs. Default: false |

When a container name is invalid, the error response automatically includes the list of available containers.

### platform_health

Run cluster health checks and return report as JSON.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| sections | string | no | Comma-separated sections: nodes,deployments,pods,events,quotas,operator,dsci,dsc |
| layer | string | no | Comma-separated layers: infrastructure,workload,operator. Ignored if sections is set |
| operator_namespace | string | no | Operator namespace. Default: opendatahub-operator-system |
| applications_namespace | string | no | Apps namespace. Default: opendatahub |
| summary | boolean | no | Return compact summary instead of full report. Default: true |

### component_status

Get detailed status of a specific ODH component including managed resources.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| component | string | yes | Component name (e.g. kserve, dashboard, workbenches) |
| applications_namespace | string | no | Apps namespace. Default: opendatahub |

Response includes `managedResources` listing Services, ConfigMaps, ServiceAccounts, and Secrets owned by the component.

### operator_dependencies

Check status of dependent operators (cert-manager, tempo, OTel, etc.).

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| operator_namespace | string | no | Operator namespace. Default: opendatahub-operator-system |
| name | string | no | Filter to specific dependent by name |

### recent_events

Warning/error events in ODH namespaces sorted by last timestamp.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| namespace | string | no | Comma-separated namespaces. Omit to auto-discover from DSCI |
| since | string | no | Go duration for look-back window (e.g. 5m, 1h). Default: 5m |
| event_type | string | no | Filter by type: Warning, Normal. Omit for all |

Event output includes a `count` field showing how many times the event occurred.

### describe_resource

Get any Kubernetes resource by apiVersion/kind/name. Returns full resource as JSON with sensitive data redacted.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| apiVersion | string | yes | API version (e.g. v1, apps/v1) |
| kind | string | yes | Resource kind (e.g. Pod, Deployment) |
| name | string | yes | Resource name |
| namespace | string | no | Namespace (omit for cluster-scoped resources) |

### classify_failure

Run health checks and classify the failure deterministically.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| sections | string | no | Comma-separated sections to check |
| layer | string | no | Comma-separated layers to check |
| operator_namespace | string | no | Operator namespace. Default: opendatahub-operator-system |
| applications_namespace | string | no | Apps namespace. Default: opendatahub |

## Running

```bash
cd cmd/mcp-server && go run .
```

## Testing

```bash
cd cmd/mcp-server && go test -v ./...
```

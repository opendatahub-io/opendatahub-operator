# Namespace-Restricted Prometheus Metrics Access

## Overview

The `data-science-prometheus-restricted` deployment provides secure, namespace-scoped access to Prometheus metrics through a two-proxy architecture that enforces authentication, authorization, and query filtering.

## Architecture

```
User Request
    ↓
kube-rbac-proxy (port 8443)
    ├─ Authenticates via bearer token (TokenReview)
    ├─ Extracts 'namespace' query parameter
    ├─ Performs SubjectAccessReview for metrics.k8s.io/pods in that namespace
    └─ Denies if unauthorized (403 Forbidden)
    ↓
prom-label-proxy (port 9091)
    ├─ Validates namespace parameter is present (400 if missing)
    ├─ Rewrites PromQL queries to inject namespace label: {namespace="value"}
    └─ Forwards to upstream Prometheus
    ↓
Prometheus (prometheus-operated service on port 9090)
```

## Components

### 1. kube-rbac-proxy

**Purpose**: Authenticates users and performs authorization checks

**How it works**:

1. Extracts the `namespace` query parameter from incoming requests
2. Validates the bearer token via TokenReview API call to Kubernetes API server
3. Performs SubjectAccessReview for `metrics.k8s.io/pods` in the specified namespace, with the verb derived from the HTTP method (`GET`→`get`, `POST`→`create`)
4. Allows (proxies to prom-label-proxy) or denies (403 Forbidden) based on the result

**Configuration**: `data-science-prometheus-restricted-config` ConfigMap

### 2. prom-label-proxy

**Purpose**: Enforces namespace isolation at the query level

**How it works**:

1. Receives proxied requests from kube-rbac-proxy (already authenticated/authorized)
2. Validates that the `namespace` parameter is present (returns 400 Bad Request if missing)
3. Rewrites the PromQL query to inject namespace label filter
4. Forwards the rewritten query to upstream Prometheus

**Example query transformation**:

```
Original query:  up
Request:         GET /api/v1/query?query=up&namespace=my-namespace
Rewritten query: up{namespace="my-namespace"}
```

This ensures users can ONLY see metrics from namespaces they're authorized for, even if they try to craft queries with different namespace labels.

## Required Query Format

**All queries MUST include the namespace parameter:**

```bash
GET /api/v1/query?query=<promql>&namespace=<namespace-name>
```

Examples:

```bash
# ✅ Correct
curl "https://prometheus-route/api/v1/query?query=up&namespace=my-namespace" \
  -H "Authorization: Bearer $(oc whoami -t)"

# ❌ Incorrect - Missing namespace parameter
curl "https://prometheus-route/api/v1/query?query=up" \
  -H "Authorization: Bearer $(oc whoami -t)"
```

## Common Errors and Troubleshooting

### 1. HTTP 400 Bad Request - Missing namespace parameter

**Error Message:**

```
Bad Request. The request or configuration is malformed.
```

**Cause**: Query is missing the required `namespace` parameter

**Example**:

```bash
curl "https://.../api/v1/query?query=up"
```

**Fix**: Add the namespace parameter

```bash
curl "https://.../api/v1/query?query=up&namespace=my-namespace"
```

**Technical Detail**: The prom-label-proxy requires the namespace parameter to perform query rewriting. Without it, it cannot inject the namespace label filter.

---

### 2. HTTP 403 Forbidden - Insufficient permissions

**Error Message:**

```text
Forbidden (user=<username>, verb=get, resource=pods, subresource=)
```

or (for POST requests, e.g. from Perses):

```text
Forbidden (user=<username>, verb=create, resource=pods, subresource=)
```

**Cause**: User lacks permissions for `metrics.k8s.io/pods` resource in the requested namespace. The verb in the error corresponds to the HTTP method used (GET→`get`, POST→`create`).

**Diagnostic**:

```bash
# Check who has metrics permissions (use the verb from the error message)
oc adm policy who-can get pods.metrics.k8s.io -n my-namespace
oc adm policy who-can create pods.metrics.k8s.io -n my-namespace
```

**Fix**: Grant appropriate role

```bash
# Option 1: Grant cluster-monitoring-view (metrics-only access)
oc adm policy add-role-to-user cluster-monitoring-view alice -n my-namespace

# Option 2: Grant view (read-only access including metrics)
oc adm policy add-role-to-user view alice -n my-namespace

# Option 3: Grant edit (read-write access including metrics)
oc adm policy add-role-to-user edit alice -n my-namespace
```

**Note**: OpenShift's built-in roles (view, edit, admin) include `get` permissions for `metrics.k8s.io`. The operator's `data-science-metrics-view` ClusterRole aggregates the `create` verb into these roles to support POST-based clients like Perses.

---

### 3. HTTP 401 Unauthorized

**Error Message:**

```
Unauthorized
```

**Cause**: Missing or invalid bearer token

**Fix**: Include a valid authentication token

```bash
TOKEN=$(oc whoami -t)
curl "https://.../api/v1/query?query=up&namespace=my-namespace" \
  -H "Authorization: Bearer $TOKEN"
```

---

### 4. Empty Results (HTTP 200 but no data)

**Response**:

```json
{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": []
  }
}
```

**Possible Causes**:

1. No metrics are being scraped in that namespace
2. Wrong namespace name (typo)
3. No pods running or exposing metrics
4. Metrics haven't been collected yet

**Diagnostic**:

```bash
# Verify namespace exists and has pods
oc get pods -n my-namespace

# Check if ServiceMonitors are configured
oc get servicemonitors -n my-namespace

# Verify namespace spelling
oc get namespaces | grep my-namespace
```

## Authorization Model

### SubjectAccessReview Details

The kube-rbac-proxy config omits `verb` from `resourceAttributes`, so it derives the verb from the HTTP method of each incoming request:

| HTTP Method | SAR Verb | Used By |
|-------------|----------|---------|
| GET         | `get`    | curl, browser, Grafana |
| POST        | `create` | Perses, large PromQL queries |

```yaml
resourceAttributes:
  apiGroup: metrics.k8s.io
  resource: pods
  namespace: "<value from query parameter>"
  # verb is omitted — derived from HTTP method (GET→get, POST→create)
```

This is **different** from checking pod visibility (core API):

```yaml
resourceAttributes:
  apiGroup: "" # Core API
  resource: pods
  namespace: "<value from query parameter>"
  verb: get
```

### Which Roles Grant Access

OpenShift built-in roles that include `metrics.k8s.io/pods` permission:

- ✅ `view` - Read-only access including metrics (`get`, `list`, `watch`)
- ✅ `edit` - Modify namespace resources including metrics access (`get`, `list`, `watch`)
- ✅ `admin` - Full namespace control including metrics access (`get`, `list`, `watch`)
- ✅ `cluster-monitoring-view` - Metrics-specific read access

These built-in roles cover **GET** requests. For **POST** requests (used by Perses), the operator deploys an aggregated `data-science-metrics-view` ClusterRole that adds the `create` verb to `view`, `edit`, and `admin`. The `metrics.k8s.io` API is read-only, so granting `create` has no side-effects — it only satisfies the SAR authorization check.

Custom roles need explicit grants:

```yaml
rules:
  - apiGroups: ["metrics.k8s.io"]
    resources: ["pods"]
    verbs: ["get", "create"]
```

## Security Features

### Defense in Depth

The deployment implements multiple security layers:

1. **Network Level**:

   - NetworkPolicy restricts ingress to OpenShift router and Alertmanager only
   - TLS termination with certificate validation

2. **Authentication Level** (kube-rbac-proxy):

   - Bearer token validation via Kubernetes TokenReview
   - Delegated authentication via `system:auth-delegator` ClusterRole

3. **Authorization Level** (kube-rbac-proxy):

   - SubjectAccessReview for every request
   - Namespace-scoped permission checks

4. **Query Level** (prom-label-proxy):

   - Mandatory namespace parameter
   - Query rewriting to inject namespace filters
   - Prevents cross-namespace metric visibility

5. **Container Level**:

   - readOnlyRootFilesystem
   - No privilege escalation
   - All capabilities dropped
   - Runs as non-root

6. **RBAC Level**:
   - ServiceAccount with minimal permissions (only cluster-monitoring-view)
   - No cluster-admin or elevated privileges

### Resource Limits

Conservative resource limits to handle typical query loads:

| Container        | CPU Limit | Memory Limit | CPU Request | Memory Request |
| ---------------- | --------- | ------------ | ----------- | -------------- |
| kube-rbac-proxy  | 100m      | 128Mi        | 50m         | 64Mi           |
| prom-label-proxy | 100m      | 128Mi        | 50m         | 64Mi           |

**Rationale**: Provides ~40% headroom for:

- Concurrent SubjectAccessReview API calls
- Complex PromQL query parsing and rewriting
- TLS termination and certificate validation
- Connection handling under load

**Recommendation**: Monitor actual usage and tune based on your query patterns and concurrency levels.

## Debugging

### Check kube-rbac-proxy Logs

```bash
oc logs -n <monitoring-namespace> deployment/data-science-prometheus-restricted -c kube-rbac-proxy --tail=50
```

Look for:

- `Unable to authenticate the request` - TokenReview failures
- `Failed to make webhook authenticator request` - API server connectivity issues
- Authorization denials with user/namespace details

### Check prom-label-proxy Logs

```bash
oc logs -n <monitoring-namespace> deployment/data-science-prometheus-restricted -c prom-label-proxy --tail=50
```

Look for:

- Query rewriting errors
- Upstream connection failures
- Namespace parameter validation errors

### Test SubjectAccessReview Directly

```bash
# Test GET access (verb: get)
cat <<EOF | oc create -f -
apiVersion: authorization.k8s.io/v1
kind: SubjectAccessReview
spec:
  resourceAttributes:
    namespace: my-namespace
    verb: get
    group: metrics.k8s.io
    resource: pods
  user: alice
EOF

# Test POST access (verb: create) — needed for Perses
cat <<EOF | oc create -f -
apiVersion: authorization.k8s.io/v1
kind: SubjectAccessReview
spec:
  resourceAttributes:
    namespace: my-namespace
    verb: create
    group: metrics.k8s.io
    resource: pods
  user: alice
EOF
```

Check the output - `"allowed": true` means the user has permission.

### Verify NetworkPolicy

```bash
# Check ingress rules
oc get networkpolicy data-science-prometheus-proxy-ingress -n <monitoring-namespace> -o yaml

# Verify pod selector matches
oc get pods -n <monitoring-namespace> -l app=data-science-prometheus-restricted
```

## Related Documentation

- [Monitoring RBAC Documentation](MONITORING_RBAC.md) - General monitoring access control
- [Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
- [kube-rbac-proxy Documentation](https://github.com/brancz/kube-rbac-proxy)
- [prom-label-proxy Documentation](https://github.com/prometheus-community/prom-label-proxy)

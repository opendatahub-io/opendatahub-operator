# Migration Guide: Models as a Service to AIGateway (3.5)

## Overview

In OpenShift AI 3.5, Models as a Service (MaaS) configuration has moved from `kserve.modelsAsService` to `aigateway.modelsAsAService`.

✅ **No immediate action required** — existing `kserve.modelsAsService` configurations continue to work through 3.6.

The operator respects `kserve.modelsAsService` for backward compatibility through 3.6. Migrate to `aigateway.modelsAsAService` before upgrading to 3.7.

## Configuration Change

**Old (3.4 and earlier):**
```yaml
spec:
  components:
    kserve:
      managementState: Managed
      modelsAsService:
        managementState: Managed
```

**New (3.5+):**
```yaml
spec:
  components:
    aigateway:
      managementState: Managed
      modelsAsAService:
        managementState: Managed
```

## What Happens Automatically

When you upgrade to OpenShift AI 3.5:

1. **Webhook conversion** automatically detects `kserve.modelsAsService: Managed`
2. **AIGateway is enabled** with `managementState: Managed`
3. **MaaS config is moved** to `aigateway.modelsAsAService`
4. **Old config is preserved** in `kserve.modelsAsService` — read-only (CEL `self == oldSelf`), pruned automatically in 3.7
5. **ai-gateway-operator** takes over MaaS deployment (instead of platform operator directly)

## Upgrade Path (3.4 → 3.5)

### Standard users (kubectl / oc)

No action needed on upgrade — MaaS continues to deploy. Migrate at your own pace before 3.7:

```bash
oc patch datasciencecluster default-dsc --type=merge -p '
spec:
  components:
    aigateway:
      managementState: Managed
      modelsAsAService:
        managementState: Managed
    kserve:
      modelsAsService:
        managementState: ""
'
```

### GitOps users (ArgoCD, Flux)

MaaS continues to work with your existing git manifest — no immediate change needed. When you are ready to migrate, update your git manifest and let your GitOps tool sync it. Replace:

```yaml
# Old (3.4)
components:
  kserve:
    modelsAsService:
      managementState: Managed
```

with:

```yaml
# New (3.5+)
components:
  aigateway:
    managementState: Managed
    modelsAsAService:
      managementState: Managed
```

Then apply via your GitOps tool. The old field is read-only in 3.5 — setting it on new clusters is rejected.

### Automatic webhook migration (v1 DSC only)

If you still use `apiVersion: datasciencecluster.opendatahub.io/v1`, the conversion webhook automatically moves `kserve.modelsAsService` → `aigateway.modelsAsAService` on admission.

## Verification

After upgrade, verify the migration succeeded:

```bash
# Check AIGateway module is created
oc get aigateway default-aigateway

# Check ai-gateway-operator is running  
oc get deployment -n opendatahub ai-gateway-operator

# Check MaaS config in new location
oc get datasciencecluster default-dsc -o jsonpath='{.spec.components.aigateway.modelsAsAService}'
```

## Benefits of New Architecture

- **Independent from KServe**: MaaS no longer requires KServe to be enabled
- **Unified Gateway Management**: All gateway components (Batch, MaaS) under one umbrella
- **Dedicated Operator**: ai-gateway-operator handles MaaS lifecycle

## Rollback

To disable MaaS after migration:

```bash
oc patch datasciencecluster default-dsc --type=merge -p '
spec:
  components:
    aigateway:
      modelsAsAService:
        managementState: Removed
'
```

## Important Notes

- **Data preserved**: Existing Tenants, Subscriptions, API keys remain intact
- **No downtime**: Migration happens automatically without service interruption
- **One-way migration**: After conversion, MaaS config exists only at `aigateway.modelsAsAService` (not backward compatible with 3.4)

## Troubleshooting

**MaaS not deploying after upgrade?**

Check if AIGateway is enabled:
```bash
oc get datasciencecluster default-dsc -o jsonpath='{.spec.components.aigateway.managementState}'
# Should return: Managed
```

Check ai-gateway-operator logs:
```bash
oc logs -n opendatahub deployment/ai-gateway-operator
```

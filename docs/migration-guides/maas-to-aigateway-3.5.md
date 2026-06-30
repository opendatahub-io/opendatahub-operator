# Migration Guide: Models as a Service to AIGateway (3.5)

## Overview

In OpenShift AI 3.5, Models as a Service (MaaS) configuration has moved from `kserve.modelsAsService` to `aigateway.modelsAsService`.

✅ **Automatic migration** - the operator automatically migrates your configuration via webhook conversion.

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
      modelsAsService:
        managementState: Managed
```

## What Happens Automatically

When you upgrade to OpenShift AI 3.5:

1. **Webhook conversion** automatically detects `kserve.modelsAsService: Managed`
2. **AIGateway is enabled** with `managementState: Managed`
3. **MaaS config is moved** to `aigateway.modelsAsService`
4. **Old config is cleared** from `kserve.modelsAsService` (migrated, not duplicated)
5. **ai-gateway-operator** takes over MaaS deployment (instead of platform operator directly)

## No Action Required

The migration is **fully automatic**. You do not need to manually edit your DataScienceCluster CR.

## Verification

After upgrade, verify the migration succeeded:

```bash
# Check AIGateway module is created
oc get aigateway default-aigateway

# Check ai-gateway-operator is running  
oc get deployment -n opendatahub ai-gateway-operator

# Check MaaS config in new location
oc get datasciencecluster default-dsc -o jsonpath='{.spec.components.aigateway.modelsAsService}'
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
      modelsAsService:
        managementState: Removed
'
```

## Important Notes

- **Data preserved**: Existing Tenants, Subscriptions, API keys remain intact
- **No downtime**: Migration happens automatically without service interruption
- **One-way migration**: After conversion, MaaS config exists only at `aigateway.modelsAsService` (not backward compatible with 3.4)

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

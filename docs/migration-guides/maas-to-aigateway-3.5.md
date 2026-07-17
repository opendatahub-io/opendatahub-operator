# Migration Guide: Models as a Service to AIGateway (3.5)

## Overview

In OpenShift AI 3.5, Models as a Service (MaaS) configuration has moved from `kserve.modelsAsService` to `aigateway.modelsAsAService`.

✅ **No immediate action required** — existing `kserve.modelsAsService` configurations continue to work at least through 3.6.

The operator respects `kserve.modelsAsService` for backward compatibility at least through 3.6.

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

### v2 DSC upgrade (most users — no DSC rewrite)

If your cluster stores a v2 DSC with `kserve.modelsAsService: Managed` (the 3.4 state), the operator reads it at startup via the `IsEnabled()` fallback and `BuildModuleCR()` — **no conversion webhook runs, no DSC fields are rewritten**:

1. **`IsEnabled()` fallback** detects `kserve.modelsAsService: Managed` when `aigateway.managementState` is not yet set
2. **AIGateway module is provisioned** — ai-gateway-operator deployed with `modelsAsAService: Managed` in the AIGateway CR
3. **DSC is untouched** — `kserve.modelsAsService` stays as-is in etcd; GitOps sees no drift
4. **`oc`/`kubectl` Warning** is emitted on any create/update while `kserve.modelsAsService` remains `Managed`

### v1 API write (conversion webhook path)

If a client submits `apiVersion: v1` with `kserve.modelsAsService: Managed`:

1. **Conversion webhook** migrates `kserve.modelsAsService` → `aigateway.modelsAsAService` in the stored v2
2. **Old field is preserved** — CEL allows clearing to `Removed` after migration, but blocks re-enabling (`Removed→Managed`)
3. **`oc`/`kubectl` Warning** is emitted
4. **ai-gateway-operator** takes over MaaS deployment

## Upgrade Path (3.4 → 3.5)

### Standard users (kubectl / oc)

No action needed on upgrade — MaaS continues to deploy.

When ready, apply the following **v2 API** patch to move to the new field and clear the old one:

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
        managementState: Removed
'
```

While `kserve.modelsAsService` is still `Managed`, `oc` prints a deprecation Warning. After you set it to `Removed`, the Warning stops. Re-enabling it (`Removed→Managed`) is rejected by CEL.

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
  kserve:
    modelsAsService:
      managementState: Removed   # optional cleanup; Managed→Removed allowed
```

Do **not** re-enable the old field after clearing it — CEL rejects `Removed→Managed`. Use `aigateway.modelsAsAService` instead.

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

If the AIGateway module should also be torn down:

```bash
oc patch datasciencecluster default-dsc --type=merge -p '
spec:
  components:
    aigateway:
      managementState: Removed
'
```

Use `Removed`, not omit/`""`. Empty `aigateway.managementState` re-triggers the legacy `kserve.modelsAsService` fallback.

## Important Notes

- **Data preserved**: Existing Tenants, Subscriptions, API keys remain intact
- **No downtime**: Migration happens automatically without service interruption
- **One-way migration**: After conversion, MaaS config exists only at `aigateway.modelsAsAService` (not backward compatible with 3.4)
- **CEL on deprecated field**: `Managed→Removed` allowed (cleanup); `Removed→Managed` blocked

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

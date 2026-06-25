# Migration Guide: Models as a Service to AIGateway (3.5)

## Overview

In OpenShift AI 3.5, Models as a Service (MaaS) configuration has moved from `kserve.modelsAsService` to `aigateway.modelsasservice`.

⚠️ **Manual migration required** - the operator will NOT automatically migrate your configuration.

## Configuration Change

**Old (3.4):**
```yaml
spec:
  components:
    kserve:
      managementState: Managed
      modelsAsService:
        managementState: Managed
```

**New (3.5):**
```yaml
spec:
  components:
    aigateway:
      managementState: Managed
      modelsasservice:
        managementState: Managed
```

## Migration Steps

### 1. Update DataScienceCluster

```bash
oc edit datasciencecluster default-dsc
```

Remove the old section:
```yaml
kserve:
  modelsAsService:        # DELETE THIS
    managementState: Managed
```

Add the new section:
```yaml
aigateway:
  managementState: Managed
  modelsasservice:        # ADD THIS
    managementState: Managed
```

### 2. Verify

Check AIGateway is created:
```bash
oc get aigateway default-aigateway
```

Check ai-gateway-operator is running:
```bash
oc get deployment -n ai-gateway-system ai-gateway-operator
```

## Important Notes

- **No backward compatibility**: Old `kserve.modelsAsService` location is ignored in 3.5+
- **Data preserved**: Existing Tenants, Subscriptions remain intact
- **KServe independent**: MaaS no longer requires KServe to be enabled

## Troubleshooting

**MaaS not deploying?** Verify you removed the old configuration:
```bash
oc get datasciencecluster default-dsc -o jsonpath='{.spec.components.kserve.modelsAsService}'
# Should return empty
```

For detailed documentation, see:
- [MaaS Enablement Guide](../enabling-models-as-a-service.md)
- [AIGateway Architecture](../architecture.md)

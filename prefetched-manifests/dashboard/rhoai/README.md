# RHOAI Manifests

These manifests are only for RHOAI. Overrides can be performed on the manifest files in [`../base`](../base/).

## Deployment modes

| Mode | Entry point | Notes |
|---|---|---|
| Sidecar (all BFF modules in main pod) | `kustomize build manifests/rhoai` | Default; includes modular-architecture sidecar patches |
| Standalone (BFF modules as independent pods) | `kustomize build manifests/rhoai/standalone` | Rendered by the dashboard-operator when `deploymentMode: Standalone` |

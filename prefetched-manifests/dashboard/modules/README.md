# Standalone module manifests

Each directory is a **complete, independently deployable** kustomize package for a BFF module running as its own Kubernetes Deployment. These are used by the Dashboard Module Controller when `spec.deploymentMode: Standalone` is set on the Dashboard CR.

```bash
# One module
kustomize build manifests/modules/gen-ai

# All modules
kustomize build manifests/modules
```

## Modules

| Module | Port | Service name | No DSC gate? | Directory |
|--------|------|--------------|--------------|-----------|
| model-registry | 8043 | `odh-dashboard-model-registry-ui` | Requires `modelregistry` | [model-registry/](model-registry/) |
| gen-ai | 8143 | `odh-dashboard-gen-ai-ui` | No gate | [gen-ai/](gen-ai/) |
| maas | 8243 | `odh-dashboard-maas-ui` | No gate | [maas/](maas/) |
| mlflow | 8343 | `odh-dashboard-mlflow-ui` | Requires `mlflowoperator` | [mlflow/](mlflow/) |
| eval-hub | 8543 | `odh-dashboard-eval-hub-ui` | Requires `trustyai` | [eval-hub/](eval-hub/) |
| automl | 8643 | `odh-dashboard-automl-ui` | Requires `aipipelines` | [automl/](automl/) |
| autorag | 8743 | `odh-dashboard-autorag-ui` | Requires `aipipelines` | [autorag/](autorag/) |
| agent-ops | 8843 | `odh-dashboard-agent-ops-ui` | No gate (feature-flag only) | [agent-ops/](agent-ops/) |

## Naming conventions

- **Deployment `metadata.name`** and **pod label `deployment:`** use `<slug>-ui` (e.g. `model-registry-ui`) to avoid conflicts with operator-managed resources of the same short name.
- **`Service.metadata.name`** uses `odh-dashboard-<slug>-ui`. RHOAI uses `rhods-dashboard-<slug>-ui`.
- **ServiceAccount, ClusterRole, ClusterRoleBinding** use `odh-dashboard-<slug>`.
- **TLS Secret** referenced in the Service annotation and Deployment volume is `<slug>-proxy-tls`.

## Per-module layout

Each directory contains:

```
<slug>/
├── kustomization.yaml     # Kustomize entry point with configMapGenerator for params
├── params.env             # Container image default (updated by release workflow)
├── params.yaml            # kustomize var reference config
├── deployment.yaml        # Standalone Deployment (2 replicas, TLS, SA isolation)
├── service.yaml           # Service exposing the BFF port with serving-cert annotation
├── networkpolicy.yaml     # Ingress from OpenShift ingress; egress to DNS, K8s API, external HTTPS
├── service-account.yaml   # Dedicated SA with automountServiceAccountToken: false
├── cluster-role.yaml      # Least-privilege ClusterRole for this BFF
└── cluster-role-binding.yaml
```

## DSC component gating

The Dashboard Module Controller (`dashboard-operator`) gates each module against DSC component availability. Modules with no DSC gate (`gen-ai`, `maas`, `agent-ops`) are deployed whenever the Dashboard CR is in Managed/Standalone state. Modules with a DSC gate are only deployed when the corresponding component is set to `Managed` in the Dashboard CR `spec.components` map.

## Sidecar counterparts

The sidecar JSON6902 patches for the same modules live at [`../sidecar/deployment.yaml`](../sidecar/deployment.yaml). In sidecar mode all BFFs run as containers inside the main `odh-dashboard` pod; in standalone mode each runs as its own pod from this directory.

Do **not** apply both overlays simultaneously — that runs two copies of each BFF.

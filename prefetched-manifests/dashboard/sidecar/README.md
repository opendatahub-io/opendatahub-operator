# Sidecar overlay

JSON6902 patches that inject BFF module containers into the main `odh-dashboard` pod for **sidecar deployment mode**.

In sidecar mode all eight BFF modules run as containers alongside `odh-dashboard` and `kube-rbac-proxy` in a single pod. This is the legacy deployment path used when the Dashboard Module Controller is not available or the Dashboard CR has `spec.deploymentMode: Sidecar` (the default).

The standalone module manifests live at [`../modules/`](../modules/).

## Files

| File | Role |
|------|------|
| `deployment.yaml` | JSON6902 patches: SA isolation (`automountServiceAccountToken: false`, projected `dashboard-sa-token`), federation config env var, and all eight BFF sidecar containers |
| `service.yaml` | JSON6902 patches adding BFF module ports to the shared `odh-dashboard` Service |
| `networkpolicy.yaml` | Ingress/egress for all ports on the dashboard pod (8443 + all BFF ports) |
| `federation-configmap.yaml` | Static `federation-config` ConfigMap for sidecar mode |
| `modules-service-account.yaml` | Shared `odh-dashboard-modules` ServiceAccount for in-pod token access |
| `modules-cluster-role.yaml` | ClusterRole for the shared modules SA |
| `modules-cluster-role-binding.yaml` | Binds the modules ClusterRole to the shared SA |
| `modules-sa-token-secret.yaml` | Projected token Secret for module SA |
| `params.env` | BFF module container image defaults (injected by operator at install) |
| `params.yaml` | kustomize var reference config |

## BFF modules (eight containers, sidecar mode)

| Container | Port |
|-----------|------|
| `model-registry-ui` | 8043 |
| `gen-ai-ui` | 8143 |
| `maas-ui` | 8243 |
| `mlflow-ui` | 8343 |
| `eval-hub-ui` | 8543 |
| `automl-ui` | 8643 |
| `autorag-ui` | 8743 |
| `agent-ops-ui` | 8843 |

## Usage

This overlay is included by `manifests/odh/` (ODH) and `manifests/rhoai/base/` (RHOAI). Build the full sidecar manifest:

```bash
kustomize build manifests/odh   # ODH sidecar mode
kustomize build manifests/rhoai # RHOAI sidecar mode
```

For standalone mode (modules as independent pods), the operator renders `manifests/odh/standalone/` or `manifests/rhoai/standalone/` instead, and deploys each module from `manifests/modules/<slug>/`.

# TLS Backend Overlay

Enables end-to-end TLS for maas-api using OpenShift serving certificates.

## Contents

| File | Purpose |
|------|---------|
| `kustomization.yaml` | References base TLS overlay and policies, applies HTTPS patches |
| `configure-authorino-tls.sh` | Configures operator-managed Authorino for TLS |


## Traffic Flow

**External (client → gateway → maas-api):**

```
Client :443 → Gateway (TLS termination) → DestinationRule → maas-api :8443
```

**Internal (Authorino → maas-api for tier lookup):**

```
Authorino → maas-api :8443 → /v1/tiers/lookup
```

## Usage

```bash
# Apply Kustomize overlay
kustomize build deployment/overlays/tls-backend | kubectl apply -f -

# Configure Authorino for TLS (operator-managed, can't be patched via Kustomize)
./deployment/overlays/tls-backend/configure-authorino-tls.sh

# Restart to pick up certificates
kubectl rollout restart deployment/maas-api -n maas-api
kubectl rollout restart deployment/authorino -n kuadrant-system
```

## Why the script?

Authorino resources are managed by the Kuadrant operator. Kustomize can't patch them because they don't exist in our manifests; they're created by the operator. The script uses `kubectl patch` to configure TLS on the live resources.

## See also

- [Securing Authorino for llm-d in RHOAI](https://github.com/opendatahub-io/kserve/tree/release-v0.15/docs/samples/llmisvc/ocp-setup-for-GA#ssl-authorino)

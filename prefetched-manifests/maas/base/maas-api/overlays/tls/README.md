# MaaS API TLS Overlay

Enables HTTPS for the maas-api service using OpenShift serving certificates.

## Features

- Configures maas-api to serve HTTPS on port 8443
- Uses OpenShift's `service.beta.openshift.io/serving-cert-secret-name` annotation
- OpenShift automatically provisions and rotates TLS certificates
- Includes DestinationRule for gateway→maas-api TLS origination

## Resources

| Resource | Purpose |
|----------|---------|
| `deployment-patch.yaml` | Configure maas-api container for TLS |
| `service-patch.yaml` | Add serving-cert annotation, expose port 8443 |
| `destinationrule.yaml` | Configure gateway TLS to maas-api backend |

## Why DestinationRule?

DestinationRule is the standard "pre-BackendTLSPolicy" workaround when using Istio as the Gateway API provider.

**The problem:** Gateway API's HTTPRoute doesn't tell Istio "use TLS to the backend". Without [BackendTLSPolicy](https://gateway-api.sigs.k8s.io/api-types/backendtlspolicy/) (GA in Gateway API v1.4), you need an Istio-native policy object to configure TLS origination.

**The solution:** DestinationRule tells the gateway's Envoy proxy how to talk to the backend:
- TLS origination from gateway → maas-api over HTTPS
- Controls TLS/mTLS settings for traffic leaving the gateway proxy

```
Client → Gateway (TLS termination) → [DestinationRule] → maas-api:8443 (TLS origination)
```

> **Future:** Once Gateway API v1.4+ with BackendTLSPolicy is supported, this DestinationRule can be replaced with a standard Gateway API resource.

## Usage

### Standalone (maas-api with TLS only)

```bash
kustomize build deployment/overlays/tls | kubectl apply -f -
```

### As part of full TLS backend

This overlay is referenced by `overlays/tls-backend` which adds:
- Authorino TLS configuration
- HTTPRoute port patches for HTTPS backend
- Service CA bundle for inter-service trust

## Certificate Management

OpenShift's service-ca controller automatically:
1. Creates `maas-api-serving-cert` secret when service is annotated
2. Rotates certificates before expiration
3. Updates the secret in-place (pods need restart to pick up new certs)


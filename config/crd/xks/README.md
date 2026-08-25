# XKS GatewayConfig CRD

This directory holds the **XKS-tailored** `GatewayConfig` CRD. The shared operator API
(`api/services/v1alpha1`, `api/infrastructure/v1`) and the OCP bundle CRD stay
OpenShift-centric (`OpenshiftDefaultIngress`, `OcpRoute`).

Regenerate after API changes:

```bash
make generate-xks-gateway-crd
```

Sync into odh-gitops (`xks-gateway` chart, not `rhai-on-xks-chart`):

```bash
./charts/dependencies/xks-gateway/scripts/sync-gatewayconfig-crd.sh /path/to/opendatahub-operator
```

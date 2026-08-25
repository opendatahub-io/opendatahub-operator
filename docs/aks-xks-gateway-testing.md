# AKS / XKS Gateway testing

Platform auth gateway (`GatewayConfig`) on vanilla Kubernetes (AKS, EKS, and other XKS clusters).

The operator hardcodes the XKS gateway namespace as `rh-ai-gateway`. Keep `gateway.namespace` at that value in the `xks-gateway` Helm chart.

## Install order

1. **GatewayConfig CRD** — install the `xks-gateway` chart (or apply the XKS CRD from `config/crd/xks/`). Do not rely on `rhai-on-xks-chart` for this CRD.
2. **Operator** — install `rhai-on-xks-chart` with `rhaiOperator.gatewayService.enabled=true` so the GatewayConfig controller reconciles.
3. **GatewayConfig** — created by `xks-gateway` (same chart as the CRD) once domain and OIDC values are set.

Example:

```bash
# 1 + 3: CRD, namespace rh-ai-gateway, and GatewayConfig
helm upgrade --install xks-gateway ./charts/dependencies/xks-gateway \
  --set gateway.domain=example.com \
  --set gateway.oidc.issuerURL=https://keycloak.example.com/realms/rhai \
  --set gateway.oidc.clientID=rhai-client \
  --set gateway.oidc.clientSecretRef.name=my-oidc-secret

# 2: operator with gateway reconciliation enabled
helm upgrade --install rhaii ./charts/rhai-on-xks-chart \
  --set azure.enabled=true \
  --set rhaiOperator.gatewayService.enabled=true \
  ...
```

If the operator is installed first, enable `gatewayService` only after the CRD exists, or install `xks-gateway` with `installCRDs=true` before the operator.

## Provider CA secret (`providerCASecretName`)

When the OIDC/OAuth provider uses a private or self-signed CA:

1. Create a Secret in **`rh-ai-gateway`** with key **`ca.crt`** (PEM).
2. Set `spec.providerCASecretName` (Helm: `gateway.providerCASecretName`) to that Secret name.

```bash
kubectl create secret generic oidc-provider-ca \
  --namespace rh-ai-gateway \
  --from-file=ca.crt=/path/to/provider-ca.crt
```

```yaml
# xks-gateway values
gateway:
  providerCASecretName: oidc-provider-ca
  verifyProviderCertificate: true
```

`oidc.secretNamespace` defaults to the gateway namespace (`rh-ai-gateway`) when unset.

## Unsupported OpenShift values on XKS

The XKS CRD and Helm validation reject:

| Field | Unsupported on XKS | Use instead |
| --- | --- | --- |
| `spec.certificate.type` | `OpenshiftDefaultIngress` | `SelfSigned` (default) or `Provided` |
| `spec.ingressMode` | `OcpRoute` | `LoadBalancer` (default) |

If a GatewayConfig is applied with those values (for example using the OpenShift CRD), the controller sets `Ready=False` with a message naming the invalid field.

Omitting `spec.certificate` on the XKS CRD applies `certificate: {}` then nested `type: SelfSigned`. Do not install the OpenShift GatewayConfig CRD on XKS; its default is `OpenshiftDefaultIngress`.

## Namespace

Gateway workloads, the provider CA Secret, and the default OIDC client secret location must be **`rh-ai-gateway`**. The operator does not honor a different `gateway.namespace` from the chart.

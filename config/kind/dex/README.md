# Dex OIDC provider for KinD XKS e2e

KinD gateway e2e tests deploy a minimal [Dex](https://dexidp.io/) instance so
`kube-auth-proxy` can complete OIDC discovery (`--skip-oidc-discovery=false`).

## Automatic bootstrap (recommended)

`make e2e-test-xks` calls `EnsureGatewayConfigForXKS`, which:

1. Deploys Dex in `dex-system` (issuer: `https://dex.dex-system.svc.cluster.local:5556/dex`)
2. Creates `GatewayConfig` with matching OIDC client (`odh-gateway`) and `verifyProviderCertificate: false`

## Manual setup

If you run gateway e2e outside the test bootstrap, ensure Dex is up first:

```bash
# Dex is created automatically when tests start; to pre-provision manually, run e2e once or
# delete/recreate GatewayConfig after wiping the cluster:
kubectl delete gatewayconfig default-gateway --ignore-not-found
kubectl delete ns dex-system --ignore-not-found
make e2e-test-xks E2E_TEST_SERVICE=gateway
```

## Notes

- **Issuer URL** uses in-cluster DNS so `kube-auth-proxy` pods can reach Dex without CoreDNS hacks.
- **Health checks** use Dex telemetry HTTP (`:5558/healthz/ready`) so readiness probes avoid self-signed TLS verification.
- **Dex config** enables the built-in password DB connector (`enablePasswordDB: true`) because Dex v2.41+ requires at least one connector at startup.
- **Redirect URI** is `https://rh-ai.kind.local/oauth2/callback` (gateway hostname + OAuth callback path).
- **arm64 Mac**: Dex is multi-arch, but `odh-kube-auth-proxy` is still amd64-only; deployment readiness may fail locally even with Dex running.

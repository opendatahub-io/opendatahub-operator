# Gateway Service Resources

This directory contains resource templates for the OpenDataHub Gateway service, which creates Gateway API resources when the networking mode is set to "gateway-api".

## Templates

### cluster-issuer.tmpl.yaml

Creates a ClusterIssuer resource for certificate management:

- **Self-Signed Certificates**: Uses cert-manager to generate self-signed certificates
- **Default Issuer**: Named `odh-gateway-issuer` (matches controller defaults)
- **Development/Testing**: Suitable for non-production environments
- **Automatic Management**: Lifecycle managed by the gateway controller

### gateway.tmpl.yaml

Creates a Gateway API Gateway resource with the following features:

- **HTTP Listener**: Port 80 for HTTP traffic
- **HTTPS Listener**: Port 443 for HTTPS traffic with TLS termination
- **Namespace Selection**: Only allows routes from namespaces with `opendatahub.io/gateway-enabled: "true"` label
- **TLS Certificate**: Includes a placeholder TLS certificate secret

## Template Variables

The templates use the following variables:

- `Name`: The name of the gateway (e.g., "odh-gateway-opendatahub")
- `Namespace`: The namespace where the gateway will be deployed
- `Domain`: The domain for the gateway (e.g., "apps.cluster.local")
- `GatewayClassName`: The gateway class to use (e.g., "openshift-default")
- `ApplicationsNamespace`: The namespace for OpenDataHub applications

## Usage

The gateway controller automatically creates these resources when:

1. DSCI has `spec.networking.mode: "gateway-api"`
2. The gateway service is enabled (management state is "Managed")

## TLS Certificate

The Gateway controller supports multiple certificate management options:

### cert-manager Integration (Default)
By default, the Gateway controller uses cert-manager to automatically generate and manage TLS certificates:

- **Automatic Certificate Generation**: cert-manager creates and renews certificates automatically
- **Certificate Resource**: Creates a `Certificate` resource that references a `ClusterIssuer` or `Issuer`
- **Secret Generation**: cert-manager creates the TLS secret referenced by the Gateway
- **Auto-Renewal**: Certificates are automatically renewed before expiration

### Alternative Certificate Options
You can configure different certificate types in the Gateway spec:

- **CertManager** (default): Uses cert-manager for automatic certificate management
- **Provided**: References a pre-existing TLS secret
- **SelfSigned**: Generates a self-signed certificate (for development only)

### Configuration
Configure certificate management in the Gateway resource:

```yaml
apiVersion: services.opendatahub.io/v1alpha1
kind: Gateway
metadata:
  name: gateway
spec:
  domain: "apps.cluster.local"
  certificate:
    type: CertManager
    secretName: odh-gateway-tls
    issuerRef:
      name: letsencrypt-prod
      kind: ClusterIssuer
      group: cert-manager.io
```

## Namespace Labeling

For namespaces to be able to create HTTPRoutes that reference this gateway, they must have the label:

```yaml
labels:
  opendatahub.io/gateway-enabled: "true"
```

This provides security by ensuring only authorized namespaces can create routes through the gateway. 
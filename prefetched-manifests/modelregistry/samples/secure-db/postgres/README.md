# Secure PostgreSQL Sample

This sample demonstrates how to deploy a Model Registry instance with a PostgreSQL database that has TLS enabled.

## Prerequisites

### 1. Identify the Operator's Watched Namespace

The Model Registry operator is configured to watch a specific namespace. Before deploying, you must identify and use the correct namespace.

**Check the operator configuration:**
```bash
kubectl get deployment -n model-registry-operator-system model-registry-operator-controller-manager -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="REGISTRIES_NAMESPACE")].value}'
```

**This will output the namespace, for example:**
```text
model-registry-test
```

**Set your context to this namespace:**
```bash
kubectl config set-context --current --namespace=model-registry-test
```

> **Note:** For OpenShift AI or Open Data Hub deployments, the typical namespace is `rhoai-model-registries` or `odh-model-registries`. The operator's admission webhook will reject ModelRegistry CRs created in namespaces other than the configured namespace.

### 2. Generate TLS Certificates

You must generate TLS certificates for the PostgreSQL server. The operator provides a convenient `make` target to generate test certificates.

**From the root of the repository, run:**

```shell
make certificates
```

This will:
- Create test certificates in the `certs/` directory
- Generate a Kubernetes secret named `model-registry-db-credential` containing:
  - `ca.crt` - Certificate Authority certificate
  - `tls.crt` - Server certificate
  - `tls.key` - Server private key

The test certificates use a self-signed CA and are suitable for development and testing only.

> **NOTE:** For production environments, use a proper certificate manager (e.g., [cert-manager](https://cert-manager.io/)) to manage certificates and create Kubernetes secrets with the keys `tls.key`, `tls.crt`, and `ca.crt`.

## Deployment

After generating the certificates, deploy the secure PostgreSQL sample:

```shell
kubectl apply -k config/samples/secure-db/postgres
```

This will create:
- A PostgreSQL database deployment with TLS enabled
- A Model Registry instance configured to connect using `sslmode=verify-full`
- Required secrets and services

## Configuration Details

### PostgreSQL TLS Configuration

The PostgreSQL container is configured with the following TLS settings:
- `ssl=on` - Enables TLS connections
- `ssl_cert_file=/etc/server-cert/tls.crt` - Server certificate
- `ssl_key_file=/etc/server-cert/tls.key` - Server private key
- `ssl_ca_file=/etc/server-cert/ca.crt` - Certificate Authority certificate

### Model Registry TLS Configuration

The Model Registry is configured to connect to PostgreSQL with:
- `sslMode: verify-full` - Requires TLS and verifies the server certificate
- `sslRootCertificateConfigMap` - References the CA certificate for server verification

## Verification

Check that the Model Registry was created successfully:

```shell
kubectl describe mr modelregistry-sample
```

Check the `Status` field for any failed conditions.

## Cleanup

To remove the secure PostgreSQL sample:

```shell
kubectl delete -k config/samples/secure-db/postgres
```

To clean up the test certificates:

```shell
make certificates/clean
```

## Security Notes

- The sample database secret `model-registry-db-credential` contains the CA cert, server key, and server cert for demonstration purposes.
- In production, the Model Registry only needs access to the CA certificate(s).
- The database server should have its own secret containing the private key and server certificate.
- Use a proper certificate management solution for certificate rotation and lifecycle management.

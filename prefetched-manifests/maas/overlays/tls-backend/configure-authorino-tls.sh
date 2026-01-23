#!/bin/bash
# Configure Authorino for TLS communication with maas-api
# This script patches operator-managed Authorino resources that can't be modified via Kustomize
#
# Prerequisites:
# - Authorino operator installed in kuadrant-system namespace
# - service-ca-bundle ConfigMap exists (created by service-ca-bundle.yaml)

set -euo pipefail

NAMESPACE="${AUTHORINO_NAMESPACE:-kuadrant-system}"

echo "🔐 Configuring Authorino TLS in namespace: $NAMESPACE"

echo "📝 Adding serving-cert annotation to Authorino service..."
kubectl annotate service authorino-authorino-authorization \
  -n "$NAMESPACE" \
  service.beta.openshift.io/serving-cert-secret-name=authorino-server-cert \
  --overwrite

echo "🔧 Patching Authorino CR for TLS listener and CA bundle volume..."
kubectl patch authorino authorino -n "$NAMESPACE" --type=merge --patch '
{
  "spec": {
    "listener": {
      "tls": {
        "enabled": true,
        "certSecretRef": {
          "name": "authorino-server-cert"
        }
      }
    }
  }
}'


# Note: The Authorino CR doesn't support envVars, so we patch the deployment directly
echo "🌍 Adding environment variables to Authorino deployment..."
kubectl -n "$NAMESPACE" set env deployment/authorino \
  SSL_CERT_FILE=/etc/ssl/certs/openshift-service-ca/service-ca-bundle.crt \
  REQUESTS_CA_BUNDLE=/etc/ssl/certs/openshift-service-ca/service-ca-bundle.crt

echo "✅ Authorino TLS configuration complete"

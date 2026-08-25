#!/usr/bin/env bash
# Generate an XKS-tailored GatewayConfig CRD from the kubebuilder base CRD.
#
# The shared API (api/services/v1alpha1, api/infrastructure/v1) stays OpenShift-centric.
# This script patches a copy for vanilla Kubernetes (XKS):
#   - certificate.type enum: SelfSigned, Provided (no OpenshiftDefaultIngress)
#   - certificate.type default: SelfSigned
#   - certificate object default: {} so the nested type default applies when omitted
#   - ingressMode enum: LoadBalancer (no OcpRoute)
#   - ingressMode default: LoadBalancer
#   - docs for providerCASecretName / oidc.secretNamespace / verifyProviderCertificate
#
# odh-gitops copies the output into charts/dependencies/xks-gateway:
#   config/crd/xks/services.platform.opendatahub.io_gatewayconfigs.yaml
#     -> charts/dependencies/xks-gateway/templates/crds/customresourcedefinition-gatewayconfigs.services.platform.opendatahub.io.yaml
# Use: make generate-xks-gateway-crd
#      charts/dependencies/xks-gateway/scripts/sync-gatewayconfig-crd.sh <operator-repo>
#
# Usage: ./hack/generate-xks-gateway-crd.sh <yq> <src-crd> <dst-crd>

set -euo pipefail

YQ="${1:?yq binary required}"
SRC="${2:?source GatewayConfig CRD path required}"
DST="${3:?destination XKS CRD path required}"

if [[ ! -f "${SRC}" ]]; then
	echo "ERROR: base GatewayConfig CRD not found at ${SRC} (run 'make manifests' first)" >&2
	exit 1
fi

mkdir -p "$(dirname "${DST}")"
cp "${SRC}" "${DST}"

SPEC='.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties'

export XKS_CERT_TYPE_DESC='Type specifies if the TLS certificate should be generated automatically, or if the certificate
is provided by the user. Allowed values are:
* SelfSigned: A certificate is going to be generated using an own private key. This is the default on Kubernetes (XKS).
* Provided: Pre-existence of the TLS Secret (see SecretName) with a valid certificate is assumed.
OpenshiftDefaultIngress is not supported on Kubernetes clusters.'

export XKS_INGRESS_MODE_DESC='IngressMode specifies how the Gateway is exposed externally.
On Kubernetes (XKS) only "LoadBalancer" is supported (requires cloud or MetalLB).
OcpRoute is OpenShift-only and is not a valid value on XKS.'

export XKS_PROVIDER_CA_DESC='ProviderCASecretName is the name of the secret containing the CA certificate for the authentication provider
Used when the OAuth/OIDC provider uses a self-signed or custom CA certificate.
Secret must exist in the gateway namespace (rh-ai-gateway) and contain a ca.crt key with the PEM-encoded CA certificate.'

export XKS_SECRET_NS_DESC='Namespace where the client secret is located.
If not specified, defaults to the gateway namespace (rh-ai-gateway).'

export XKS_VERIFY_CERT_DESC='VerifyProviderCertificate controls TLS certificate verification for the authentication provider.
When true (default), certificates are verified against the system trust store and providerCASecretName.
When false, certificate verification is disabled (development/testing only).
WARNING: Setting this to false disables security and should only be used in non-production environments.
For production use with self-signed certificates, use ProviderCASecretName (secret in rh-ai-gateway, key ca.crt) instead.'

"${YQ}" -i "
	.metadata.annotations.\"helm.sh/resource-policy\" = \"keep\" |
	${SPEC}.certificate.default = {} |
	${SPEC}.certificate.properties.type.enum = [\"SelfSigned\", \"Provided\"] |
	${SPEC}.certificate.properties.type.default = \"SelfSigned\" |
	${SPEC}.certificate.properties.type.description = strenv(XKS_CERT_TYPE_DESC) |
	${SPEC}.ingressMode.enum = [\"LoadBalancer\"] |
	${SPEC}.ingressMode.default = \"LoadBalancer\" |
	${SPEC}.ingressMode.description = strenv(XKS_INGRESS_MODE_DESC) |
	${SPEC}.providerCASecretName.description = strenv(XKS_PROVIDER_CA_DESC) |
	${SPEC}.oidc.properties.secretNamespace.description = strenv(XKS_SECRET_NS_DESC) |
	${SPEC}.verifyProviderCertificate.description = strenv(XKS_VERIFY_CERT_DESC)
" "${DST}"

echo "Wrote XKS GatewayConfig CRD to ${DST}"

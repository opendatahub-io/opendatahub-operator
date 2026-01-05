#!/bin/bash

# --- Configuration ---

# CA Details
CA_KEY="ca.key"
CA_CERT="ca.crt"
CA_SUBJECT="/C=US/ST=California/L=Los Angeles/O=Demo Corp/OU=OpenShift CA/CN=example-ca"

# Security Configuration
KEY_SIZE=4096
CA_VALIDITY_DAYS=365

echo "================================================"
echo "WARNING: This script is for TESTING PURPOSES ONLY"
echo "Do NOT use these certificates in production!"
echo "================================================"
echo

# --- 1. Create the Certificate Authority (CA) ---

echo "Generating CA private key and self-signed certificate..."

# Generate the CA's private key with stronger key size
openssl genrsa -out "${CA_KEY}" ${KEY_SIZE}

# Generate the self-signed CA certificate
openssl req -x509 -new -nodes -key "${CA_KEY}" \
  -sha256 -days ${CA_VALIDITY_DAYS} \
  -subj "${CA_SUBJECT}" \
  -out "${CA_CERT}"

echo "CA created successfully: ${CA_KEY}, ${CA_CERT}"


echo "   All files created successfully!"
echo "------------------------------------"
echo "  - CA Private Key:           ${CA_KEY} (${KEY_SIZE}-bit)"
echo "  - CA Certificate:           ${CA_CERT} (valid for ${CA_VALIDITY_DAYS} days)"
echo "------------------------------------"
echo
echo "Security Reminders:"
echo "- Keep private keys secure and never commit them to version control"
echo "- Consider using cert-manager for production certificate management"
echo "- Rotate certificates regularly in production environments"


# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

mkdir -p "${SCRIPT_DIR}/vllm-ca-certs"

# Move to a mock CA bundle file
cp "${CA_CERT}" "${SCRIPT_DIR}/vllm-ca-certs/ca-bundle.crt"

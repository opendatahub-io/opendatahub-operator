#!/bin/bash

set -euo pipefail

# Script to validate the bundle ClusterServiceVersion (CSV) file
# This script validates the following critical fields:
# 1. containerImage: quay.io/opendatahub/opendatahub-operator:v{VERSION}
# 2. name: opendatahub-operator.v{VERSION}
# 3. image (in deployment): quay.io/opendatahub/opendatahub-operator:v{VERSION}
# 4. version: {VERSION} (without 'v' prefix)

VERSION=$1

if [ -z "$VERSION" ]; then
  echo "Usage: $0 <version>"
  echo "Example: $0 3.1.0"
  exit 1
fi

CSV_FILE="odh-bundle/manifests/opendatahub-operator.clusterserviceversion.yaml"

if [ ! -f "$CSV_FILE" ]; then
  echo "Error: CSV file not found at $CSV_FILE"
  exit 1
fi

if ! command -v yq &> /dev/null; then
  echo "Error: yq is not installed. Please install yq (https://github.com/mikefarah/yq)"
  exit 1
fi

echo "Validating bundle CSV for version $VERSION..."
echo "CSV file: $CSV_FILE"
echo ""

VALIDATION_FAILED=0

EXPECTED_IMAGE="quay.io/opendatahub/opendatahub-operator:v${VERSION}"
EXPECTED_NAME="opendatahub-operator.v${VERSION}"
EXPECTED_VERSION="$VERSION"

validate_field() {
  local field_name=$1
  local yq_path=$2
  local expected_value=$3

  echo "Checking ${field_name}..."
  local actual_value
  actual_value=$(yq eval "${yq_path}" "$CSV_FILE")

  if [ "$actual_value" = "$expected_value" ]; then
    echo "${field_name} is correct: ${expected_value}"
  else
    echo "ERROR: ${field_name} is not set to ${expected_value}"
    echo "  Actual: $actual_value"
    VALIDATION_FAILED=1
  fi
}

validate_field "containerImage" ".metadata.annotations.containerImage" "$EXPECTED_IMAGE"
validate_field "name" ".metadata.name" "$EXPECTED_NAME"
validate_field "deployment image" ".spec.install.spec.deployments[0].spec.template.spec.containers[0].image" "$EXPECTED_IMAGE"
validate_field "version" ".spec.version" "$EXPECTED_VERSION"

echo ""

if [ $VALIDATION_FAILED -eq 0 ]; then
  echo "All CSV validations passed!"
  echo ""
  echo "Summary:"
  echo "  - containerImage: ${EXPECTED_IMAGE}"
  echo "  - name: ${EXPECTED_NAME}"
  echo "  - deployment image: ${EXPECTED_IMAGE}"
  echo "  - version: ${EXPECTED_VERSION}"
  exit 0
else
  echo "CSV validation failed!"
  echo ""
  echo "Please ensure the following fields are correct in the CSV:"
  echo "  1. containerImage: ${EXPECTED_IMAGE}"
  echo "  2. name: ${EXPECTED_NAME}"
  echo "  3. deployment image: ${EXPECTED_IMAGE}"
  echo "  4. version: ${EXPECTED_VERSION}"
  exit 1
fi

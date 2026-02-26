#!/bin/bash

set -euo pipefail

# Script to validate the community operator bundle
# This script validates:
# 1. annotations.yaml has fast-3 channel
# 2. annotations.yaml has OpenShift versions annotation
# 3. release-config.yaml exists
# 4. release-config.yaml has correct replaces field

BUNDLE_DIR=$1
PREVIOUS_VERSION=$2
OPENSHIFT_VERSIONS=$3

if [ -z "$BUNDLE_DIR" ] || [ -z "$PREVIOUS_VERSION" ] || [ -z "$OPENSHIFT_VERSIONS" ]; then
  echo "Usage: $0 <bundle-dir> <previous-version> <openshift-versions>"
  echo "Example: $0 /path/to/bundle 3.0.0 'v4.19-v4.20'"
  exit 1
fi

if [ ! -d "$BUNDLE_DIR" ]; then
  echo "Error: Bundle directory does not exist: $BUNDLE_DIR"
  exit 1
fi

if ! command -v yq &> /dev/null; then
  echo "Error: yq is not installed. Please install yq (https://github.com/mikefarah/yq)"
  exit 1
fi

ANNOTATIONS_FILE="$BUNDLE_DIR/metadata/annotations.yaml"
RELEASE_CONFIG_FILE="$BUNDLE_DIR/release-config.yaml"

echo "Validating community bundle..."
echo "Bundle directory: $BUNDLE_DIR"
echo ""

VALIDATION_FAILED=0

validate_yaml_field() {
  local file_path=$1
  local field_name=$2
  local yq_path=$3
  local expected_value=$4
  local is_warning=${5:-false}  # Optional: set to true for warnings instead of errors

  echo "Checking ${field_name}..."
  local actual_value
  actual_value=$(yq eval "${yq_path}" "$file_path")

  if [ "$actual_value" = "$expected_value" ]; then
    echo "${field_name} is correct: ${expected_value}"
  else
    if [ "$is_warning" = "true" ]; then
      echo "WARNING: ${field_name} mismatch"
      echo "  Expected: $expected_value"
      echo "  Actual: $actual_value"
    else
      echo "ERROR: ${field_name} is not set to ${expected_value}"
      echo "  Actual: $actual_value"
      VALIDATION_FAILED=1
    fi
  fi
}

# 1. Validate annotations.yaml exists
if [ ! -f "$ANNOTATIONS_FILE" ]; then
  echo "ERROR: annotations.yaml not found at $ANNOTATIONS_FILE"
  exit 1
fi

# 2. Validate channel is fast-3
validate_yaml_field "$ANNOTATIONS_FILE" "channel" '.annotations."operators.operatorframework.io.bundle.channels.v1"' "fast-3"

# 3. Validate OpenShift versions annotation exists
echo "Checking OpenShift versions annotation..."
ACTUAL_OPENSHIFT_VERSIONS=$(yq eval '.annotations."com.redhat.openshift.versions"' "$ANNOTATIONS_FILE")
if [ "$ACTUAL_OPENSHIFT_VERSIONS" = "null" ]; then
  echo "ERROR: OpenShift versions annotation not found"
  VALIDATION_FAILED=1
else
  validate_yaml_field "$ANNOTATIONS_FILE" "OpenShift versions annotation" '.annotations."com.redhat.openshift.versions"' "$OPENSHIFT_VERSIONS" "true"
fi

# 4. Validate release-config.yaml exists
echo "Checking release-config.yaml..."
if [ ! -f "$RELEASE_CONFIG_FILE" ]; then
  echo "ERROR: release-config.yaml not found at $RELEASE_CONFIG_FILE"
  VALIDATION_FAILED=1
else
  echo "release-config.yaml exists"

  # 5. Validate replaces field
  EXPECTED_REPLACES="opendatahub-operator.v${PREVIOUS_VERSION}"
  validate_yaml_field "$RELEASE_CONFIG_FILE" "replaces field" '.catalog_templates[0].replaces' "$EXPECTED_REPLACES"

  # 6. Validate channels in release-config.yaml
  echo "Checking release-config channels..."
  ACTUAL_CHANNELS=$(yq eval '.catalog_templates[0].channels[]' "$RELEASE_CONFIG_FILE")
  if echo "$ACTUAL_CHANNELS" | grep -q "fast-3"; then
    echo "Release config has fast-3 channel"
  else
    echo "ERROR: Release config does not have fast-3 channel"
    echo "  Actual channels: $ACTUAL_CHANNELS"
    VALIDATION_FAILED=1
  fi
fi

echo ""

if [ $VALIDATION_FAILED -eq 0 ]; then
  echo "All community bundle validations passed!"
  echo ""
  echo "Summary:"
  echo "  - Channel: fast-3"
  echo "  - OpenShift versions: $ACTUAL_OPENSHIFT_VERSIONS"
  echo "  - Replaces: opendatahub-operator.v${PREVIOUS_VERSION}"
  exit 0
else
  echo "Community bundle validation failed!"
  echo ""
  echo "Please check the errors above and fix the bundle."
  exit 1
fi

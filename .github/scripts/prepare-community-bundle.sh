#!/bin/bash

set -euo pipefail

# Script to prepare the community operator bundle
# This script:
# 1. Updates annotations.yaml to change channel from 'fast' to 'fast-3'
# 2. Adds OpenShift version compatibility annotation
# 3. Creates release-config.yaml with the replaces field

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

ANNOTATIONS_FILE="$BUNDLE_DIR/metadata/annotations.yaml"
RELEASE_CONFIG_FILE="$BUNDLE_DIR/release-config.yaml"

echo "Preparing community bundle in: $BUNDLE_DIR"

# 1. Update annotations.yaml
if [ ! -f "$ANNOTATIONS_FILE" ]; then
  echo "Error: annotations.yaml not found at $ANNOTATIONS_FILE"
  exit 1
fi

echo "Updating annotations.yaml..."

# Change channel from 'fast' to 'fast-3'
sed -i -e 's|operators.operatorframework.io.bundle.channels.v1: fast$|operators.operatorframework.io.bundle.channels.v1: fast-3|g' "$ANNOTATIONS_FILE"

# Add OpenShift version compatibility at the end of the file
# First, check if it already exists (idempotency)
if ! grep -q "com.redhat.openshift.versions:" "$ANNOTATIONS_FILE"; then
  # Add a blank line if the file doesn't end with one
  if [ -n "$(tail -c 1 "$ANNOTATIONS_FILE")" ]; then
    echo "" >> "$ANNOTATIONS_FILE"
  fi

  # Add the OpenShift versions annotation
  cat >> "$ANNOTATIONS_FILE" <<EOF

  # OpenShift specific version
  com.redhat.openshift.versions: "$OPENSHIFT_VERSIONS"
EOF
  echo "Added OpenShift versions annotation: $OPENSHIFT_VERSIONS"
else
  echo "OpenShift versions annotation already exists"
fi

# Verify the channel was updated
if grep -q "operators.operatorframework.io.bundle.channels.v1: fast-3" "$ANNOTATIONS_FILE"; then
  echo "Channel updated to fast-3"
else
  echo "Error: Failed to update channel to fast-3"
  exit 1
fi

# 2. Create release-config.yaml
echo "Creating release-config.yaml..."

cat > "$RELEASE_CONFIG_FILE" <<EOF
---
catalog_templates:
  - template_name: basic-template.yaml
    channels: [fast-3]
    replaces: opendatahub-operator.v${PREVIOUS_VERSION}
EOF

if [ -f "$RELEASE_CONFIG_FILE" ]; then
  echo "Created release-config.yaml"
  echo "  - Replaces: opendatahub-operator.v${PREVIOUS_VERSION}"
  echo "  - Channel: fast-3"
else
  echo "Error: Failed to create release-config.yaml"
  exit 1
fi

echo ""
echo "Community bundle preparation completed successfully!"
echo ""
echo "Summary:"
echo "  - Bundle directory: $BUNDLE_DIR"
echo "  - Channel: fast-3"
echo "  - Replaces: opendatahub-operator.v${PREVIOUS_VERSION}"
echo "  - OpenShift versions: $OPENSHIFT_VERSIONS"

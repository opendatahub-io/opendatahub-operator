#!/bin/bash
# Validates that downloaded manifests don't contain floating image tags
# This prevents image drift when manifests are SHA-pinned but contain :latest tags
#
# Set STRICT_IMAGE_VALIDATION=true to make violations blocking (fails builds)
# Default: non-strict mode (warnings only, does not fail builds)

set -e

STRICT_MODE="${STRICT_IMAGE_VALIDATION:-false}"
MANIFESTS_DIR="opt/manifests"
FAILED=0
VIOLATIONS=()

echo "Scanning manifests for floating image tags..."

# Check if manifests directory exists
if [ ! -d "$MANIFESTS_DIR" ]; then
    echo "Warning: Manifests directory not found: $MANIFESTS_DIR"
    echo "Skipping validation (manifests may not be downloaded yet)"
    exit 0
fi

# Find all params.env and kustomization.yaml files
while IFS= read -r file; do
    # Check for :latest tags
    if grep -q ':latest' "$file" 2>/dev/null; then
        violations=$(grep -n ':latest' "$file" | head -5)
        VIOLATIONS+=("ERROR: $file contains :latest tag(s):")
        while IFS= read -r line; do
            VIOLATIONS+=("   $line")
        done <<< "$violations"
        FAILED=1
    fi

    # Check for :stable tags
    if grep -q ':stable' "$file" 2>/dev/null; then
        violations=$(grep -n ':stable' "$file" | head -5)
        VIOLATIONS+=("ERROR: $file contains :stable tag(s):")
        while IFS= read -r line; do
            VIOLATIONS+=("   $line")
        done <<< "$violations"
        FAILED=1
    fi

    # Check for semantic version tags without @sha256 (potential floating tags)
    # Pattern: image=something:v1.2.3 (without @sha256)
    if grep -E 'image.*:[v]?[0-9]+\.[0-9]+(\.[0-9]+)?' "$file" 2>/dev/null | grep -v '@sha256:' | grep -q .; then
        violations=$(grep -E 'image.*:[v]?[0-9]+\.[0-9]+(\.[0-9]+)?' "$file" | grep -v '@sha256:' | grep -n . | head -5)
        VIOLATIONS+=("Warning: $file may contain floating version tag(s):")
        while IFS= read -r line; do
            VIOLATIONS+=("   $line")
        done <<< "$violations"
        # Don't fail on semantic versions, just warn
        # FAILED=1
    fi
done < <(find "$MANIFESTS_DIR" -type f \( -name "params.env" -o -name "kustomization.yaml" -o -name "*.yaml" \) 2>/dev/null)

if [ $FAILED -eq 1 ]; then
    echo ""
    echo "Floating image tags detected in component manifests:"
    echo ""
    for violation in "${VIOLATIONS[@]}"; do
        echo "$violation"
    done
    echo ""
    echo "Please use SHA-pinned images in component params.env files:"
    echo "  Good: quay.io/opendatahub/image@sha256:abc123..."
    echo "  Bad:  quay.io/opendatahub/image:latest"
    echo "  Bad:  quay.io/opendatahub/image:stable"
    echo ""
    if [ "$STRICT_MODE" = "true" ]; then
        echo "ERROR: Build failed due to floating image tags."
        echo "Set STRICT_IMAGE_VALIDATION=false to make this non-blocking."
        exit 1
    else
        echo "Warning: Continuing build (violations above are warnings only)."
        echo "Set STRICT_IMAGE_VALIDATION=true to fail on image tag violations."
        exit 0
    fi
fi

echo "All manifests use pinned image tags (or no images found)"
exit 0

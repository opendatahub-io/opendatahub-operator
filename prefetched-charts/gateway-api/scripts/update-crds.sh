#!/bin/bash
# Update Gateway API CRDs
# Usage: ./update-crds.sh [version] [channel]
# Examples:
#   ./update-crds.sh v1.4.0
#   ./update-crds.sh v1.4.0 experimental

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHART_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

VERSION="${1:-$(awk '/^gatewayAPI:/{found=1; next} found && /^[^ ]/{exit} found && /^  version:/{gsub(/"/,"",$2); print $2; exit}' "$CHART_DIR/values.yaml")}"
CHANNEL="${2:-$(awk '/^gatewayAPI:/{found=1; next} found && /^[^ ]/{exit} found && /^  channel:/{gsub(/"/,"",$2); print $2; exit}' "$CHART_DIR/values.yaml")}"
CHANNEL="${CHANNEL:-standard}"

# Ensure version has 'v' prefix (GitHub tags require it)
if [[ "$VERSION" =~ ^[0-9] ]]; then
  VERSION="v${VERSION}"
fi

echo "============================================"
echo "  Updating Gateway API CRDs"
echo "============================================"
echo "Version: $VERSION"
echo "Channel: $CHANNEL"
echo "Source:  https://github.com/kubernetes-sigs/gateway-api/config/crd/${CHANNEL}?ref=${VERSION}"
echo ""

CRD_DIR="$CHART_DIR/templates"
BASE_URL="https://raw.githubusercontent.com/kubernetes-sigs/gateway-api/${VERSION}/config/crd/${CHANNEL}"

# Download into a staging directory so we don't clobber existing CRDs on failure
STAGING_DIR=$(mktemp -d)
trap 'rm -rf "$STAGING_DIR"' EXIT

echo "Downloading CRDs..."
for crd_file in $(curl -fsSL "https://api.github.com/repos/kubernetes-sigs/gateway-api/contents/config/crd/${CHANNEL}?ref=${VERSION}" | \
  python3 -c "import sys,json; [print(f['name']) for f in json.load(sys.stdin) if f['name'].endswith('.yaml') and f['name'] != 'kustomization.yaml']"); do
  echo "  ${crd_file}"
  curl -fsSL "${BASE_URL}/${crd_file}" -o "$STAGING_DIR/${crd_file}"
done

CRD_COUNT=$(find "$STAGING_DIR" -name "*.yaml" | wc -l)
if [ "$CRD_COUNT" -eq 0 ]; then
  echo "ERROR: No CRDs were downloaded. Aborting."
  exit 1
fi
echo ""
echo "Downloaded ${CRD_COUNT} CRDs"

# Replace existing CRDs with staged downloads
rm -rf "$CRD_DIR"
mkdir -p "$CRD_DIR"
mv "$STAGING_DIR"/*.yaml "$CRD_DIR/"

# Update values.yaml
sed -i "s/^  version: \"v[^\"]*\"/  version: \"${VERSION}\"/" "$CHART_DIR/values.yaml"
sed -i "s/^  channel: \"[^\"]*\"/  channel: \"${CHANNEL}\"/" "$CHART_DIR/values.yaml"

# Update Chart.yaml
sed -i "s/^version: .*/version: ${VERSION#v}/" "$CHART_DIR/Chart.yaml"
sed -i "s/^appVersion: .*/appVersion: \"${VERSION}\"/" "$CHART_DIR/Chart.yaml"

echo ""
echo "============================================"
echo "  Update Complete!"
echo "============================================"
echo ""
echo "Next steps:"
echo "  1. Review CRDs in $CRD_DIR"
echo "  2. Test with: helm template gateway-api $CHART_DIR"

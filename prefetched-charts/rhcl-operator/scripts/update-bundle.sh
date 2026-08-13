#!/bin/bash
# Update Helm chart with new RHCL bundle version
# Extracts manifests from both the Kuadrant and Authorino operator bundles
# since the Kuadrant bundle depends on Authorino/Limitador operators that
# are distributed as separate OLM subscriptions.
#
# Usage: ./update-bundle.sh [version]
# Examples:
#   ./update-bundle.sh 1.3.0

set -e

VERSION="${1:-1.3.0}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHART_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Bundle images from registry.redhat.io
KUADRANT_BUNDLE="registry.redhat.io/rhcl-1/rhcl-operator-bundle:${VERSION}"
AUTHORINO_BUNDLE="registry.redhat.io/rhcl-1/authorino-operator-bundle:${VERSION}"
LIMITADOR_BUNDLE="registry.redhat.io/rhcl-1/limitador-operator-bundle:${VERSION}"
DNS_BUNDLE="registry.redhat.io/rhcl-1/dns-operator-bundle:${VERSION}"

echo "============================================"
echo "  Updating RHCL Operator Helm Chart"
echo "============================================"
echo "Version: $VERSION"
echo "Bundles:"
echo "  - $KUADRANT_BUNDLE"
echo "  - $AUTHORINO_BUNDLE"
echo "  - $LIMITADOR_BUNDLE"
echo "  - $DNS_BUNDLE"
echo ""

# Check for auth (persistent location first, then session)
if [ -f ~/.config/containers/auth.json ]; then
  AUTH_FILE=~/.config/containers/auth.json
elif [ -f "${XDG_RUNTIME_DIR}/containers/auth.json" ]; then
  AUTH_FILE="${XDG_RUNTIME_DIR}/containers/auth.json"
else
  echo "ERROR: Not logged in to registry.redhat.io"
  echo "Run: podman login registry.redhat.io"
  echo "Then: cp ~/pull-secret.txt ~/.config/containers/auth.json"
  exit 1
fi

AUTH_ARG="-v ${AUTH_FILE}:/root/.docker/config.json:z"

# Create temp directory
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

EXCLUDE_ARGS='--exclude .kind == "ConsoleCLIDownload" --exclude .kind == "ConsolePlugin" --exclude .kind == "Route" --exclude .kind == "SecurityContextConstraints" --exclude .kind == "ConsoleYAMLSample"'

# Extract manifests from Kuadrant bundle
echo "[1/4] Extracting Kuadrant operator manifests..."
podman run --rm --pull=always $AUTH_ARG \
  quay.io/lburgazzoli/olm-extractor:main \
  run "$KUADRANT_BUNDLE" \
  -n kuadrant-operators \
  --watch-namespace="" \
  --exclude '.kind == "ConsoleCLIDownload"' \
  --exclude '.kind == "ConsolePlugin"' \
  --exclude '.kind == "Route"' \
  --exclude '.kind == "SecurityContextConstraints"' \
  --exclude '.kind == "ConsoleYAMLSample"' \
  2>/dev/null | grep -v "^time=" > "$TMP_DIR/kuadrant-manifests.yaml"

echo "  Kuadrant: $(wc -l < "$TMP_DIR/kuadrant-manifests.yaml") lines"

# Extract manifests from Authorino bundle
echo "[2/4] Extracting Authorino operator manifests..."
podman run --rm --pull=always $AUTH_ARG \
  quay.io/lburgazzoli/olm-extractor:main \
  run "$AUTHORINO_BUNDLE" \
  -n kuadrant-operators \
  --watch-namespace="" \
  --exclude '.kind == "ConsoleCLIDownload"' \
  --exclude '.kind == "ConsolePlugin"' \
  --exclude '.kind == "Route"' \
  --exclude '.kind == "SecurityContextConstraints"' \
  --exclude '.kind == "ConsoleYAMLSample"' \
  2>/dev/null | grep -v "^time=" > "$TMP_DIR/authorino-manifests.yaml"

echo "  Authorino: $(wc -l < "$TMP_DIR/authorino-manifests.yaml") lines"

# Extract manifests from Limitador bundle
echo "[2b/4] Extracting Limitador operator manifests..."
podman run --rm --pull=always $AUTH_ARG \
  quay.io/lburgazzoli/olm-extractor:main \
  run "$LIMITADOR_BUNDLE" \
  -n kuadrant-operators \
  --watch-namespace="" \
  --exclude '.kind == "ConsoleCLIDownload"' \
  --exclude '.kind == "ConsolePlugin"' \
  --exclude '.kind == "Route"' \
  --exclude '.kind == "SecurityContextConstraints"' \
  --exclude '.kind == "ConsoleYAMLSample"' \
  2>/dev/null | grep -v "^time=" > "$TMP_DIR/limitador-manifests.yaml"

echo "  Limitador: $(wc -l < "$TMP_DIR/limitador-manifests.yaml") lines"

# Extract manifests from DNS bundle
echo "[2c/4] Extracting DNS operator manifests..."
podman run --rm --pull=always $AUTH_ARG \
  quay.io/lburgazzoli/olm-extractor:main \
  run "$DNS_BUNDLE" \
  -n kuadrant-operators \
  --watch-namespace="" \
  --exclude '.kind == "ConsoleCLIDownload"' \
  --exclude '.kind == "ConsolePlugin"' \
  --exclude '.kind == "Route"' \
  --exclude '.kind == "SecurityContextConstraints"' \
  --exclude '.kind == "ConsoleYAMLSample"' \
  2>/dev/null | grep -v "^time=" > "$TMP_DIR/dns-manifests.yaml"

echo "  DNS: $(wc -l < "$TMP_DIR/dns-manifests.yaml") lines"

# Merge all manifests
cat "$TMP_DIR/kuadrant-manifests.yaml" "$TMP_DIR/authorino-manifests.yaml" \
    "$TMP_DIR/limitador-manifests.yaml" "$TMP_DIR/dns-manifests.yaml" > "$TMP_DIR/manifests.yaml"

# Validate extraction produced output
if [ ! -s "$TMP_DIR/manifests.yaml" ]; then
  echo "ERROR: Extraction produced empty manifests. Check bundle images and registry access."
  exit 1
fi

echo "  Combined: $(wc -l < "$TMP_DIR/manifests.yaml") lines"

# Clean: remove all CRDs and templates (only after successful extraction)
echo "[3/4] Cleaning old manifests..."
find "$CHART_DIR/crds" -name "*.yaml" -delete 2>/dev/null || true
find "$CHART_DIR/templates" -name "*.yaml" \
  ! -name "namespace.yaml" \
  ! -name "serviceaccount-components.yaml" \
  ! -name "kuadrant.yaml" \
  -delete 2>/dev/null || true

# Split manifests into CRDs and templates, templatize namespace references
echo "[4/4] Splitting into CRDs and templates..."

export TMP_DIR CHART_DIR
python3 << 'PYEOF'
import yaml
import os

tmp_dir = os.environ.get('TMP_DIR', '/tmp')
chart_dir = os.environ.get('CHART_DIR', '.')

input_file = f'{tmp_dir}/manifests.yaml'
crds_dir = f'{chart_dir}/crds'
templates_dir = f'{chart_dir}/templates'

os.makedirs(crds_dir, exist_ok=True)
os.makedirs(templates_dir, exist_ok=True)

with open(input_file, 'r') as f:
    content = f.read()

docs = content.split('\n---\n')
crd_count = 0
other_count = 0
skipped = []
seen = set()

# OpenShift-specific resources to skip
skip_kinds = [
    'ConsoleCLIDownload',
    'ConsolePlugin',
    'ConsoleYAMLSample',
    'Route',
    'SecurityContextConstraints',
    'ImageContentSourcePolicy',
]

for doc in docs:
    if not doc.strip():
        continue
    try:
        obj = yaml.safe_load(doc)
        if not obj:
            continue
        kind = obj.get('kind', 'unknown')
        name = obj.get('metadata', {}).get('name', 'unknown')

        # Skip OpenShift-specific resources
        if kind in skip_kinds:
            skipped.append(f"{kind}/{name}")
            continue

        # Deduplicate (same resource from multiple bundles)
        key = f"{kind}/{name}"
        if key in seen:
            continue
        seen.add(key)

        if kind == 'CustomResourceDefinition':
            # Convention: customresourcedefinition-<name-with-dots-as-dashes>.yaml
            filename = f"customresourcedefinition-{name.replace('.', '-')}.yaml"
            filepath = os.path.join(crds_dir, filename)
            crd_count += 1
            with open(filepath, 'w') as out:
                out.write(doc.strip() + '\n')
        elif kind == 'Namespace':
            # Skip — managed by templates/namespace.yaml
            continue
        else:
            # Convention: <kind-lowercase>-<name-truncated>.yaml
            filename = f"{kind.lower()}-{name[:60]}.yaml"
            filepath = os.path.join(templates_dir, filename)
            other_count += 1
            # Templatize namespace references
            content = doc.strip()
            content = content.replace('namespace: kuadrant-operators', 'namespace: {{ .Values.operatorNamespace }}')
            content = content.replace('namespace: kuadrant-system', 'namespace: {{ .Values.operandNamespace }}')
            # Add imagePullSecrets to ServiceAccounts
            if kind == 'ServiceAccount':
                content += '\n{{- with .Values.imagePullSecrets }}\nimagePullSecrets:\n  {{- toYaml . | nindent 2 }}\n{{- end }}'
            # Add imagePullSecrets to Deployments
            if kind == 'Deployment':
                content = content.replace(
                    '    spec:\n      containers:',
                    '    spec:\n      {{- with .Values.imagePullSecrets }}\n      imagePullSecrets:\n        {{- toYaml . | nindent 8 }}\n      {{- end }}\n      containers:'
                )
            with open(filepath, 'w') as out:
                out.write(content + '\n')

    except Exception as e:
        print(f"Error processing manifest: {e}", file=__import__('sys').stderr)
        __import__('sys').exit(1)

print(f"Created {crd_count} CRDs")
print(f"Created {other_count} templates")
if skipped:
    print(f"Skipped {len(skipped)} OpenShift-specific resources:")
    for s in skipped:
        print(f"  - {s}")
PYEOF

# Update bundle.version in values.yaml
sed -i '' '/^bundle:/,/^[a-z]/{s/  version: ".*"/  version: "'"$VERSION"'"/;}' "$CHART_DIR/values.yaml"

echo ""
echo "============================================"
echo "  Update Complete!"
echo "============================================"
echo ""
echo "Chart updated at: $CHART_DIR"
echo "New version: $VERSION"
echo ""
echo "Next steps:"
echo "  1. Review extracted manifests"
echo "  2. Update images.operator in values.yaml if the image SHA changed"
echo "  3. Regenerate snapshots: make chart-snapshots CHART_NAME=dependencies/rhcl-operator"
echo "  4. Test with: helm template rhcl-operator $CHART_DIR"

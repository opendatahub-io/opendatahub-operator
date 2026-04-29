#!/usr/bin/env bash
set -euo pipefail

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

cat <<'EOF' > "$TMP/test-csv.yaml"
spec:
  install:
    spec:
      deployments:
        - spec:
            template:
              spec:
                containers:
                  - env:
                      - name: RELATED_IMAGE_ODH_KSERVE_CONTROLLER_IMAGE
                        value: registry.redhat.io/rhoai/kserve@sha256:abc123
                      - name: RELATED_IMAGE_ODH_DASHBOARD_IMAGE
                        value: registry.redhat.io/rhoai/dashboard@sha256:def456
                      - name: NOT_A_RELATED_IMAGE
                        value: should-be-skipped
                      - name: RELATED_IMAGE_ODH_SPARK_OPERATOR_IMAGE
                        value: registry.redhat.io/rhoai/spark@sha256:ghi789
EOF

YQ="${YQ:-$(command -v yq || true)}"
[[ -n "$YQ" ]] || { echo "ERROR: yq not found"; exit 1; }

echo "=== Test: select(.name == \"RELATED_IMAGE_*\") ==="
"${YQ}" eval '
    .spec.install.spec.deployments[].spec.template.spec.containers[].env[]
    | select(.name == "RELATED_IMAGE_*")
    | .name + "=" + .value
' "$TMP/test-csv.yaml" | tr -d '"'

echo ""
echo "=== Expected: 3 RELATED_IMAGE entries, 0 non-RELATED_IMAGE ==="

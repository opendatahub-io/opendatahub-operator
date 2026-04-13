#!/usr/bin/env bash
set -euo pipefail

if [[ "${BASH_VERSINFO[0]}" -lt 4 ]]; then
  echo "ERROR: bash 4+ is required (found ${BASH_VERSION}). On macOS: brew install bash" >&2
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CHARTS_DIR="${REPO_ROOT}/opt/charts"
TARGET_FILE="${REPO_ROOT}/internal/controller/cloudmanager/common/kubebuilder_rbac.go"

YQ="${1:?yq binary path is required as first argument}"
HELM="${2:?helm binary path is required as second argument}"

BEGIN_MARKER='+auto:rbac:begin'
END_MARKER='+auto:rbac:end'

ROLE_VERBS="get;list;watch;patch;update;delete;bind;escalate"

# Validate tool binaries exist and are executable.
for tool in "$YQ" "$HELM"; do
  if [[ ! -x "$tool" ]]; then
    echo "ERROR: $tool is not found or not executable" >&2
    exit 1
  fi
done

# Validate charts directory exists.
if [[ ! -d "$CHARTS_DIR" ]]; then
  echo "ERROR: Charts directory '$CHARTS_DIR' does not exist. Run 'make get-manifests' first." >&2
  exit 1
fi

# Collect role and clusterrole names from rendered chart templates.
# Output format: <chart_name> <kind> <name>
collect_names() {
  for chart_dir in "${CHARTS_DIR}"/*/; do
    [ -d "$chart_dir" ] || continue
    chart_name=$(basename "$chart_dir")

    # Render chart with default values.
    local rendered helm_stderr
    helm_stderr=$(mktemp)
    rendered=$("$HELM" template "$chart_name" "$chart_dir" 2>"$helm_stderr") || {
      echo "ERROR: helm template failed for chart '$chart_name': $(cat "$helm_stderr")" >&2
      rm -f "$helm_stderr"
      return 1
    }
    rm -f "$helm_stderr"

    # Extract Role/ClusterRole names from the rendered multi-document YAML.
    echo "$rendered" | chart="$chart_name" "$YQ" eval '
      select(.kind == "Role" or .kind == "ClusterRole") |
      env(chart) + " " + .kind + " " + .metadata.name
    ' -
  done
}

# Sort semicolon-separated names within a value.
sort_names() {
  echo "$1" | tr ';' '\n' | LC_ALL=C sort | paste -sd ';' -
}

# Build the annotation block.
generate_annotations() {
  local entries
  entries=$(collect_names | grep -v '^---$' | LC_ALL=C sort)

  if [[ -z "$entries" ]]; then
    echo "WARNING: No Role or ClusterRole found in chart templates" >&2
    return 1
  fi

  local -A role_map
  local -A clusterrole_map

  while IFS=' ' read -r chart kind name; do
    if [[ "$kind" == "Role" ]]; then
      if [[ -n "${role_map[$chart]+x}" ]]; then
        role_map[$chart]="${role_map[$chart]};${name}"
      else
        role_map[$chart]="$name"
      fi
    elif [[ "$kind" == "ClusterRole" ]]; then
      if [[ -n "${clusterrole_map[$chart]+x}" ]]; then
        clusterrole_map[$chart]="${clusterrole_map[$chart]};${name}"
      else
        clusterrole_map[$chart]="$name"
      fi
    fi
  done <<< "$entries"

  local has_roles=false
  local has_clusterroles=false
  [[ ${#role_map[@]} -gt 0 ]] && has_roles=true
  [[ ${#clusterrole_map[@]} -gt 0 ]] && has_clusterroles=true

  # Print role annotations (one line per chart, names semicolon-separated and sorted).
  if $has_roles; then
    for chart in $(printf '%s\n' "${!role_map[@]}" | LC_ALL=C sort); do
      local sorted_names
      sorted_names=$(sort_names "${role_map[$chart]}")
      echo "// +kubebuilder:rbac:groups=\"rbac.authorization.k8s.io\",resources=roles,verbs=${ROLE_VERBS},resourceNames=${sorted_names}"
    done
  fi

  # Blank comment line between roles and clusterroles (only if both exist).
  if $has_roles && $has_clusterroles; then
    echo "//"
  fi

  # Print clusterrole annotations (one line per chart, names semicolon-separated and sorted).
  if $has_clusterroles; then
    for chart in $(printf '%s\n' "${!clusterrole_map[@]}" | LC_ALL=C sort); do
      local sorted_names
      sorted_names=$(sort_names "${clusterrole_map[$chart]}")
      echo "// +kubebuilder:rbac:groups=\"rbac.authorization.k8s.io\",resources=clusterroles,verbs=${ROLE_VERBS},resourceNames=${sorted_names}"
    done
  fi
}

# Replace content between markers in the target file.
update_file() {
  # Validate markers exist before generating.
  if ! grep -qF "$BEGIN_MARKER" "$TARGET_FILE"; then
    echo "ERROR: Begin marker '$BEGIN_MARKER' not found in $TARGET_FILE" >&2
    return 1
  fi
  if ! grep -qF "$END_MARKER" "$TARGET_FILE"; then
    echo "ERROR: End marker '$END_MARKER' not found in $TARGET_FILE" >&2
    return 1
  fi

  local annotations
  annotations=$(generate_annotations)

  # Validate all generated lines match expected kubebuilder annotation or blank comment format.
  while IFS= read -r line; do
    if [[ "$line" != "// +kubebuilder:rbac:"* && "$line" != "//" ]]; then
      echo "ERROR: Unexpected annotation format: '$line'" >&2
      return 1
    fi
  done <<< "$annotations"

  local begin_line
  begin_line=$(grep -nF "$BEGIN_MARKER" "$TARGET_FILE" | head -1 | cut -d: -f1)
  local end_line
  end_line=$(grep -nF "$END_MARKER" "$TARGET_FILE" | head -1 | cut -d: -f1)

  if [[ "$begin_line" -ge "$end_line" ]]; then
    echo "ERROR: Begin marker (line $begin_line) must come before end marker (line $end_line)" >&2
    return 1
  fi

  # Replace content between markers using a single-pass awk.
  local tmp
  tmp=$(mktemp)
  trap 'rm -f "$tmp"' RETURN

  _ANNOTATIONS="$annotations" awk '
    /\+auto:rbac:begin/ { print; printf "%s\n", ENVIRON["_ANNOTATIONS"]; skip=1; next }
    /\+auto:rbac:end/   { skip=0 }
    skip { next }
    { print }
  ' "$TARGET_FILE" > "$tmp" && mv "$tmp" "$TARGET_FILE"

  echo "Updated $TARGET_FILE"
}

update_file

#!/bin/bash
set -euo pipefail

# Resolves affected e2e component/service test names from changed files.
# Uses convention-based path matching — no static mapping to maintain.
#
# Usage:
#   bash resolve_affected_components.sh              # uses git diff vs origin/main
#   echo "path/to/file.go" | bash resolve_affected_components.sh --stdin
#
# Output (stdout):
#   COMPONENTS=dashboard,kserve,trustyai,modelcontroller,modelsasservice
#   SERVICES=gateway
#
# If shared/cross-cutting code changed or an error occurs, outputs nothing
# (caller should run all tests).

# ============================================================================
# Configuration — edit these when adding components, deps, or manifest keys
# ============================================================================

# Path patterns that resolve to a specific component (capture group 1 = name)
COMPONENT_PATTERNS=(
  "^internal/controller/components/([^/]+)/"
  "^internal/controller/modules/([^/]+)/"
  "^api/components/v1alpha1/([a-z]+)_types.*\.go$"
  "^tests/e2e/([a-z][a-z0-9_]+)_test\.go$"
)

# Path patterns that resolve to a specific service (capture group 1 = name)
SERVICE_PATTERNS=(
  "^internal/controller/services/([^/]+)/"
  "^api/services/v1alpha1/([a-z]+)_types.*\.go$"
)

# E2E test files that are shared infrastructure (not component-specific)
SHARED_E2E_FILES="controller|helper|test_context|test_tag|creation|deletion|cleanup|cfmap_deletion|dag_ordering|odh_manager|cluster_health|resource_fetcher|debug_utils|resource_options|v2tov3upgrade|circuit_breaker|components"

# Dependency graph (forward expansion)
declare -A DEPS=(
  [kserve]="trustyai,modelcontroller,modelsasservice"
  [modelregistry]="modelcontroller"
  [workbenches]="kueue,mlflowoperator"
)

# Manifest key -> component name mapping (for get_all_manifests.sh diff parsing)
# Only needed for keys that don't match component test names 1:1.
declare -A MANIFEST_KEY_TO_COMPONENT=(
  [maas]="modelsasservice"
  [workbenches/kf-notebook-controller]="workbenches"
  [workbenches/odh-notebook-controller]="workbenches"
  [workbenches/notebooks]="workbenches"
)

# ============================================================================
# Functions
# ============================================================================

resolve_manifest_components() {
  local merge_base="$1"

  local changed_keys
  changed_keys=$(git diff "${merge_base}...HEAD" -- get_all_manifests.sh \
    | grep '^[+-]' \
    | grep -oE '\["[a-zA-Z0-9_/.-]+"\]' \
    | tr -d '[]"' \
    | sort -u)

  if [[ -z "$changed_keys" ]]; then
    echo "shared"
    return
  fi

  local result=""
  while IFS= read -r key; do
    [[ -z "$key" ]] && continue
    local comp
    if [[ -n "${MANIFEST_KEY_TO_COMPONENT[$key]:-}" ]]; then
      comp="${MANIFEST_KEY_TO_COMPONENT[$key]}"
    else
      comp="$key"
    fi
    result="${result:+$result,}$comp"
    echo "  get_all_manifests.sh [$key] -> component:$comp" >&2
  done <<< "$changed_keys"

  echo "$result"
}

get_changed_files() {
  if [[ "${1:-}" == "--stdin" ]]; then
    cat
    return
  fi

  local merge_base
  merge_base=$(git merge-base HEAD origin/main 2>/dev/null) || {
    echo "WARNING: cannot determine merge base" >&2
    return 1
  }

  local head
  head=$(git rev-parse HEAD 2>/dev/null)

  if [[ "$merge_base" == "$head" ]]; then
    echo "INFO: HEAD is merge base (on main?) -- no diff" >&2
    return 1
  fi

  git diff --name-only "${merge_base}...HEAD"
}

# Returns: "component:NAME", "service:NAME", "manifest", or "shared"
classify_file() {
  local file="$1"

  # Check component patterns
  for pattern in "${COMPONENT_PATTERNS[@]}"; do
    if [[ "$file" =~ $pattern ]]; then
      local name="${BASH_REMATCH[1]}"
      # Filter out shared e2e test infrastructure files
      if [[ "$pattern" == *"tests/e2e"* ]] && [[ "$name" =~ ^($SHARED_E2E_FILES)$ ]]; then
        echo "shared"
      else
        echo "component:${name}"
      fi
      return
    fi
  done

  # Check service patterns
  for pattern in "${SERVICE_PATTERNS[@]}"; do
    if [[ "$file" =~ $pattern ]]; then
      echo "service:${BASH_REMATCH[1]}"
      return
    fi
  done

  # get_all_manifests.sh needs diff parsing (handled in main)
  if [[ "$file" == "get_all_manifests.sh" ]]; then
    echo "manifest"
    return
  fi

  # Everything else is shared/cross-cutting
  echo "shared"
}

# ============================================================================
# Main
# ============================================================================

main() {
  local files
  files=$(get_changed_files "$@") || exit 1

  if [[ -z "$files" ]]; then
    echo "INFO: no changed files" >&2
    exit 1
  fi

  local merge_base=""
  if [[ "${1:-}" != "--stdin" ]]; then
    merge_base=$(git merge-base HEAD origin/main 2>/dev/null) || true
  fi

  declare -A components=()
  declare -A services=()
  local has_shared=false
  local has_manifest=false

  while IFS= read -r file; do
    [[ -z "$file" ]] && continue

    local classification
    classification=$(classify_file "$file")

    case "$classification" in
      component:*)
        local name="${classification#component:}"
        components["$name"]=1
        echo "  $file -> component:$name" >&2
        ;;
      service:*)
        local name="${classification#service:}"
        services["$name"]=1
        echo "  $file -> service:$name" >&2
        ;;
      manifest)
        has_manifest=true
        ;;
      shared)
        echo "  $file -> shared (run all)" >&2
        has_shared=true
        break
        ;;
    esac
  done <<< "$files"

  if [[ "$has_shared" == "true" ]]; then
    echo "INFO: shared code changed -- all tests will run" >&2
    exit 1
  fi

  if [[ "$has_manifest" == "true" ]]; then
    if [[ -z "$merge_base" ]]; then
      echo "  get_all_manifests.sh -> shared (no merge base for diff)" >&2
      echo "INFO: cannot parse manifest diff -- all tests will run" >&2
      exit 1
    fi
    local manifest_components
    manifest_components=$(resolve_manifest_components "$merge_base")
    if [[ "$manifest_components" == "shared" ]]; then
      echo "INFO: get_all_manifests.sh structural change -- all tests will run" >&2
      exit 1
    fi
    IFS=',' read -ra mc <<< "$manifest_components"
    for c in "${mc[@]}"; do
      components["$c"]=1
    done
  fi

  # Expand dependencies
  for comp in "${!components[@]}"; do
    if [[ -n "${DEPS[$comp]:-}" ]]; then
      IFS=',' read -ra deps <<< "${DEPS[$comp]}"
      for dep in "${deps[@]}"; do
        if [[ -z "${components[$dep]:-}" ]]; then
          components["$dep"]=1
          echo "  + $dep (dependency of $comp)" >&2
        fi
      done
    fi
  done

  # Output
  local comp_list=""
  for c in $(echo "${!components[@]}" | tr ' ' '\n' | sort); do
    comp_list="${comp_list:+$comp_list,}$c"
  done

  local svc_list=""
  for s in $(echo "${!services[@]}" | tr ' ' '\n' | sort); do
    svc_list="${svc_list:+$svc_list,}$s"
  done

  if [[ -z "$comp_list" && -z "$svc_list" ]]; then
    echo "INFO: no components or services affected" >&2
    exit 1
  fi

  echo "COMPONENTS=${comp_list}"
  echo "SERVICES=${svc_list}"
}

main "$@"

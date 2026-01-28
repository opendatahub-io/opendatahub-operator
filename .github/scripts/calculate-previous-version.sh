#!/bin/bash
#
# Versioning rules:
#   1. EA.2 always replaces EA.1: 3.5.0-ea.2 -> 3.5.0-ea.1
#   2. EA.1 always replaces last stable: 3.5.0-ea.1 -> 3.4.0
#   3. Stable (GA) always replaces EA.2: 3.5.0 -> 3.5.0-ea.2
#   4. Patch releases chain: 3.5.1 -> 3.5.0
#

set -euo pipefail

VERSION=${1:-}

if [[ -z "${VERSION}" ]]; then
  echo >&2 "Usage: $0 <version>"
  exit 1
fi

VERSION="${VERSION#[vV]}"

if [[ "$VERSION" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)-ea\.2$ ]]; then
  # EA.2 version -> previous is EA.1
  echo "${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.${BASH_REMATCH[3]}-ea.1"

elif [[ "$VERSION" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)-ea\.1$ ]]; then
  # EA.1 version -> previous is last stable minor
  major="${BASH_REMATCH[1]}"
  minor="${BASH_REMATCH[2]}"

  if [[ "$minor" -eq 0 ]]; then
    # Edge case: 4.0.0-ea.1 -> 3.0.0
    echo "$((major - 1)).0.0"
  else
    echo "${major}.$((minor - 1)).0"
  fi

elif [[ "$VERSION" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)-ea\.([0-9]+)$ ]]; then
  # Unsupported EA version (ea.3, ea.4, etc.)
  echo >&2 "Error: Unsupported EA version '$VERSION'. Only ea.1 and ea.2 are supported."
  exit 1

elif [[ "$VERSION" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
  # Stable version (no prerelease)
  major="${BASH_REMATCH[1]}"
  minor="${BASH_REMATCH[2]}"
  patch="${BASH_REMATCH[3]}"

  if [[ "$patch" -gt 0 ]]; then
    # Patch release: 3.5.1 -> 3.5.0
    echo "${major}.${minor}.$((patch - 1))"
  else
    # GA release: 3.5.0 -> 3.5.0-ea.2
    echo "${major}.${minor}.${patch}-ea.2"
  fi

else
  echo >&2 "Error: Invalid version format '$VERSION'"
  echo >&2 "Expected: X.Y.Z, X.Y.Z-ea.1, or X.Y.Z-ea.2"
  exit 1
fi

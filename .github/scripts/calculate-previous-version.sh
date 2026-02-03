#!/bin/bash
#
# Calculate the previous version for OLM upgrade path.
#
# IMPORTANT: This script runs AFTER release-staging.yaml has already created
# the tag for the current version. Therefore, the "latest" tag in the repo is
# the current release, not the previous one. We must exclude it to find the
# actual previous release.
#
# Logic:
#   1. If manual override provided: validate and use it
#   2. Otherwise: find most recent tag (excluding current version) = previous release
#

set -euo pipefail

VERSION=${1:-}
PREV_VERSION_OVERRIDE=${2:-}

if [[ -z "${VERSION}" ]]; then
  echo >&2 "Usage: $0 <version> [previous-version]"
  echo >&2 "Examples:"
  echo >&2 "  $0 3.5.0-ea.1"
  echo >&2 "  $0 3.5.0-ea.1 3.4.2"
  exit 1
fi

VERSION="${VERSION#[vV]}"

if [[ -n "${PREV_VERSION_OVERRIDE}" ]]; then
  PREV_VERSION_OVERRIDE="${PREV_VERSION_OVERRIDE#[vV]}"

  semver_pattern='^([0-9]+)\.([0-9]+)\.([0-9]+)(-[0-9A-Za-z.-]+)?$'
  if [[ ! "$PREV_VERSION_OVERRIDE" =~ $semver_pattern ]]; then
    echo >&2 "Error: Invalid previous version format '$PREV_VERSION_OVERRIDE'"
    echo >&2 "Expected valid semver: X.Y.Z or X.Y.Z-prerelease"
    exit 1
  fi

  echo "$PREV_VERSION_OVERRIDE"
  exit 0
fi

# Find previous version: get second-most-recent tag by excluding current version
# NOTE: We exclude the current version because release-staging.yaml has already
# created this tag before this script runs. Without exclusion, we'd incorrectly
# return the current version as the "previous" version.
prev_tag=$(git tag --list 'v*' --sort=-creatordate 2>/dev/null | \
           grep -Fxv "v${VERSION}" | head -n1)

if [[ -z "$prev_tag" ]]; then
  echo >&2 "Error: No git tags found. Cannot auto-detect previous version."
  echo >&2 ""
  echo >&2 "Please provide previous version manually:"
  echo >&2 "  $0 $VERSION <previous-version>"
  echo >&2 ""
  echo >&2 "Example:"
  echo >&2 "  $0 $VERSION 3.4.0"
  exit 1
fi

# Strip 'v' prefix from output
prev_tag="${prev_tag#v}"

echo "$prev_tag"

#!/bin/bash

set -euo pipefail

VERSION=${1:-}

if [[ -z "${VERSION}" ]]; then
  echo >&2 "Usage: $0 <version>"
  echo >&2 "Example: $0 v3.1.0"
  exit 1
fi

SEMVER_PATTERN="^[vV](0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)(\\-[0-9A-Za-z-]+(\\.[0-9A-Za-z-]+)*)?(\\+[0-9A-Za-z-]+(\\.[0-9A-Za-z-]+)*)?$"

if [[ ! "${VERSION}" =~ $SEMVER_PATTERN ]]; then
  echo >&2 "Error: '${VERSION}' does not match semantic versioning."
  echo >&2 "Please ensure it conforms with https://semver.org/ and starts with 'v' prefix."
  exit 1
fi

echo "Version ${VERSION} is valid"
#!/bin/bash

set -euo pipefail

sem_ver_pattern="^[vV](0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)(\\-[0-9A-Za-z-]+(\\.[0-9A-Za-z-]+)*)?(\\+[0-9A-Za-z-]+(\\.[0-9A-Za-z-]+)*)?$"

die () {
    echo >&2 "$@"
    exit 1
}

validate_semantic_versioning() {
  version=$1

  if [[ ${version} == "" ]]; then
    die "Undefined version. Please use semantic versioning https://semver.org/."
  fi

  # Ensure defined version matches semver rules
  if [[ ! "${version}" =~ $sem_ver_pattern ]]; then
    die "\`${version}\` you defined as a version does not match semantic versioning. Please make sure it conforms with https://semver.org/ and make sure it starts with v prefix."
  fi
}
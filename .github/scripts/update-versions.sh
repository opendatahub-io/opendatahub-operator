#!/bin/bash

set -euo

NEW_VERSION=$1
# get major, minor, and patch versions
MAJOR=$(echo "$NEW_VERSION" | cut -d '.' -f 1)
MINOR=$(echo "$NEW_VERSION" | cut -d '.' -f 2)
PATCH=$(echo "$NEW_VERSION" | cut -d '.' -f 3)
# to support the patch version 2.29.0 to 2.29.1
if [ "$PATCH" -eq 0 ]; then
  OLD_PATCH=0
  OLD_MINOR=$((MINOR - 1))
  OLD_VERSION="$MAJOR.$OLD_MINOR.$OLD_PATCH"
else
  OLD_PATCH=$((PATCH - 1))
  OLD_VERSION="$MAJOR.$MINOR.$OLD_PATCH"
fi

CURRENT_VERSION=$(cat Makefile | grep -w "VERSION ?=" | cut -d ' ' -f 3)
CSV_FILE=config/manifests/bases/opendatahub-operator.clusterserviceversion.yaml
sed -i -e "s/^VERSION ?=.*/VERSION ?= $NEW_VERSION/g" Makefile
sed -i -e "s|containerImage.*|containerImage: quay.io/opendatahub/opendatahub-operator:v$NEW_VERSION|g" $CSV_FILE
sed -i -e "s|createdAt.*|createdAt: \"$(date +"%Y-%-m-%dT00:00:00Z")\"|g" $CSV_FILE
sed -i -e "s|name: opendatahub-operator.v.*|name: opendatahub-operator.v$NEW_VERSION|g" $CSV_FILE
sed -i -e "s|version: $CURRENT_VERSION.*|version: $NEW_VERSION|g" $CSV_FILE

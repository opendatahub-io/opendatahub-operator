#!/bin/bash

set -euo pipefail

NEW_VERSION=$1

CSV_FILE=config/manifests/bases/opendatahub-operator.clusterserviceversion.yaml
# Update the ODH VERSION variable (inside the ifeq ODH_PLATFORM_TYPE OpenDataHub block)
sed -i -e "/^ifeq.*ODH_PLATFORM_TYPE.*OpenDataHub/,/^else$/{s/[[:space:]]*VERSION = .*/\t\tVERSION = $NEW_VERSION/;}" Makefile
sed -i -e "s|containerImage.*|containerImage: quay.io/opendatahub/opendatahub-operator:v$NEW_VERSION|g" $CSV_FILE
sed -i -e "s|createdAt.*|createdAt: \"$(date +"%Y-%-m-%dT00:00:00Z")\"|g" $CSV_FILE
sed -i -e "s|name: opendatahub-operator.v.*|name: opendatahub-operator.v$NEW_VERSION|g" $CSV_FILE
sed -i -e "s|version: [0-9][0-9.]*|version: $NEW_VERSION|g" $CSV_FILE

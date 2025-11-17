#!/bin/bash

set -euo

NEW_VERSION=$1

CSV_FILE=config/manifests/bases/opendatahub-operator.clusterserviceversion.yaml
# Only update the main VERSION variable inside the ifeq block, not other VERSION variables
sed -i -e "/^ifeq.*VERSION/,/^endif/{s/[[:space:]]*VERSION = .*/\tVERSION = $NEW_VERSION/;}" Makefile
sed -i -e "s|containerImage.*|containerImage: quay.io/opendatahub/opendatahub-operator:v$NEW_VERSION|g" $CSV_FILE
sed -i -e "s|createdAt.*|createdAt: \"$(date +"%Y-%-m-%dT00:00:00Z")\"|g" $CSV_FILE
sed -i -e "s|name: opendatahub-operator.v.*|name: opendatahub-operator.v$NEW_VERSION|g" $CSV_FILE
sed -i -e "s|version: [0-9][0-9.]*|version: $NEW_VERSION|g" $CSV_FILE

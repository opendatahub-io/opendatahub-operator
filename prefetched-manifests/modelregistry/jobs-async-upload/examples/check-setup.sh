#!/bin/bash
set -e

echo "server: $(oc whoami --show-server)"
echo "user: $(oc whoami)"
echo "project: $(oc project --short)"

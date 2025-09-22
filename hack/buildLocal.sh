#!/bin/bash
eval $(crc oc-env) && \
podman login --tls-verify=false -u kubeadmin -p $(oc whoami -t) https://default-route-openshift-image-registry.apps-crc.testing && \
make image-build USE_LOCAL=true IMG=default-route-openshift-image-registry.apps-crc.testing/opendatahub-operator-system/opendatahub-operator:latest && \
make deploy IMG=image-registry.openshift-image-registry.svc:5000/opendatahub-operator-system/opendatahub-operator:latest && \
podman push --tls-verify=false default-route-openshift-image-registry.apps-crc.testing/opendatahub-operator-system/opendatahub-operator:latest

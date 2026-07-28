#!/bin/bash
set -e

MR_BASE_URL="https://model-registry-rest.apps.rosa.my-cluster.019m.p3.openshiftapps.com"

MR_TOKEN=$(oc whoami -t)
MODEL_ID=$(curl -sk -H"Authorization: Bearer $MR_TOKEN" "$MR_BASE_URL/api/model_registry/v1alpha3/registered_models" | jq -r '.items | max_by(.lastUpdateTimeSinceEpoch | tonumber) | .id')

oc process --local -f configmap-create-version-template.yaml \
  -p MODEL_VERSION_NAME="v2" \
  -o yaml > model-metadata.yaml

oc process --local -f jobs-async-upload-uri-to-oci-template.yaml \
  -p MODEL_SYNC_MODEL_UPLOAD_INTENT=create_version \
  -p MODEL_SYNC_MODEL_ID="$MODEL_ID" \
  -p MODEL_SYNC_REGISTRY_SERVER_ADDRESS="$MR_BASE_URL" \
  -p MODEL_SYNC_REGISTRY_PORT="443" \
  -p MODEL_SYNC_SOURCE_URI="https://github.com/onnx/models/raw/refs/heads/main/validated/vision/classification/mnist/model/mnist-8.onnx" \
  -p MODEL_SYNC_DESTINATION_OCI_URI="default-route-openshift-image-registry.apps.rosa.my-cluster.019m.p3.openshiftapps.com/project3/mnist-v2:latest" \
  -p DESTINATION_CONNECTION=oci-credentials \
  -o yaml > job.yaml

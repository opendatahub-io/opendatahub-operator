#!/bin/bash
set -e

MR_BASE_URL="https://model-registry-rest.apps.rosa.my-cluster.019m.p3.openshiftapps.com"

MR_TOKEN=$(oc whoami -t)
MODEL_ID=$(curl -sk -H"Authorization: Bearer $MR_TOKEN" "$MR_BASE_URL/api/model_registry/v1alpha3/registered_models" | jq -r '.items | max_by(.lastUpdateTimeSinceEpoch | tonumber) | .id')
MODEL_VERSION_ID=$(curl -sk -H"Authorization: Bearer $MR_TOKEN" "$MR_BASE_URL/api/model_registry/v1alpha3/registered_models/"$MODEL_ID"/versions" | jq -r '.items | max_by(.lastUpdateTimeSinceEpoch | tonumber) | .id')
MODEL_ARTIFACT_ID=$(curl -sk -H"Authorization: Bearer $MR_TOKEN" "$MR_BASE_URL/api/model_registry/v1alpha3/model_versions/$MODEL_VERSION_ID/artifacts" | jq -r '.items | max_by(.lastUpdateTimeSinceEpoch | tonumber) | .id')

oc process --local -f jobs-async-upload-uri-to-oci-template.yaml \
  -p MODEL_SYNC_MODEL_UPLOAD_INTENT=update_artifact \
  -p MODEL_SYNC_MODEL_ID="$MODEL_ID" \
  -p MODEL_SYNC_MODEL_VERSION_ID="$MODEL_VERSION_ID" \
  -p MODEL_SYNC_MODEL_ARTIFACT_ID="$MODEL_ARTIFACT_ID" \
  -p MODEL_SYNC_REGISTRY_SERVER_ADDRESS="$MR_BASE_URL" \
  -p MODEL_SYNC_REGISTRY_PORT="443" \
  -p MODEL_SYNC_SOURCE_URI="https://github.com/onnx/models/raw/refs/heads/main/validated/vision/classification/mnist/model/mnist-8.onnx" \
  -p MODEL_SYNC_DESTINATION_OCI_URI="default-route-openshift-image-registry.apps.rosa.my-cluster.019m.p3.openshiftapps.com/project3/mnist-v3:latest" \
  -p DESTINATION_CONNECTION=oci-credentials \
  -o yaml > job.yaml

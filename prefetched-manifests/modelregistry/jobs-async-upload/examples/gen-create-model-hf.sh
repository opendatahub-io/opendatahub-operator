#!/bin/bash
set -euo pipefail

MR_BASE_URL="https://model-registry-rest.apps.rosa.my-cluster.019m.p3.openshiftapps.com"

oc process --local -f configmap-create-model-template.yaml \
  -p REGISTERED_MODEL_NAME="rh-granite" \
  -p MODEL_VERSION_NAME="v1" \
  -p MODEL_ARTIFACT_NAME="rh-granite" \
  -o yaml > model-metadata.yaml

oc process --local -f jobs-async-upload-uri-to-oci-template.yaml \
  -p MODEL_SYNC_MODEL_UPLOAD_INTENT=create_model \
  -p MODEL_SYNC_REGISTRY_SERVER_ADDRESS="$MR_BASE_URL" \
  -p MODEL_SYNC_REGISTRY_PORT="443" \
  -p MODEL_SYNC_SOURCE_URI="hf://RedHatAI/granite-3.1-8b-instruct-quantized.w4a16" \
  -p MODEL_SYNC_DESTINATION_OCI_URI="default-route-openshift-image-registry.apps.rosa.my-cluster.019m.p3.openshiftapps.com/project3/granite-v1:latest" \
  -p DESTINATION_CONNECTION=oci-credentials \
  -p SOURCE_CONNECTION=hf-credentials \
  -o yaml > job.yaml

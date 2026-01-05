## Async Upload Template

This OpenShift Template generates the Job 'async-upload' developed in: https://github.com/opendatahub-io/model-registry/tree/main/jobs/async-upload

## Examples

### Example scripts

See the [examples](./examples) directory for some example scripts and utilities.

### Before applying jobs

You may wish to delete any configmaps or previous job runs before creating new ones:
```
oc delete configmap model-metadata
oc delete job my-async-upload-job
```
After applying, view logs with:
```
oc logs job/my-async-upload-job -f
```

### Update Artifact

```sh
oc process --local -f jobs-async-upload-s3-to-oci-template.yaml \
  -p MODEL_SYNC_MODEL_UPLOAD_INTENT=update_artifact \
  -p MODEL_SYNC_MODEL_ID=1 \
  -p MODEL_SYNC_MODEL_VERSION_ID=3 \
  -p MODEL_SYNC_MODEL_ARTIFACT_ID=6 \
  -p MODEL_SYNC_REGISTRY_SERVER_ADDRESS=https://... \
  -p MODEL_SYNC_REGISTRY_PORT=443 \
  -p SOURCE_CONNECTION=my-s3-credentials \
  -p DESTINATION_CONNECTION=my-oci-credentials \
  -p MODEL_SYNC_SOURCE_AWS_KEY=path/in/bucket \
  -p MODEL_SYNC_DESTINATION_OCI_URI="oci-route.openshiftapps.com/project/model:latest" \
  -o yaml \
  > job.yaml

oc apply -f job.yaml
```

### Create Model

**Note**: The `create_model` intent requires a ConfigMap containing model metadata.

```sh
# First create the ConfigMap
oc process --local -f configmap-create-model-template.yaml \
  -p REGISTERED_MODEL_NAME="my-model" \
  -p MODEL_VERSION_NAME="1.0.0" \
  -p MODEL_ARTIFACT_NAME="my-artifact" \
  -o yaml \
  > model-metadata.yaml

# Then create the upload job
oc process --local -f jobs-async-upload-s3-to-oci-template.yaml \
  -p MODEL_SYNC_MODEL_UPLOAD_INTENT=create_model \
  -p MODEL_SYNC_REGISTRY_SERVER_ADDRESS=https://your-registry.com \
  -p MODEL_SYNC_REGISTRY_PORT=443 \
  -p SOURCE_CONNECTION=my-s3-credentials \
  -p DESTINATION_CONNECTION=my-oci-credentials \
  -p MODEL_SYNC_SOURCE_AWS_KEY=path/in/bucket \
  -p MODEL_SYNC_DESTINATION_OCI_URI="oci-route.openshiftapps.com/project/model:latest" \
  -o yaml \
  > job.yaml

oc apply -f model-metadata.yaml,job.yaml
```

### Create Version

**Note**: The `create_version` intent requires a ConfigMap containing model metadata.

```bash
# Basic usage for adding a new version to an existing model
oc process --local -f configmap-create-version-template.yaml \
  -p MODEL_VERSION_NAME="2.0.0" \
  -p MODEL_VERSION_DESCRIPTION="Improved model with better accuracy and faster inference" \
  -p MODEL_VERSION_AUTHOR="john-doe" \
  -p MODEL_ARTIFACT_NAME="sentiment-model-v2" \
  -p MODEL_ARTIFACT_FORMAT_NAME="tensorflow" \
  -p MODEL_ARTIFACT_FORMAT_VERSION="2.11" \
  -o yaml \
  > model-metadata.yaml

oc process --local -f jobs-async-upload-s3-to-oci-template.yaml \
  -p MODEL_SYNC_MODEL_UPLOAD_INTENT=create_version \
  -p MODEL_SYNC_REGISTRY_SERVER_ADDRESS=https://your-registry.com \
  -p MODEL_SYNC_REGISTRY_PORT=443 \
  -p SOURCE_CONNECTION=my-s3-credentials \
  -p DESTINATION_CONNECTION=my-oci-credentials \
  -p MODEL_SYNC_SOURCE_AWS_KEY=path/in/bucket \
  -p MODEL_SYNC_DESTINATION_OCI_URI="oci-route.openshiftapps.com/project/model:latest" \
  -o yaml \
  > job.yaml

oc apply -f model-metadata.yaml,job.yaml
```

## Getting IDs

A custom script to get and use the IDs of the latest registered model, version, and artifact, could look something like the below. Replace `my-model-registry-namespace` and `CLUSTER-NAME` at a minimum.

```sh
#!/bin/bash
set -e

MR_NAMESPACE=my-model-registry-namespace

MR_TOKEN=$(oc whoami -t)
# `oc get route` may require login as admin user
MR_BASE_URL="https://$(oc get route -n odh-model-registries "$MR_NAMESPACE"-https -o 'jsonpath={.status.ingress[0].host}')"

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
  -p MODEL_SYNC_SOURCE_URI="https://huggingface.co/RedHatAI/granite-3.1-8b-instruct-quantized.w4a16/resolve/main/model.safetensors" \
  -p MODEL_SYNC_DESTINATION_OCI_URI="default-route-openshift-image-registry.apps.rosa.CLUSTER-NAME.d4bs.p3.openshiftapps.com/minio-manual/model2:latest" \
  -p DESTINATION_CONNECTION=my-oci-credentials \
  -p MODEL_SYNC_DESTINATION_OCI_URI="oci-route.openshiftapps.com/project/model:latest" \
  -o yaml \
  > job.yaml
```

### More configmap examples

```bash
# Basic usage with minimal required fields
oc process --local -f configmap-create-model-template.yaml \
  -p REGISTERED_MODEL_NAME="sentiment-analyzer" \
  -p REGISTERED_MODEL_DESCRIPTION="A machine learning model for sentiment analysis" \
  -p REGISTERED_MODEL_OWNER="data-science-team" \
  -p MODEL_VERSION_NAME="1.0.0" \
  -p MODEL_ARTIFACT_NAME="sentiment-model-v1" \
  -p MODEL_ARTIFACT_FORMAT_NAME="tensorflow" \
  -p MODEL_ARTIFACT_FORMAT_VERSION="2.8" \
  -o yaml \
  > model-metadata.yaml

# Advanced usage with custom properties
oc process --local -f configmap-create-model-template.yaml \
  -p REGISTERED_MODEL_NAME="advanced-sentiment-analyzer" \
  -p REGISTERED_MODEL_DESCRIPTION="Advanced sentiment analysis with transformer architecture" \
  -p REGISTERED_MODEL_OWNER="ml-engineering-team" \
  -p REGISTERED_MODEL_CUSTOM_PROPERTIES='{"project": "nlp-research", "cost_center": "engineering", "team": "ml-ops"}' \
  -p MODEL_VERSION_NAME="1.0.0" \
  -p MODEL_VERSION_DESCRIPTION="Initial production release with BERT architecture" \
  -p MODEL_VERSION_AUTHOR="jane-smith" \
  -p MODEL_VERSION_CUSTOM_PROPERTIES='{"accuracy": 0.95, "f1_score": 0.93, "training_dataset": "sentiment-corpus-v1.2"}' \
  -p MODEL_ARTIFACT_NAME="bert-sentiment-v1" \
  -p MODEL_ARTIFACT_FORMAT_NAME="tensorflow" \
  -p MODEL_ARTIFACT_FORMAT_VERSION="2.8" \
  -p MODEL_ARTIFACT_SOURCE_KIND="HuggingFace" \
  -p MODEL_ARTIFACT_SOURCE_ID="bert-base-uncased" \
  -p MODEL_ARTIFACT_SOURCE_NAME="BERT Base Uncased" \
  -p MODEL_ARTIFACT_CUSTOM_PROPERTIES='{"model_size_mb": 438, "inference_time_ms": 120, "supported_languages": ["en", "es", "fr"]}' \
  -o yaml \
  > model-metadata.yaml
```

### Generate ConfigMap for create_version Intent

```bash
# Basic usage for adding a new version to an existing model
oc process --local -f configmap-create-version-template.yaml \
  -p MODEL_VERSION_NAME="2.0.0" \
  -p MODEL_VERSION_DESCRIPTION="Improved model with better accuracy and faster inference" \
  -p MODEL_VERSION_AUTHOR="john-doe" \
  -p MODEL_ARTIFACT_NAME="sentiment-model-v2" \
  -p MODEL_ARTIFACT_FORMAT_NAME="tensorflow" \
  -p MODEL_ARTIFACT_FORMAT_VERSION="2.11" \
  -o yaml \
  > model-metadata.yaml

# Advanced usage with detailed metadata
oc process --local -f configmap-create-version-template.yaml \
  -p MODEL_VERSION_NAME="2.1.0" \
  -p MODEL_VERSION_DESCRIPTION="Optimized version with quantization and pruning" \
  -p MODEL_VERSION_AUTHOR="ai-optimization-team" \
  -p MODEL_VERSION_CUSTOM_PROPERTIES='{"accuracy": 0.97, "f1_score": 0.95, "model_compression": "quantized", "speedup_factor": 2.3}' \
  -p MODEL_ARTIFACT_NAME="distilbert-sentiment-optimized" \
  -p MODEL_ARTIFACT_FORMAT_NAME="tensorflow" \
  -p MODEL_ARTIFACT_FORMAT_VERSION="2.11" \
  -p MODEL_ARTIFACT_SOURCE_KIND="HuggingFace" \
  -p MODEL_ARTIFACT_SOURCE_ID="distilbert-base-uncased" \
  -p MODEL_ARTIFACT_CUSTOM_PROPERTIES='{"model_size_mb": 265, "inference_time_ms": 85, "compression_ratio": 0.6}' \
  -o yaml \
  > model-metadata.yaml
```

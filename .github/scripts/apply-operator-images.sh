#!/bin/bash

# Apply operator-level image environment variables to manager.yaml
# Reads RELATED_IMAGE_* env vars exported by get-release-branches.js and updates the deployment spec

set -e

MANAGER_FILE="config/manager/manager.yaml"

echo "Updating operator images in manager.yaml..."

HAS_CHANGES=false

while IFS='=' read -r ENV_VAR_NAME IMAGE_VALUE; do
    if [[ -z "$ENV_VAR_NAME" ]] || [[ -z "$IMAGE_VALUE" ]]; then
        continue
    fi

    echo "Processing $ENV_VAR_NAME=$IMAGE_VALUE"

    # Export to environment for yq env() function to access safely
    export YQ_ENV_VAR_NAME="$ENV_VAR_NAME"
    export YQ_IMAGE_VALUE="$IMAGE_VALUE"

    # Use yq's env() function to safely access variables (prevents injection)
    if yq eval '(select(.kind == "Deployment" and .metadata.name == "controller-manager") | .spec.template.spec.containers[0].env[] | select(.name == env(YQ_ENV_VAR_NAME))) | .value' "$MANAGER_FILE" | grep -q .; then
        CURRENT_VALUE=$(yq eval '(select(.kind == "Deployment" and .metadata.name == "controller-manager") | .spec.template.spec.containers[0].env[] | select(.name == env(YQ_ENV_VAR_NAME))) | .value' "$MANAGER_FILE")

        if [[ "$CURRENT_VALUE" != "$IMAGE_VALUE" ]]; then
            yq eval -i '(select(.kind == "Deployment" and .metadata.name == "controller-manager") | .spec.template.spec.containers[0].env[] | select(.name == env(YQ_ENV_VAR_NAME))).value = env(YQ_IMAGE_VALUE)' "$MANAGER_FILE"
            echo "Updated $ENV_VAR_NAME: $CURRENT_VALUE -> $IMAGE_VALUE"
            HAS_CHANGES=true
        else
            echo "  - $ENV_VAR_NAME already set to $IMAGE_VALUE"
        fi
    else
        yq eval -i '(select(.kind == "Deployment" and .metadata.name == "controller-manager") | .spec.template.spec.containers[0].env) += [{"name": env(YQ_ENV_VAR_NAME), "value": env(YQ_IMAGE_VALUE)}]' "$MANAGER_FILE"
        echo "Added $ENV_VAR_NAME=$IMAGE_VALUE"
        HAS_CHANGES=true
    fi

    # Clean up temp env vars
    unset YQ_ENV_VAR_NAME YQ_IMAGE_VALUE
done < <(env | grep '^RELATED_IMAGE_' || true)

if [[ "$HAS_CHANGES" == "true" ]]; then
    echo "Successfully updated manager.yaml"
else
    echo "No changes needed to manager.yaml"
fi

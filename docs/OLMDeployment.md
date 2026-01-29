# OLM Deployment Guide

This guide covers building and deploying the opendatahub-operator using Operator Lifecycle Manager (OLM).

> **Note:** For development/testing, use `make deploy`  or `make install` with `make run-nowebhook` instead. See the [Makefile](../Makefile) for details.

## Quick Links
- [Deployment](#deployment)
- [Cleanup](#cleanup)
- [Troubleshooting](#troubleshooting)

## Prerequisites

1. **Update your `local.mk` file** with these variables:

```bash
VERSION=1.1.1
IMG_TAG=v1.1.1
IMAGE_TAG_BASE=quay.io/example/opendatahub-operator
DEFAULT_MANIFESTS_PATH=./opt/manifests
USE_LOCAL=false
PLATFORM=linux/amd64

# Platform type: "OpenDataHub" for ODH (default), "rhoai" for RHOAI
# ODH_PLATFORM_TYPE=OpenDataHub  # Default - can be omitted for ODH
# ODH_PLATFORM_TYPE=rhoai        # Uncomment for RHOAI

# Image variables (automatically derived from above)
OPERATOR_IMAGE="${IMAGE_TAG_BASE}:${IMG_TAG}"
BUNDLE_IMAGE="${IMAGE_TAG_BASE}-bundle:${IMG_TAG}"
CATALOG_IMAGE="${IMAGE_TAG_BASE}-catalog:${IMG_TAG}"
```

2. **Load environment variables**:

```bash
set -a
source local.mk
set +a

# Set platform-specific variables (override for RHOAI)
if [[ "${ODH_PLATFORM_TYPE}" == "rhoai" ]]; then
  export OPERATOR_NAMESPACE="redhat-ods-operator"
  export OPERATOR_NAME="rhods-operator"
else
  export OPERATOR_NAMESPACE="openshift-operators"
  export OPERATOR_NAME="opendatahub-operator"
fi

# Print configured variables
echo "Platform: ${ODH_PLATFORM_TYPE}"
echo "Operator Namespace: ${OPERATOR_NAMESPACE}"
echo "Operator Name: ${OPERATOR_NAME}"
echo "Operator Image: ${OPERATOR_IMAGE}"
echo "Bundle Image: ${BUNDLE_IMAGE}"
echo "Catalog Image: ${CATALOG_IMAGE}"
```




### Build and Push Images (Optional)

```bash
make image-build IMG=$OPERATOR_IMAGE
make image-push IMG=$OPERATOR_IMAGE
make bundle-build BUNDLE_IMG=$BUNDLE_IMAGE IMG=$OPERATOR_IMAGE
make bundle-push BUNDLE_IMG=$BUNDLE_IMAGE
make catalog-build CATALOG_IMG=$CATALOG_IMAGE BUNDLE_IMG=$BUNDLE_IMAGE IMG=$OPERATOR_IMAGE
make catalog-push CATALOG_IMG=$CATALOG_IMAGE
```

## Deployment

### 1. Verify images are publicly accessible:

```bash
podman pull --quiet --authfile=<(echo '{"auths":{}}') ${OPERATOR_IMAGE} >/dev/null && echo "✓ Operator image pullable: ${OPERATOR_IMAGE}" 
podman pull --quiet --authfile=<(echo '{"auths":{}}') ${BUNDLE_IMAGE} >/dev/null && echo "✓ Bundle image pullable: ${BUNDLE_IMAGE} "
podman pull --quiet --authfile=<(echo '{"auths":{}}') ${CATALOG_IMAGE} >/dev/null && echo "✓ Catalog image pullable: ${CATALOG_IMAGE}" 
```

### 2. Deploy Operator

#### RHOAI only: Create Namespace and OperatorGroup
```bash
# RHOAI ONLY: Create namespace (ODH uses existing openshift-operators)
[[ "${ODH_PLATFORM_TYPE}" == "rhoai" ]] && oc create namespace ${OPERATOR_NAMESPACE}

# RHOAI ONLY: Create OperatorGroup (ODH doesn't need one)
[[ "${ODH_PLATFORM_TYPE}" == "rhoai" ]] && cat << EOF | oc create -f -
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: ${OPERATOR_NAME}-group
  namespace: ${OPERATOR_NAMESPACE}
spec: {}
EOF
```
#### Create CatalogSource and Subscription
```bash
# Create CatalogSource
cat << EOF | oc create -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: ${OPERATOR_NAME}-catalog
  namespace: ${OPERATOR_NAMESPACE}
spec:
  sourceType: grpc
  image: ${CATALOG_IMAGE}
EOF

# Wait for catalog to be ready
oc wait --for=jsonpath='{.status.connectionState.lastObservedState}'=READY \
  catalogsource/${OPERATOR_NAME}-catalog -n ${OPERATOR_NAMESPACE} --timeout=120s

# Create Subscription
cat << EOF | oc create -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: ${OPERATOR_NAME}
  namespace: ${OPERATOR_NAMESPACE}
spec:
  channel: fast
  installPlanApproval: Automatic
  name: ${OPERATOR_NAME}
  source: ${OPERATOR_NAME}-catalog
  sourceNamespace: ${OPERATOR_NAMESPACE}
EOF
```

### 3. Verify Deployment (Optional)

Validate that the operator is using the correct image:

```bash
# Check CSV image
oc get csv -n ${OPERATOR_NAMESPACE} -l operators.coreos.com/${OPERATOR_NAME}.${OPERATOR_NAMESPACE} \
  -o jsonpath='{.items[0].spec.install.spec.deployments[0].spec.template.spec.containers[0].image}'

# Check running deployment image (try both possible deployment names)
oc get deployment ${OPERATOR_NAME}-controller-manager -n ${OPERATOR_NAMESPACE} \
  -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null || \
  oc get deployment ${OPERATOR_NAME} -n ${OPERATOR_NAMESPACE} \
  -o jsonpath='{.spec.template.spec.containers[0].image}'
```

Both commands should return your `$OPERATOR_IMAGE` value.

### 4. Create DSC Resources

**Create DSCInitialization:**

> **Note:** RHOAI will auto-create a default DSCI. 

```bash
oc apply -f your_dsci.yaml
```
- [Example DSCInitialization](../README.md#example-dscinitialization) 

#### Create DSC:
```bash
oc apply -f your_dsc.yaml
```
- [Example DataScienceCluster](../README.md#example-datasciencecluster)

```bash
# Wait for DSC to be ready (note: Kueue may not be ready, but operator will function)
oc wait --for=jsonpath='{.status.phase}'=Ready datasciencecluster/default-dsc --timeout=300s
```

---

## Cleanup


### 1. Identify ODH/RHOAI Application Namespaces 

**Identify all ODH/RHOAI managed namespaces:**

```bash
# List all namespaces managed by ODH/RHOAI then manually delete as needed.
oc get namespaces -l 'opendatahub.io/dashboard'
```

### 2. Delete DSC Resources

```bash
oc delete datasciencecluster default-dsc
oc delete dscinitialization default-dsci
```

### 3. Delete Operator

```bash
oc delete subscription ${OPERATOR_NAME} -n ${OPERATOR_NAMESPACE}
CSV_NAME=$(oc get csv -n ${OPERATOR_NAMESPACE} | grep ${OPERATOR_NAME} | awk '{print $1}')
oc delete csv $CSV_NAME -n ${OPERATOR_NAMESPACE}
oc delete catalogsource ${OPERATOR_NAME}-catalog -n ${OPERATOR_NAMESPACE}

# Delete OperatorGroup and namespace for RHOAI only
[[ "${ODH_PLATFORM_TYPE}" == "rhoai" ]] && oc delete operatorgroup ${OPERATOR_NAME}-group -n ${OPERATOR_NAMESPACE}
[[ "${ODH_PLATFORM_TYPE}" == "rhoai" ]] && oc delete namespace ${OPERATOR_NAMESPACE}
```

### 4. Delete Service Mesh Resources (Optional)

> **Important:** Service Mesh is automatically installed by RHOAI/ODH when DSCI is created. If you plan to reinstall RHOAI/ODH, you must clean up Service Mesh operator and CRDs to prevent version conflicts. If you're only doing a partial cleanup or keeping Service Mesh for other purposes, you can skip this step.

```bash
# Delete Service Mesh operator
oc delete subscription servicemeshoperator3 -n openshift-operators
SERVICEMESH_CSV=$(oc get csv -n openshift-operators | grep servicemeshoperator3 | awk '{print $1}')
oc delete csv $SERVICEMESH_CSV -n openshift-operators

# Delete old/failed install plans
oc get installplan -n openshift-operators | grep servicemesh | awk '{print $1}' | xargs -r oc delete installplan -n openshift-operators

# Delete Istio and Service Mesh CRDs to prevent version conflicts
oc get crd | grep -E 'istio.io|sailoperator.io' | awk '{print $1}' | xargs -r oc delete crd

# Verify CRDs are deleted
oc get crd | grep -E 'istio.io|sailoperator.io'
```

### 5. Delete CRDs (Optional)

> **Warning:** Only delete CRDs if no other resources depend on them.

**Delete core CRDs:**
```bash
oc delete crd datascienceclusters.datasciencecluster.opendatahub.io
oc delete crd dscinitializations.dscinitialization.opendatahub.io
oc delete crd featuretrackers.features.opendatahub.io
```

**Or delete all OpenDataHub CRDs:**
```bash
# Review CRDs first
oc get crd | grep opendatahub

# Delete all
oc get crd | grep opendatahub | awk '{print $1}' | xargs -n 1 oc delete crd
```

### 6. Delete Application Namespaces (Optional)

> **Warning:** This will delete all workbenches, notebooks, pipelines, model deployments, and user data. Back up important data first.
 
**OpenDataHub:**
```bash
oc delete namespace opendatahub
oc delete namespace odh-model-registries
```
 
**RHOAI:**
```bash
oc delete namespace redhat-ods-applications
oc delete namespace redhat-ods-monitoring
oc delete namespace rhods-notebooks
oc delete namespace rhoai-model-registries
```

### 7. Verify Cleanup

```bash
# Check for remaining resources
oc get pods -n openshift-operators | grep ${OPERATOR_NAMESPACE}
oc get crd | grep opendatahub
```

### 8. Unset Environment Variables (Optional)

```bash
unset $(grep -oP '^[A-Z_]+(?==)' local.mk)
```

## Troubleshooting

### Manually Approving Install Plans
These steps are only needed if `installPlanApproval` is set to `Manual`, or if the operator gets in an 'upgrade detected' state:

```bash
# Approve InstallPlan
oc wait --for=condition=InstallPlanPending=true \
  Subscription/${OPERATOR_NAME} -n ${OPERATOR_NAMESPACE} --timeout=120s
INSTALL_PLAN=$(oc get subscription -n ${OPERATOR_NAMESPACE} ${OPERATOR_NAME} -oyaml | yq '.status.installplan.name')
oc patch installplan $INSTALL_PLAN -n ${OPERATOR_NAMESPACE} --type merge --patch '{"spec":{"approved":true}}'
oc wait --for=condition=Installed=true InstallPlan/$INSTALL_PLAN -n ${OPERATOR_NAMESPACE} --timeout=120s
```

### Override operator image
```bash
# Patch CSV with correct operator image (make targets override it)
CSV_NAME=$(oc get csv -n ${OPERATOR_NAMESPACE} | grep ${OPERATOR_NAME} | awk '{print $1}')
JSON_PATCH='[{"op": "replace", "path": "/spec/install/spec/deployments/0/spec/template/spec/containers/0/image", "value": "'"$OPERATOR_IMAGE"'"}]'
oc patch csv $CSV_NAME -n ${OPERATOR_NAMESPACE} --type=json --patch="$JSON_PATCH"
oc wait --for=condition=Available=true \
  Deployment/${OPERATOR_NAME}-controller-manager -n ${OPERATOR_NAMESPACE} --timeout=150s 2>/dev/null || \
  oc wait --for=condition=Available=true \
  Deployment/${OPERATOR_NAME} -n ${OPERATOR_NAMESPACE} --timeout=150s
```
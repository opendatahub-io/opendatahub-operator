# OpenDataHub Installation and Configuration

This directory contains the configuration files for installing OpenDataHub (ODH) with KServe support for the MaaS platform.

## Key Features

- **RawDeployment Mode**: Direct pod deployments without Knative/Serverless overhead
- **NVIDIA NIM Support**: GPU-accelerated inference with NVIDIA Inference Microservices
- **Headless Services**: Direct pod-to-pod communication for low latency
- **OpenShift Integration**: Uses OpenShift's default ingress and certificates

## Installation Methods

### Method 1: Automated Installation (Recommended)

Use the provided installation script which handles all steps in the correct order:

```bash
# From the project root
./scripts/installers/install-odh.sh
```

### Method 2: Manual Installation

1. **Install ODH Operator** from OperatorHub:
   ```bash
   # Via OpenShift Console:
   # 1. Navigate to Operators → OperatorHub
   # 2. Search for "OpenDataHub"
   # 3. Install with default settings
   
   # Or via CLI:
   oc create -f - <<EOF
   apiVersion: operators.coreos.com/v1alpha1
   kind: Subscription
   metadata:
     name: opendatahub-operator
     namespace: openshift-operators
   spec:
     channel: fast
     name: opendatahub-operator
     source: community-operators
     sourceNamespace: openshift-marketplace
   EOF
   ```

2. **Create namespace**:
   ```bash
   kubectl create namespace opendatahub
   ```

3. **Wait for CRDs to be registered**:
   ```bash
   # Wait for the operator to create the CRDs
   kubectl wait --for condition=established --timeout=300s \
     crd/dscinitializations.dscinitialization.opendatahub.io \
     crd/datascienceclusters.datasciencecluster.opendatahub.io
   ```

4. **Apply the configuration**:
   ```bash
   # IMPORTANT: DSCInitialization MUST be created before DataScienceCluster
   kubectl apply -f dscinitialisation.yaml
   
   # Wait for DSCInitialization to be ready
   kubectl wait --for=jsonpath='{.status.phase}'=Ready \
     dscinitializations.dscinitialization.opendatahub.io/default-dsci \
     -n opendatahub --timeout=300s
   
   # Now create the DataScienceCluster
   kubectl apply -f datasciencecluster.yaml
   ```

   Or use kustomize:
   ```bash
   kubectl apply -k deployment/components/odh/
   ```

## Troubleshooting

### Error: "dscinitializations.dscinitialization.opendatahub.io not found"

This is the most common error when creating a DataScienceCluster. It occurs when:
1. The ODH operator is not installed
2. The DSCInitialization resource hasn't been created yet
3. The CRDs haven't been registered yet

**Solution**: Run the fix script:
```bash
./scripts/installers/fix-odh-dsci.sh
```

This script will:
- Check if the ODH operator is installed
- Wait for CRDs to be registered
- Create the DSCInitialization if missing
- Provide next steps for creating the DataScienceCluster

### Manual Troubleshooting Steps

1. **Check operator status**:
   ```bash
   kubectl get csv -n openshift-operators | grep opendatahub
   kubectl logs -n openshift-operators deployment/opendatahub-operator-controller-manager
   ```

2. **Check CRDs**:
   ```bash
   kubectl get crd | grep opendatahub
   ```

3. **Check existing resources**:
   ```bash
   kubectl get dscinitializations -A
   kubectl get datasciencecluster -A
   ```

4. **Check pod status**:
   ```bash
   kubectl get pods -n opendatahub
   kubectl get pods -n kserve
   ```

## Configuration Details

### DSCInitialization
- Configures the foundational settings for ODH
- Sets up Service Mesh integration
- Configures monitoring and trusted CA bundles
- **MUST be created before DataScienceCluster**

### DataScienceCluster
- Deploys the actual ODH components
- Configured for KServe with:
  - **RawDeployment mode**: No Knative/Serverless overhead
  - **NIM support**: For NVIDIA GPU inference
  - **Headless services**: For direct pod communication
  - **OpenShift ingress**: Native OpenShift routing

### Components Status
- ✅ **Enabled**: Dashboard, Workbenches, KServe (with NIM)
- ❌ **Disabled**: ModelMesh, Pipelines, Ray, Kueue, Model Registry, TrustyAI, Training Operator

## Verification

After installation, verify the deployment:

```bash
# Check DSCInitialization status
kubectl get dscinitializations -n opendatahub

# Check DataScienceCluster status
kubectl get datasciencecluster -n opendatahub

# Check KServe components
kubectl get pods -n kserve

# Check if InferenceService CRD is available
kubectl get crd inferenceservices.serving.kserve.io
```

## Integration with MaaS

Once ODH is installed with KServe, you can:
1. Deploy models using KServe InferenceService
2. Use the MaaS API for model management
3. Apply rate limiting and authentication policies
4. Monitor model performance through the ODH dashboard

## Additional Resources

- [OpenDataHub Documentation](https://opendatahub.io/docs/)
- [KServe Documentation](https://kserve.github.io/website/)
- [NVIDIA NIM Documentation](https://docs.nvidia.com/nim/) 
# Open Data Hub Operator Redesign

## Motivation

Following are the general goals for redesigning the existing ODH operator:

- Create an opinionated deployment of ODH components.
- Provide users / cluster administrators with ability to customize components
- Provide ability to enable / disable individual components
- Eliminate limitations of v1 operator:
  - ODH uninstallation fails to clean up, leaving broken clusters that cannot be easily upgraded.
  - ODH deployments, when they fail, provide no useful troubleshooting or debugging information.
  - ODH cannot easily reconcile some change in live declared state.

## Proposed Design

To deploy ODH components seamlessly, ODH operator will watch two CRDs:

- DSCInitialization
- DataScienceCluster

![Operator Re-architecture](images/Operator%20Architecture.png)

### DSCInitialization

- This CR will be created by the ODH operator to perform initial setup that is common for all components
- Some examples of initial setup include creating namespaces, network policies, SCCs, common configmaps and secrets.
- This will be a singleton CR i.e 1 instance of this CR will always be present in the cluster.
- DSCInitialization CR can be deleted to re-run initial setup without requiring re-build of the operator.

### DataScienceCluster

- This CR will be watched by the ODH operator to enable various data science components.
- It is responsible for enabling support for CRDs like Notebooks, DataSciencePipelinesApplication, InferenceService etc. based on the configuration
- Initially only one instance of DataScienceCluster CR will be supported by the operator. A user can extend/update the CR to enable/disable components.
- Detailed API fields are described in the CRD.

## Examples

1. Enable all components

    ```console
      apiVersion: datasciencecluster.opendatahub.io/v1
      kind: DataScienceCluster
      metadata:
        name: example
      spec:
        components:
          codeflare:
            managementState: Managed
          dashboard:
            managementState: Managed
          datasciencepipelines:
            managementState: Managed
          kserve:
            managementState: Managed
            serving:
              ingressGateway:
                certificate:
                  type: OpenshiftDefaultIngress
              managementState: Managed
              name: knative-serving
          modelmeshserving:
            managementState: Managed
          modelregistry:
            managementState: Removed
            registriesNamespace: "rhoai-model-registries"
          ray:
            managementState: Managed
          kueue:
            managementState: Managed
          trainingoperator:
            managementState: Managed
          trustyai:
            managementState: Managed
          workbenches:
            managementState: Managed
          trustyai:
            managementState: Managed
          feastoperator:
            managementState: Managed
        ```

2. Enable only Dashboard and Workbenches(Jupyter Notebooks)

    ```console
      apiVersion: datasciencecluster.opendatahub.io/v1
      kind: DataScienceCluster
      metadata:
        name: example
      spec:
        components:
          dashboard:
            managementState: Managed
          workbenches:
            managementState: Managed 
    ```

3. Enable Data Science Pipelines

    ```console
      apiVersion: datasciencecluster.opendatahub.io/v1
      kind: DataScienceCluster
      metadata:
        name: example
      spec:
        components:
          datasciencepipelines:
            managementState: Managed
    ```

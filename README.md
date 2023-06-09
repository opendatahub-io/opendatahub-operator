# Open Data Hub Operator

This operator is the primary operator for Open Data Hub. It is responsible for enabling Data science applications like 
Jupyter Notebooks, Modelmesh serving, Datascience pipelines etc. The operator makes use of `DataScienceCluster` CRD to deploy
and configure these applications.

## Usage

### Installation

The latest version of operator can be installed from the community-operators catalog on OperatorHub. It can also be build
and installed from source manually, see the Developer guide for further instructions.

### Developer Guide

#### Pre-requisites

- Go version **go1.18.9**
- operator-sdk version can be updated to **v1.24.1**

#### Build Image

- Custom operator image can be built using your local repository
    ```
    make image -e IMG=quay.io/<username>/opendatahub-operator:<custom-tag>
    ```
  The default image used is `quay.io/opendatahub/opendatahub-operator:dev-<VERSION>`


- Once the image is created, the operator can be deployed either directly, or through OLM. For each deployment method a
  kubeconfig should be exported
  ```
  export KUBECONFIG=<path to kubeconfig>
  ```

#### Deployment

**Deploying operator locally**

- Define operator namespace
  ```
  export OPERATOR_NAMESPACE=<namespace-to-install-operator>
  ```
- Deploy the created image in your cluster using following command:
  ```
  make deploy -e IMG=quay.io/<username>/opendatahub-operator:<custom-tag>
  ```

- To remove resources created during installation use:
  ```
  make undeploy
  ```

**Deploying operator using OLM**

- Define operator namespace
  ```
  export OPERATOR_NAMESPACE=<namespace-to-install-operator>
  ```

- To create a new bundle, run following command:
  ```commandline
  make bundle
  ```
  **Note** : Skip the above step if you want to run the existing operator bundle.


- Build Bundle Image:
  ```
  make bundle-build bundle-push BUNDLE_IMG=quay.io/<username>/opendatahub-operator-bundle:<VERSION>
  ```

- Run the Bundle on a cluster:
  ```commandline
  operator-sdk run bundle quay.io/<username>/opendatahub-operator-bundle:<VERSION> --namespace $OPERATOR_NAMESPACE
  ```

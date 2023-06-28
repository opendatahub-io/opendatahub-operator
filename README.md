
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

  ```commandline
  make image -e IMG=quay.io/<username>/opendatahub-operator:<custom-tag>
  ```
  
  or (for example to user vhire)

  ```commandline
  make image -e IMAGE_OWNER=vhire
  ```

  The default image used is `quay.io/opendatahub/opendatahub-operator:dev-0.0.1` when not supply argument for `make image`

- Once the image is created, the operator can be deployed either directly, or through OLM. For each deployment method a
  kubeconfig should be exported

  ```commandline
  export KUBECONFIG=<path to kubeconfig>
  ```

#### Deployment

**Deploying operator locally**

- Define operator namespace

  ```commandline
  export OPERATOR_NAMESPACE=<namespace-to-install-operator>
  ```

- Deploy the created image in your cluster using following command:

  ```commandline
  make deploy -e IMG=quay.io/<username>/opendatahub-operator:<custom-tag> [-e OPERATOR_NAMESPACE=<namespace-to-install-operator>]
  ```

- To remove resources created during installation use:

  ```commandline
  make undeploy
  ```

**Deploying operator using OLM**

- To create a new bundle in defined operator namespace, run following command:
  
  ```commandline
  export OPERATOR_NAMESPACE=<namespace-to-install-operator>
  make bundle
  ```

  **Note** : Skip the above step if you want to run the existing operator bundle.

- Build Bundle Image:
  
  ```commandline
  make bundle-build bundle-push BUNDLE_IMG=quay.io/<username>/opendatahub-operator-bundle:<VERSION>
  ```

- Run the Bundle on a cluster:
  
  ```commandline
  operator-sdk run bundle quay.io/<username>/opendatahub-operator-bundle:<VERSION> --namespace $OPERATOR_NAMESPACE
  ```

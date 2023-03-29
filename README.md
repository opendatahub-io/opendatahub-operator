# Open Data Hub Operator

## Introduction

Open Data Hub operator is a downstream project of [kfctl](https://github.com/kubeflow/kfctl) operator that manages the 
[KfDef](config/crd/bases/kfdef.apps.kubeflow.org_kfdefs.yaml) Custom Resource . This CR is used to deploy [Open Data Hub components](https://github.com/opendatahub-io/odh-manifests/blob/master/kfdef/odh-core.yaml) in the
OpenShift cluster. 

## Usage

### Installation

The latest version of operator can be installed from the community-operators catalog on OperatorHub. It can also be build 
and installed from source manually, see the Developer guide for further instructions.

### Developer Guide

#### Pre-requisites

- Go version **go1.18.4** 
- operator-sdk version can be updated to **v1.24.1**

#### Build Image

- Custom operator image can be built using your local repository
    ```
    make image -e IMG=quay.io/<username>/opendatahub-operator:<custom-tag>
    ```
   The default image used is `quay.io/opendatahub/opendatahub-operator:dev-<VERSION>`


- Once the image is create, the operator can be deployed either directly, or through OLM. For each deployment method a
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


### Example KfDefs

When the operator is installed sucessfully in the cluster, a user can make use of
following KfDefs to install Open Data Hub components:

- [KfDef](https://github.com/opendatahub-io/odh-manifests/blob/master/kfdef/odh-core.yaml) for Core Components



### Run e2e Tests

A user can run the e2e tests in the same namespace as the operator. To deploy
opendatahub-operator refer to [this](#deployment) section. The
following environment variables must be set when running locally:

```shell
export KUBECONFIG=/path/to/kubeconfig
export OPERATOR_NAMESPACE=<namespace-of-operator>
```

Once the above variables are set, run the following:

```shell
make e2e-test
```

Additional flags that can be passed to e2e-tests by setting up `E2E_TEST_FLAGS`
variable. Following table lists all the available flags to run the tests:

| Flag            | Description                                                                                                                                         | Default value |
|-----------------|-----------------------------------------------------------------------------------------------------------------------------------------------------|---------------|
| --skip-deletion | To skip running  of `kfdef-deletion` test that includes deleting `KfDef` resources. Assign this variable to `true` to skip KfDef deletion. | false         |



Example command to run full test suite in a custom namespace, skipping the test
for KfDef deletion.

```shell
make e2e-test -e OPERATOR_NAMESPACE=<YOUR_NAMESPACE> -e E2E_TEST_FLAGS="--skip-deletion=true"
```


This operator is the primary operator for Open Data Hub. It is responsible for enabling Data science applications like 
Jupyter Notebooks, Modelmesh serving, Datascience pipelines etc. The operator makes use of `DataScienceCluster` CRD to deploy
and configure these applications.

## Usage

### Installation

The latest version of operator can be installed from the `community-operators` catalog on `OperatorHub`. It can also be build
and installed from source manually, see the Developer guide for further instructions.

1. Subscribe to operator by creating following subscription
    ```console
    cat <<EOF | oc create -f -
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

2. Create [DataScienceCluster](#example-datasciencecluster) CR to enable components

## Dev Preview

Developer Preview of the new Open Data Hub operator codebase is now avaible.
Refer [Dev-Preview.md](./docs/Dev-Preview.md) for testing preview features.


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
  make deploy -e IMG=quay.io/<username>/opendatahub-operator:<custom-tag> -e OPERATOR_NAMESPACE=<namespace-to-install-operator>
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

### Example DataScienceCluster 

When the operator is installed successfully in the cluster, a user can create a `DataScienceCluster` CR to enable ODH 
components. At a given time, ODH supports only **one** instance of the CR, which can be updated to get custom list of components.

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
          modelmeshserving:
            managementState: Managed
          ray:
            managementState: Managed
          workbenches:
            managementState: Managed
    ```
2. Enable only Dashboard and Workbenches

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

**Note:** Default value for a component is `false`.


### Run e2e Tests

A user can run the e2e tests in the same namespace as the operator. To deploy
opendatahub-operator refer to [this](#deployment) section. The
following environment variables must be set when running locally:

```shell
export KUBECONFIG=/path/to/kubeconfig
```

Ensure when testing RHODS operator in dev mode, no ODH CSV exists
Once the above variables are set, run the following:

```shell
make e2e-test
```

Additional flags that can be passed to e2e-tests by setting up `E2E_TEST_FLAGS`
variable. Following table lists all the available flags to run the tests:

| Flag            | Description                                                                                                                                                        | Default value |
|-----------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------|---------------|
| --skip-deletion | To skip running  of `dsc-deletion` test that includes deleting `DataScienceCluster` resources. Assign this variable to `true` to skip DataScienceCluster deletion. | false         |



Example command to run full test suite skipping the test
for DataScienceCluster deletion.

```shell
make e2e-test -e OPERATOR_NAMESPACE=<namespace> -e E2E_TEST_FLAGS="--skip-deletion=true"
```
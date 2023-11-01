
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

Developer Preview of the new Open Data Hub operator codebase is now available.
Refer [Dev-Preview.md](./docs/Dev-Preview.md) for testing preview features.

### Developer Guide

#### Pre-requisites

- Go version **go1.18.9**
- operator-sdk version can be updated to **v1.24.1**

#### Download manifests

The `get_all_manifests.sh` script facilitates the process of fetching manifests from remote git repositories. It is configured to work with a predefined map of components and their corresponding manifest locations.

#### Structure of `COMPONENT_MANIFESTS`

Each component is associated with its manifest location in the `COMPONENT_MANIFESTS` map. The key is the component's name, and the value is its location, formatted as `<repo-org>:<repo-name>:<branch-name>:<source-folder>:<target-folder>`

#### Workflow

1. The script clones the remote repository `<repo-org>/<repo-name>` from the specified `<branch-name>`.
2. It then copies the content from the relative path `<source-folder>` to the local `odh-manifests/<target-folder>` folder.

#### Local Storage

The script utilizes a local, empty folder named `odh-manifests` to host all required manifests, sourced either directly from the component’s source repository or the default `odh-manifests` git repository.

#### Adding New Components

To include a new component in the list of manifest repositories, simply extend the `COMPONENT_MANIFESTS` map with a new entry, as shown below:

```shell
declare -A COMPONENT_MANIFESTS=(
  // existing components ...
  ["new-component"]="<repo-org>:<repo-name>:<branch-name>:<source-folder>:<target-folder>"
)
```
#### Customizing Manifests Source
You have the flexibility to change the source of the manifests. Invoke the `get_all_manifests.sh` script with specific flags, as illustrated below:

```shell
./get_all_manifests.sh --odh-dashboard="maistra:odh-dashboard:test-manifests:manifests:odh-dashboard"
```

If the flag name matches components key defined in `COMPONENT_MANIFESTS` it will overwrite its location, otherwise the command will fail.

##### for local development

```
make get-manifests
```

This first cleanup your local `odh-manifests` folder.
Ensure back up before run this command if you have local changes of manifests want to reuse later.

##### for build operator image

```

make image-build
```

By default, building an image without any local changes(as a clean build)
This is what the production build system is doing.

In order to build an image with local `odh-manifests` folder, to set `IMAGE_BUILD_FLAGS ="--build-arg USE_LOCAL=true"` in make.
e.g `make image-build -e IMAGE_BUILD_FLAGS="--build-arg USE_LOCAL=true"`

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

### Test with customized manifests

There are 2 ways to test your changes with modification:

1. set `devFlags.ManifestsUri` field of DSCI instance during runtime: this will pull down manifests from remote git repo
    by using this method, it overwrites manifests and component images if images are set in the params.env file
2. [Under implementation] build operator image with local manifests   

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

### Run functional Tests

The functional tests are writted based on [ginkgo](https://onsi.github.io/ginkgo/) and [gomega](https://onsi.github.io/gomega/). In order to run the tests, the user needs to setup the envtest which provides a mocked kubernetes cluster. A detailed explanation on how to configure envtest is provided [here](https://book.kubebuilder.io/reference/envtest.html#configuring-envtest-for-integration-tests).

To run the test on individual controllers, change directory into the contorller's folder and run
```shell
ginkgo -v
```

This provides detailed logs of the test spec.

**Note:** When runninng tests for each controller, make sure to add the `BinaryAssetsDirectory` attribute in the `envtest.Environment` in the `suite_test.go` file. The value should point to the path where the envtest binaries are installed.

In order to run tests for all the controllers, we can use the `make` command
```shell
make unit-test
```
**Note:** The make command should be executed on the root project level.
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

### Troubleshooting

Please refer to [troubleshooting documentation](docs/troubleshooting.md)
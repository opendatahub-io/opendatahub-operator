
This operator is the primary operator for Open Data Hub. It is responsible for enabling Data science applications like 
Jupyter Notebooks, Modelmesh serving, Datascience pipelines etc. The operator makes use of `DataScienceCluster` CRD to deploy
and configure these applications.

### Table of contents
- [Usage](#usage)
  - [Prerequisites](#prerequisites)
  - [Installation](#installation)
- [Developer Guide](#developer-guide)
    - [Pre-requisites](#pre-requisites)
    - [Download manifests](#download-manifests)
    - [Structure of `COMPONENT_MANIFESTS`](#structure-of-component_manifests)
    - [Workflow](#workflow)
    - [Local Storage](#local-storage)
    - [Adding New Components](#adding-new-components)
    - [Customizing Manifests Source](#customizing-manifests-source)
      - [for local development](#for-local-development)
      - [for build operator image](#for-build-operator-image)
    - [Build Image](#build-image)
    - [Deployment](#deployment)
  - [Test with customized manifests](#test-with-customized-manifests)
  - [Update API docs](#update-api-docs)
  - [Example DSCInitialization](#example-dscinitialization)
  - [Example DataScienceCluster](#example-datasciencecluster)
  - [Run functional Tests](#run-functional-tests)
  - [Run e2e Tests](#run-e2e-tests)
  - [API Overview](#api-overview)
  - [Component Integration](#component-integration)
  - [Troubleshooting](#troubleshooting)
  - [Upgrade testing](#upgrade-testing)

## Usage

### Prerequisites
If `single model serving configuration` is used or if `Kserve` component is used then please make sure to install the following operators before proceeding to create a DSCI and DSC instances.
 - [Authorino operator](https://github.com/Kuadrant/authorino)
 - [Service Mesh operator](https://github.com/Maistra/istio-operator)
 - [Serverless operator](https://github.com/openshift-knative/serverless-operator)

Additionally installing `Authorino operator` & `Service Mesh operator` enhances user-experience by providing a single sign on experience.

### Installation

- The latest version of operator can be installed from the `community-operators` catalog on `OperatorHub`.

  ![ODH operator in OperatorHub](docs/images/OperatorHub%20ODH%20Operator.png)

  Please note that the latest releases are made in the `Fast` channel.

- It can also be build
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

  2. Create [DSCInitialization](#example-dscinitialization) CR manually.
    You can also use operator to create default DSCI CR by removing env variable DISABLE_DSC_CONFIG from CSV or changing the value to "false", followed by restarting the operator pod.

  3. Create [DataScienceCluster](#example-datasciencecluster) CR to enable components

## Developer Guide

#### Pre-requisites

- Go version **go1.21**
- operator-sdk version can be updated to **v1.31.1**

#### Download manifests

The [get_all_manifests.sh](/get_all_manifests.sh) script facilitates the process of fetching manifests from remote git repositories. It is configured to work with a predefined map of components and their corresponding manifest locations.

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

```commandline
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
  operator-sdk run bundle quay.io/<username>/opendatahub-operator-bundle:<VERSION> --namespace $OPERATOR_NAMESPACE --decompression-image quay.io/project-codeflare/busybox:1.36
  ```
### Test with customized manifests

There are 2 ways to test your changes with modification:

1. Each component in the `DataScienceCluster` CR has `devFlags.manifests` field, which can be used to pull down the manifests from the remote git repos of the respective components. By using this method, it overwrites manifests and creates customized resources for the respective components.

2. [Under implementation] build operator image with local manifests.

### Update API docs

Whenever a new api is added or a new field is added to the CRD, please make sure to run the command:
  ```commandline
  make api-docs 
  ```
This will ensure that the doc for the apis are updated accordingly.

### Enabled logging

#### Controller level

Logger on all controllers can only be changed from CSV with parameters: --log-mode devel
valid value: "" (as default) || prod || production || devel || development

This mainly impacts logging for operator pod startup, generating common resource, monitoring deployment.

| --log-mode value | mapping Log level   | Comments       |
| ---------------- | ------------------- | -------------- |
| devel            | debug  / 0          | lowest level   |
| ""               | info / 1            | default option |
| default          | info / 1            | default option |
| prod             | error / 2           | highest level  |

#### Component level

Logger on components can be changed by DSCI devFlags during runtime.
By default, if not set .spec.devFlags.logmode, it uses INFO level
Modification applies to all components, not only these "Managed" ones.
Update DSCI CR with .spec.devFlags.logmode, see example :

```console
apiVersion: dscinitialization.opendatahub.io/v1
kind: DSCInitialization
metadata:
  name: default-dsci
spec:
  devFlags:
    logmode: development
  ...
```

Avaiable value for logmode is "devel", "development", "prod", "production".
The first two work the same set to DEBUG level; the later two work the same as using ERROR level.

| .spec.devFlags.logmode | stacktrace level | verbosity | Output   | Comments       |
| ---------------------- | ---------------- | --------- | -------- | -------------- |
| devel                  | WARN             | INFO      | Console  | lowest level, using epoch time  |
| development            | WARN             | INFO      | Console  | same as devel  |
| ""                     | ERROR            | INFO      | JSON     | default option |
| prod                   | ERROR            | INFO      | JSON     | highest level, using human readable timestamp  |
| production             | ERROR            | INFO      | JSON     | same as prod   |

### Example DSCInitialization

Below is the default DSCI CR config

```console
kind: DSCInitialization
apiVersion: dscinitialization.opendatahub.io/v1
metadata:
  name: default-dsci
spec:
  applicationsNamespace: opendatahub
  monitoring:
    managementState: Managed
    namespace: opendatahub
  serviceMesh:
    controlPlane:
      metricsCollection: Istio
      name: data-science-smcp
      namespace: istio-system
    managementState: Managed
  trustedCABundle:
    customCABundle: ''
    managementState: Managed

```

Apply this example with modification for your usage.

### Example DataScienceCluster

When the operator is installed successfully in the cluster, a user can create a `DataScienceCluster` CR to enable ODH 
components. At a given time, ODH supports only **one** instance of the CR, which can be updated to get custom list of components.

1. Enable all components

```console
apiVersion: datasciencecluster.opendatahub.io/v1
kind: DataScienceCluster
metadata:
  name: default-dsc
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
            type: SelfSigned
        managementState: Managed
        name: knative-serving
    kueue:
      managementState: Managed
    modelmeshserving:
      managementState: Managed
    modelregistry:
      managementState: Managed
    ray:
      managementState: Managed
    trainingoperator:
      managementState: Managed
    trustyai:
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
### API Overview

Please refer to [api documentation](docs/api-overview.md)

### Component Integration

Please refer to [components docs](components/README.md)

### Troubleshooting

Please refer to [troubleshooting documentation](docs/troubleshooting.md)

### Upgrade testing

Please refer to [upgrade testing documentation](docs/upgrade-testing.md)

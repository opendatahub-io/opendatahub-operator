This operator is the primary operator for Open Data Hub. It is responsible for enabling Data science applications like
Jupyter Notebooks, Modelmesh serving, Datascience pipelines etc. The operator makes use of `DataScienceCluster` CRD to deploy
and configure these applications.

### Table of contents
- [Usage](#usage)
  - [Prerequisites](#prerequisites)
  - [Installation](#installation)
  - [Configuration](#configuration)
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
  - [Enabled logging](#enabled-logging)
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

- It can also be build and installed from source manually, see the Developer guide for further instructions.

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


### Configuration

- in ODH 2.23.1, we introduced a new feature which allows user to use their own application namespace than default one "opendatahub".

1. for new cluster, as this cluster has not been used for ODH or RHOAI.
   Here we use namespace A for example as targeted application namespace, please follow below steps before install ODH operator:

   - create namespace A
   - add label `opendatahub.io/application-namespace: true`  onto namespace A. Only one namespace in the cluster can have this label.
   - install ODH operator either from UI or by GitOps/CLI
   - once Operator is up and running, manually create DSCI CR by set `.spec.applicationsNamespace:A`
   - wait till DSCI status update to "Ready"
   - continue to create DSC CR

2. for upgrade case, as ODH is running in the cluster.

   Be aware: to switch to a different application namespace can cause more issues and require manual cleanup, therefore we suggest this to be done for new cluster.


## Developer Guide

#### Pre-requisites

- Go version **go1.22**
- operator-sdk version can be updated to **v1.33.0**

#### Download manifests

The [get_all_manifests.sh](/get_all_manifests.sh) script facilitates the process of fetching manifests from remote git repositories. It is configured to work with a predefined map of components and their corresponding manifest locations.

#### Structure of `COMPONENT_MANIFESTS`

Each component is associated with its manifest location in the `COMPONENT_MANIFESTS` map. The key is the component's name, and the value is its location, formatted as `<repo-org>:<repo-name>:<branch-name>:<source-folder>:<target-folder>`

#### Workflow

1. The script clones the remote repository `<repo-org>/<repo-name>` from the specified `<branch-name>`.
2. It then copies the content from the relative path `<source-folder>` to the local `opt/manifests/<target-folder>` folder.

#### Local Storage

The script utilizes a local, empty folder named `opt/manifests` to host all required manifests, sourced directly from each component’s source repository.

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

This first cleanup your local `opt/manifests` folder.
Ensure back up before run this command if you have local changes of manifests want to reuse later.

##### for build operator image

```commandline
make image-build
```

By default, building an image without any local changes(as a clean build)
This is what the production build system is doing.

In order to build an image with local `opt/manifests` folder set `USE_LOCAL` make variable to `true`
e.g `make image-build USE_LOCAL=true"`

#### Build Image

- Custom operator image can be built using your local repository

  ```commandline
  make image IMG=quay.io/<username>/opendatahub-operator:<custom-tag>
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
  make deploy IMG=quay.io/<username>/opendatahub-operator:<custom-tag> OPERATOR_NAMESPACE=<namespace-to-install-operator>
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

Global logger configuration can be changed with an environemnt variable `ZAP_LOG_LEVEL`
or a command line switch `--log-mode <mode>` for example from CSV.
Command line switch has higher priority.
Valid values for `<mode>`: "" (as default) || prod || production || devel || development.

Verbosity level is INFO.
To fine tune zap backend [standard operator sdk zap switches](https://sdk.operatorframework.io/docs/building-operators/golang/references/logging/)
can be used.

Log level can be changed by DSCI devFlags during runtime by setting
.spec.devFlags.logLevel. It accepts the same values as `--zap-log-level`
command line switch. See example :

```console
apiVersion: dscinitialization.opendatahub.io/v1
kind: DSCInitialization
metadata:
  name: default-dsci
spec:
  devFlags:
    logLevel: debug
  ...
```

| logmode     | stacktrace level | verbosity | Output  | Comments                                      |
|-------------|------------------|-----------|---------|-----------------------------------------------|
| devel       | WARN             | INFO      | Console | lowest level, using epoch time                |
| development | WARN             | INFO      | Console | same as devel                                 |
| ""          | ERROR            | INFO      | JSON    | default option                                |
| prod        | ERROR            | INFO      | JSON    | highest level, using human readable timestamp |
| production  | ERROR            | INFO      | JSON    | same as prod                                  |

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
      nim:
        managementState: Managed
      serving:
        ingressGateway:
          certificate:
            type: OpenshiftDefaultIngress
        managementState: Managed
        name: knative-serving
    kueue:
      managementState: Managed
    modelmeshserving:
      managementState: Managed
    modelregistry:
      managementState: Managed
      registriesNamespace: "rhoai-model-registries"
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
  name: default-dsc
spec:
  components:
    dashboard:
      managementState: Managed
    workbenches:
      managementState: Managed
```

**Note:** Default value for managementState in component is `false`.

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

Ensure when testing RHOAI operator in dev mode, no ODH CSV exists
Once the above variables are set, run the following:

```shell
make e2e-test
```

Additional flags that can be passed to e2e-tests by setting up `E2E_TEST_FLAGS`
variable. Following table lists all the available flags to run the tests:

| Flag                       | Description                                                                                                                                                                   | Default value |
|----------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|---------------|
| --skip-deletion            | To skip running  of `dsc-deletion` test that includes deleting `DataScienceCluster` resources. Assign this variable to `true` to skip DataScienceCluster deletion.            | false         |
| --test-operator-controller | To configure the execution of tests related to the Operator POD, this is useful to run e2e tests for an operator running out of the cluster i.e. for debugging purposes       | true          |
| --test-webhook             | To configure the execution of tests rellated to the Operator WebHooks, this is useful to run e2e tests for an operator running out of the cluster i.e. for debugging purposes | true          |
| --test-component           | A repeatable flag that control what component should be tested, by default all component specific test are executed                                                           | true          |

Example command to run full test suite skipping the test for DataScienceCluster deletion.

```shell
make e2e-test OPERATOR_NAMESPACE=<namespace> E2E_TEST_FLAGS="--skip-deletion=true"
```

Example commands to run test suite for the dashboard `component` only, with the operator running out of the cluster.

```shell
make run-nowebhook
```

## Run Prometheus Unit Tests for Alerts

Unit tests for Prometheus alerts are included in the repository. You can run them using the following command:

```shell
make test-alerts
```

To check for alerts that don't have unit tests, run the below command:

```shell
make check-prometheus-alert-unit-tests
```

To add a new unit test file, name it the same as the rules file in the [prometheus ConfigMap](./config/monitoring/prometheus/apps/prometheus-configs.yaml), just with the `.rules` suffix replaced with `.unit-tests.yaml`

### API Overview

Please refer to [api documentation](docs/api-overview.md)

### Component Integration

Please refer to [components docs](components/README.md)

### Troubleshooting

Please refer to [troubleshooting documentation](docs/troubleshooting.md)

### Upgrade testing

Please refer to [upgrade testing documentation](docs/upgrade-testing.md)

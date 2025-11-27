[![codecov](https://codecov.io/github/opendatahub-io/opendatahub-operator/graph/badge.svg?token=QN7G7IVSYA)](https://codecov.io/github/opendatahub-io/opendatahub-operator)

This operator is the primary operator for Open Data Hub. It is responsible for enabling Data science applications like
Jupyter Notebooks, Datascience pipelines etc. The operator makes use of `DataScienceCluster` CRD to deploy
and configure these applications.

### Table of contents
- [Usage](#usage)
  - [Prerequisites](#prerequisites)
  - [Installation](#installation)
  - [Configuration](#configuration)
    - [Log mode values](#log-mode-values)
    - [Use custom application namespace](#use-custom-application-namespace)
    - [Use custom workbench namespace](#use-custom-workbench-namespace)
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
  - [Change logging level at runtime](#change-logging-level-at-runtime)
  - [Example DSCInitialization](#example-dscinitialization)
  - [Example DataScienceCluster](#example-datasciencecluster)
  - [Example GatewayConfig](#example-gatewayconfig)
  - [Run functional Tests](#run-functional-tests)
  - [Run e2e Tests](#run-e2e-tests)
    - [Configuring e2e Tests](#configuring-e2e-tests)
    - [E2E Tips/FAQ](#e2e-tipsfaq)
  - [Run Integration tests (Jenkins pipeline)](#run-integration-tests-jenkins-pipeline)
  - [Run Prometheus Unit Tests for Alerts](#run-prometheus-unit-tests-for-alerts)
  - [API Overview](#api-overview)
  - [Component Integration](#component-integration)
  - [Troubleshooting](#troubleshooting)
  - [Upgrade testing](#upgrade-testing)
  - [Release Workflow Guide](#release-workflow-guide)

## Usage

### Prerequisites

Before installing the OpenDataHub Operator, ensure your environment meets the following requirements:

#### Platform Requirements

- **OpenShift**: Version 4.19 or higher

#### External Operators (Prerequisites)

The following external operators are **required** or **recommended** depending on different use case. These must be installed separately before ODH operator:

| Operator | Purpose | Required For | Installation |
|----------|---------|--------------|--------------|
| **OpenShift Cert Manager Operator** | Certificate management | Gateway with TLS, KServe with Istio | Optional - Install from OperatorHub for llm-d, MaaS and Kueue |
| **Red Hat build of Kueue Operator** | Advanced job queueing and resource management | Distributed workloads, job scheduling with Kueue component | Optional - Install from OperatorHub for Kueue integration |
| **Red Hat Connectivity Link Operator** | Manage the lifecycle of the Kuadrant system | Optional - Install from OperatorHub |
| **LeaderWorkerSet (LWS) Operator** | Leader-worker set controller for distributed training | Advanced distributed training workflows | Optional - Install from OperatorHub for Kueue integration |
| **OpenTelemetry Operator** | Distributed tracing and telemetry | Monitoring and observability | Optional - for advanced monitoring |
| **Tempo Operator** | Distributed tracing backend | Tracing infrastructure | Optional - for trace storage |
| **Cluster Observability Operator** | Enhanced cluster monitoring | Production monitoring | Optional - for comprehensive observability |
| **Perses Operator** | Dashboard and visualization for metrics | Monitoring dashboards and visualizations | Optional - Install from OperatorHub for enhanced metric visualization |
| **JobSet Operator** | Providing JobSet controller | Trainer component | Optional - Install from OperatorHub for Trainer component integration |


#### ODH Component Operators (Managed by ODH)

The following components are **automatically integrated** by the ODH operator based on your DataScienceCluster configuration:

| Component | Operator/Repository | Purpose | Management State |
|-----------|-------------------|---------|------------------|
| **KServe** | [opendatahub-io/kserve](https://github.com/opendatahub-io/kserve) | Model serving platform | Optional |
| **Ray** | [opendatahub-io/kuberay](https://github.com/opendatahub-io/kuberay) | Distributed computing framework | Optional |
| **Training Operator** | [opendatahub-io/training-operator](https://github.com/opendatahub-io/training-operator) | ML training job management | Optional |
| **Trainer** | [opendatahub-io/trainer](https://github.com/opendatahub-io/trainer) | ML training job management | Optional |
| **Feast Operator** | [opendatahub-io/feast](https://github.com/opendatahub-io/feast) | Feature store for ML | Optional |
| **Model Registry Operator** | [opendatahub-io/model-registry](https://github.com/opendatahub-io/model-registry-operator) | Model versioning and registry | Optional |
| **TrustyAI** | [opendatahub-io/trustyai-service-operator](https://github.com/opendatahub-io/trustyai-service-operator) | AI explainability and governance | Optional |
| **Dashboard** | [opendatahub-io/odh-dashboard](https://github.com/opendatahub-io/odh-dashboard) | Web UI for ODH management | Optional |
| **Workbenches** | [opendatahub-io/notebooks](https://github.com/opendatahub-io/notebooks) | Jupyter notebook environments | Optional |
| **AI Pipelines** | [opendatahub-io/data-science-pipelines-operator](https://github.com/opendatahub-io/data-science-pipelines-operator) | ML pipeline orchestration | Optional |

**Note**: These component controller do **not** need to be installed separately.

#### Optional Components

- **GPU Support**: For GPU workloads and metrics:
  - NVIDIA GPU Operator (for NVIDIA GPUs)
  - NVIDIA DCGM Exporter (for GPU metrics collection)

- **Monitoring Stack**:
  - Prometheus (for metrics collection)
  - OpenTelemetry Collector (for distributed tracing)
  - Cluster monitoring capabilities
  - See [Namespace-Restricted Metrics Documentation](docs/NAMESPACE_RESTRICTED_METRICS.md) for secure metrics access

#### Namespace Requirements

- One namespace will be designated as the `applicationsNamespace` in DSCI (default: `opendatahub`)
- Only **one** namespace in the cluster can have the label `opendatahub.io/application-namespace: true` if user prefer overwrite default namespace
- The operator itself runs in a separate namespace (default: `opendatahub-operator` in ODH)

#### Resource Requirements

- Sufficient cluster resources to run enabled components
- For production deployments, ensure adequate CPU, memory, and storage based on your workload requirements
- GPU nodes (if using GPU-accelerated workloads)

### Installation

- The latest version of operator can be installed from the `community-operators` catalog on `OperatorHub`.

  ![ODH operator in OperatorHub](docs/images/OperatorHub%20ODH%20Operator.png)

  Please note that the latest releases are made in the `Fast` channel.

- It can also be built and installed from source manually, see the Developer guide for further instructions.

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

ODH operator can be configured both through flags and environment variables, here a list of the available one:

| Env variable                                         | Corresponding flag          | Description                                                                                                                                                                | Default value |
|------------------------------------------------------|-----------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------|---------------|
| ODH_MANAGER_METRICS_BIND_ADDRESS                     | --metrics-bind-address      | The address the metric endpoint binds to.                                                                                                                                  | :8080         |
| ODH_MANAGER_HEALTH_PROBE_BIND_ADDRESS                | --health-probe-bind-address | The address the probe endpoint binds to.                                                                                                                                   | :8081         |
| ODH_MANAGER_LEADER_ELECT                             | --leader-elect              | Enable leader election for controller manager.                                                                                                                             | false         |
| ODH_MANAGER_LOG_MODE                                 | --log-mode                  | Log mode ('', prod, devel), default to ''. See [Log mode values](#log-mode-values) for details.                                                                            |               |
| ODH_MANAGER_PPROF_BIND_ADDRESS or PPROF_BIND_ADDRESS | --pprof-bind-address        | The address that pprof binds to.                                                                                                                                           |               |
| ZAP_DEVEL                                            | --zap-devel                 | Development Mode defaults(encoder=consoleEncoder,logLevel=Debug,stackTraceLevel=Warn)<br>Production Mode defaults(encoder=jsonEncoder,logLevel=Info,stackTraceLevel=Error) | false         |
| ZAP_ENCODER                                          | --zap-encoder               | Zap log encoding (one of 'json' or 'console')                                                                                                                              |               |
| ZAP_LOG_LEVEL                                        | --zap-log-level             | Zap Level to configure the verbosity of logging. Can be one of 'debug', 'info', 'error'                                                                                    | info          |
| ZAP_STACKTRACE_LEVEL                                 | --zap-stacktrace-level      | Zap Level at and above which stacktraces are captured (one of 'info', 'error', 'panic').                                                                                   |               |
| ZAP_TIME_ENCODING                                    | --zap-time-encoding         | Zap time encoding (one of 'epoch', 'millis', 'nano', 'iso8601', 'rfc3339' or 'rfc3339nano').                                                                               |               |

If both env variables and flags are set for the same configuration, flags values will be used.

#### Log mode values

| log-mode    | zap-stacktrace-level | zap-log-level | zap-encoder | Comments                                      |
|-------------|----------------------|---------------|-------------|-----------------------------------------------|
| devel       | WARN                 | INFO          | Console     | lowest level, using epoch time                |
| development | WARN                 | INFO          | Console     | same as devel                                 |
| ""          | ERROR                | INFO          | JSON        | default option                                |
| prod        | ERROR                | INFO          | JSON        | highest level, using human readable timestamp |
| production  | ERROR                | INFO          | JSON        | same as prod                                  |

#### Use custom application namespace
In ODH 2.23.1, we introduced a new feature which allows user to use their own application namespace than default one "opendatahub".
To enable it:

- For new cluster (i.e. a cluster that has not been used for ODH or RHOAI), using "namespace A" as an example of the targeted application namespace. Follow below steps before install ODH operator:
  1. create namespace A
  2. add label `opendatahub.io/application-namespace: true`  onto namespace A. Only one namespace in the cluster can have this label.
  3. install ODH operator either from UI or by GitOps/CLI
  4. once Operator is up and running, manually create DSCI CR by set `.spec.applicationsNamespace:A`
  5. wait till DSCI status update to "Ready"
  6. continue to create DSC CR
- For cases in which ODH is already running in the cluster:
  - WARNING: Be aware that switching to a different application namespace can cause issues that require manual intervention to be fixed, therefore we suggest this to be done for new clusters only.

#### Use custom workbench namespace

The workbench namespace is configurable. By default, workbenches are deployed to the `opendatahub` application namespace (or `rhods-notebooks` for downstream). You can configure a custom workbench namespace when enabling the workbenches component.

**Important notes:**
- The `workbenchNamespace` field is **immutable** - it can only be set once when workbenches are first enabled
- The namespace must follow Kubernetes naming conventions (lowercase alphanumeric characters or '-', max 63 characters)

**To configure a custom workbench namespace:**

In your DataScienceCluster CR, specify the `workbenchNamespace` field:

```yaml
apiVersion: datasciencecluster.opendatahub.io/v2
kind: DataScienceCluster
metadata:
  name: default-dsc
spec:
  components:
    workbenches:
      managementState: Managed
      workbenchNamespace: my-custom-workbench-namespace
```

## Developer Guide

#### Pre-requisites

- Go version **go1.24**
- operator-sdk version can be updated to **v1.37.0**

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

- To build multi-arch image, set environment variable PLATFORM
  ```commandline
  export PLATFORM=linux/amd64,linux/arm64,linux/ppc64le,linux/s390x
  make image
  ```

- Once the image is created, the operator can be deployed either directly, or through OLM. For each deployment method a
  kubeconfig should be exported

  ```commandline
  export KUBECONFIG=<path to kubeconfig>
  ```

- The operator can be built in ODH or RHOAI mode (build defaults to ODH mode); to build in RHOAI mode, use
  `ODH_PLATFORM_TYPE=rhoai`

  ```commandline
  make image ODH_PLATFORM_TYPE=rhoai
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

- RHOAI bundle can be built using `ODH_PLATFORM_TYPE=rhoai` with any of the bundle make targets.

- Run the Bundle on a cluster:

  ```commandline
  operator-sdk run bundle quay.io/<username>/opendatahub-operator-bundle:<VERSION> --namespace $OPERATOR_NAMESPACE --decompression-image quay.io/project-codeflare/busybox:1.36
  ```

- Understanding Catalog Generation:

  The operator uses File-Based Catalog (FBC) format for OLM integration. The `make catalog-build` command internally runs `catalog-prepare` which:
  - Uses the basic template from `config/catalog/fbc-basic-template.yaml`
  - Processes the template using `hack/update-catalog-template.sh` to generate `catalog/operator.yaml`
  - Validates the generated catalog structure

  **Important Note**: Users must provide the old version bundle images as a comma-separated list, in ascending order, to generate an upgradeable catalog image. For example:
  ```commandline
  make catalog-build catalog-push -e CATALOG_IMG=quay.io/<username>/opendatahub-operator-index:<target_version> \
    BUNDLE_IMGS=quay.io/<username>/opendatahub-operator-bundle:v2.26.0,\
    quay.io/<username>/opendatahub-operator-bundle:v2.27.0,\
    quay.io/<username>/opendatahub-operator-bundle:v2.28.0
  ```

  This creates the following upgrade path:
  ```
  v2.26.0 -> v2.27.0 -> v2.28.0 -> target_version
  ```

  For more details on the File-Based Catalog format, see the [FBC documentation](https://olm.operatorframework.io/docs/reference/file-based-catalogs/).

- Build Catalog Image:

  ```commandline
  make catalog-build catalog-push -e CATALOG_IMG=quay.io/<username>/opendatahub-operator-index:<target_version> BUNDLE_IMGS=<list-of-comma-separated-bundle-images>
  ```

### Test with customized manifests

There are 2 ways to test your changes with modification:

1. Using custom manifests in OLM operator. See [custom manifest](hack/component-dev/README.md) for more details.

2. Build operator image with local manifests, running `make image-build USE_LOCAL=true`

### Update API docs

Whenever a new api is added or a new field is added to the CRD, please make sure to run the command:
  ```commandline
  make api-docs
  ```
This will ensure that the doc for the apis are updated accordingly.

### Change logging level at runtime

Log level can be changed at runtime by DSCI devFlags by setting
`.spec.devFlags.logLevel`. It accepts the same values as `--zap-log-level` flag or `ZAP_LOG_LEVEL` env variable. See example :

```console
apiVersion: dscinitialization.opendatahub.io/v2
kind: DSCInitialization
metadata:
  name: default-dsci
spec:
  devFlags:
    logLevel: debug
  ...
```

### Example DSCInitialization

1. Default DSCI configuration

```console
kind: DSCInitialization
apiVersion: dscinitialization.opendatahub.io/v2
metadata:
  name: default-dsci
spec:
  applicationsNamespace: opendatahub
  monitoring:
    managementState: Managed
    namespace: opendatahub
    metrics:
      replicas: 2
      resources:
        cpulimit: 500m
        cpurequest: 100m
        memorylimit: 512Mi
        memoryrequest: 256Mi
      storage:
        retention: 90d
        size: 5Gi
    traces:
      storage:
        backend: pv
        size: 5Gi
        retention: 2160h
  trustedCABundle:
    customCABundle: ''
    managementState: Managed

```

2. DSCI with custom application and monitoring namespace

```console
kind: DSCInitialization
apiVersion: dscinitialization.opendatahub.io/v2
metadata:
  name: default-dsci
spec:
  applicationsNamespace: my-custom-namespace
  monitoring:
    managementState: Managed
    namespace: my-custom-namespace
  trustedCABundle:
    managementState: Managed

```

**Note:** Before applying DSCI with a custom application namespace, ensure you have created the namespace and labeled it with `opendatahub.io/application-namespace: true`. See [Use custom application namespace](#use-custom-application-namespace) for complete setup instructions.

Apply these examples with modifications for your usage.

### Example DataScienceCluster

When the operator is installed successfully in the cluster, a user can create a `DataScienceCluster` CR to enable ODH
components. At a given time, ODH supports only **one** instance of the CR, which can be updated to get custom list of components.

1. Enable all components

```console
apiVersion: datasciencecluster.opendatahub.io/v2
kind: DataScienceCluster
metadata:
  name: default-dsc
spec:
  components:
    dashboard:
      managementState: Managed
    aipipelines:
      managementState: Managed
    kserve:
      managementState: Managed
      nim:
        managementState: Managed
      rawDeploymentServiceConfig: Headed
    kueue:
      managementState: Unmanaged
    modelregistry:
      managementState: Managed
      registriesNamespace: "odh-model-registries"
    ray:
      managementState: Managed
    trainingoperator:
      managementState: Managed
    trustyai:
      managementState: Managed
      eval:
        lmeval:
          permitCodeExecution: deny
          permitOnline: deny
    workbenches:
      managementState: Managed
    feastoperator:
      managementState: Managed
    llamastackoperator:
      managementState: Removed
```

2. Enable only Dashboard and Workbenches

```console
apiVersion: datasciencecluster.opendatahub.io/v2
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

3. Enable Workbenches with custom namespace

```console
apiVersion: datasciencecluster.opendatahub.io/v2
kind: DataScienceCluster
metadata:
  name: example
spec:
  components:
    workbenches:
      managementState: Managed
      workbenchNamespace: my-workbench-namespace
```

**Note:** The `workbenchNamespace` field once set, it cannot be changed (immutable).

### Example GatewayConfig

The GatewayConfig custom resource is used to configure gateway settings for OpenDataHub, including OIDC authentication and ingress gateway configuration. This CR is cluster-scoped and must be named `default-gateway`.

**Automatic Creation**: The GatewayConfig CR is automatically created when a DSCInitialization CR is applied to the cluster. Users typically don't need to create this CR manually unless they want to configure OIDC authentication mode or customize ingress gateway settings.

Here's an example of the default GatewayConfig CR (automatically created):

```yaml
apiVersion: services.platform.opendatahub.io/v1alpha1
kind: GatewayConfig
metadata:
  name: default-gateway
spec:
  cookie: {}  # Uses defaults: expire: 24h, refresh: 1h
  certificate:
    type: OpenshiftDefaultIngress
```

For a setup with custom certificate (Provided type):

```yaml
apiVersion: services.platform.opendatahub.io/v1alpha1
kind: GatewayConfig
metadata:
  name: default-gateway
spec:
  cookie: {}  # Uses defaults: expire: 24h, refresh: 1h
  domain: "*.apps.example.com"
  certificate:
    type: Provided
    secretName: custom-tls-cert  # Secret must already exist in openshift-ingress namespace with tls.crt and tls.key
```

For an advanced example with OIDC authentication:

```yaml
apiVersion: services.platform.opendatahub.io/v1alpha1
kind: GatewayConfig
metadata:
  name: default-gateway
spec:
  oidc:
    issuerURL: "https://keycloak.example.com/auth/realms/opendatahub"
    clientID: "opendatahub-client"
    clientSecretRef:
      name: oidc-client-secret
      key: client-secret
  cookie:
    expire: 24h  # Default: 24h
    refresh: 1h  # Default: 1h
  domain: "*.apps.example.com"
  authTimeout: 10s  # Default: 5s
  certificate:
    type: SelfSigned
```

For an advanced example using cluster domain with custom subdomain:

```yaml
apiVersion: services.platform.opendatahub.io/v1alpha1
kind: GatewayConfig
metadata:
  name: default-gateway
spec:
  cookie: {}  # Uses defaults
  domain: apps.example.com
  certificate:
    type: SelfSigned
```

This will create a gateway `data-science-gateway.apps.example.com` (using default subdomain)


For an advanced example with custom subdomain:

```yaml
apiVersion: services.platform.opendatahub.io/v1alpha1
kind: GatewayConfig
metadata:
  name: default-gateway
spec:
  cookie: {}  # Uses defaults
  domain: apps.cluster.example.com
  subdomain: custom-gateway
  certificate:
    type: SelfSigned
```

This will use the cluster's default domain with your custom subdomain: `custom-gateway.apps.cluster.example.com`

**Important Notes:**
- The GatewayConfig name must be exactly `default-gateway`
- This is a cluster-scoped resource
- **Automatic creation**: This CR is automatically created after DSCInitialization CR is applied
- **Manual configuration needed**: Only configure this CR manually if you want to enable OIDC authentication mode or customize ingress gateway settings
- OIDC configuration is optional and only needed when cluster is in OIDC authentication mode
- Certificate types can be `OpenshiftDefaultIngress`, `SelfSigned`, or `Provided`
- If `subdomain` is not specified or is empty, the default value `data-science-gateway` is used.
- If `domain` is not specified, the cluster's default domain is used.

### Run functional Tests

The functional tests are writted based on [ginkgo](https://onsi.github.io/ginkgo/) and [gomega](https://onsi.github.io/gomega/). In order to run the tests, the user needs to setup the envtest which provides a mocked kubernetes cluster. A detailed explanation on how to configure envtest is provided [here](https://book.kubebuilder.io/reference/envtest.html#configuring-envtest-for-integration-tests).

To run the test on individual controllers, change directory into the contorller's folder and run
```shell
ginkgo -v
```

This provides detailed logs of the test spec.

**Note:** When running tests for each controller, make sure to add the `BinaryAssetsDirectory` attribute in the `envtest.Environment` in the `suite_test.go` file. The value should point to the path where the envtest binaries are installed.

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

#### Configuring e2e Tests
Evn vars can be set to configure e2e tests:

| Configuration Env var           | Description                                                                                                                                                                  | Default value                 |
|---------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|-------------------------------|
| E2E_TEST_OPERATOR_NAMESPACE     | Namespace where the ODH operator is deployed.                                                                                                                                | `opendatahub-operator-system` |
| E2E_TEST_APPLICATIONS_NAMESPACE | Namespace where the ODH applications are deployed.                                                                                                                           | `opendatahub`                 |
| E2E_TEST_WORKBENCHES_NAMESPACE | Namespace where the workbenches are deployed. | `opendatahub` |
| E2E_TEST_DSC_MONITORING_NAMESPACE | Namespace where the ODH monitoring is deployed. | `opendatahub` |
| E2E_TEST_OPERATOR_CONTROLLER    | To configure the execution of tests related to the Operator POD, this is useful to run e2e tests for an operator running out of the cluster i.e. for debugging purposes      | `true`                        |
| E2E_TEST_OPERATOR_RESILIENCE    | To configure the execution of operator resilience tests, useful for testing operator fault tolerance scenarios                                 | `true`                        |
| E2E_TEST_WEBHOOK                | To configure the execution of tests related to the Operator WebHooks, this is useful to run e2e tests for an operator running out of the cluster i.e. for debugging purposes | `true`                        |
| E2E_TEST_DELETION_POLICY        | Specify when to delete `DataScienceCluster`, `DSCInitialization`, and controllers. Valid options are: `always`, `on-failure`, and `never`.                                   | `always`                      |
| E2E_TEST_COMPONENTS             | Enable testing of individual components specified by --test-component flag                                                                                                   | `true`                        |
| E2E_TEST_COMPONENT              | A space separated configuration to control which component should be tested, by default all component specific test are executed                                             | `all components`              |
| E2E_TEST_SERVICES               | Enable testing of individual services specified by --test-service flag                                                                                                       | `true`                        |
| E2E_TEST_SERVICE                | A space separated configuration to control which services should be tested, by default all service specific test are executed                                                | `all services`                |
| E2E_TEST_OPERATOR_V2TOV3UPGRADE | To configure the execution of V2 to V3 upgrade tests, useful for testing V2 to V3 upgrade scenarios                                                                       | `true`                        |
| E2E_TEST_HARDWARE_PROFILE       | To configure the execution of hardware profile tests, useful for testing hardware profile functionality for v1 and v1alpha1                                              | `true`                        |
|                                 |                                                                                                                                                                              |                               |
| E2E_TEST_FLAGS                  | Alternatively the above configurations can be passed to e2e-tests as flags using this env var (see flags table below)                                                        |                               |

Alternatively the above configurations can be passed to e2e-tests as flags by setting up `E2E_TEST_FLAGS` variable. Following table lists all the available flags:

| Flag                          | Description                                                                                                                                                                  | Default value                 |
|----------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|-------------------------------|
| --operator-namespace          | Namespace where the ODH operator is deployed.                                                                                                                                | `opendatahub-operator-system` |
| --applications-namespace      | Namespace where the ODH applications are deployed.                                                                                                                           | `opendatahub`                 |
| --workbenches-namespace | Namespace where the workbenches are deployed. | `opendatahub` |
| --dsc-monitoring-namespace | Namespace where the ODH monitoring is deployed. | `opendatahub` |
| --test-operator-controller    | To configure the execution of tests related to the Operator POD, this is useful to run e2e tests for an operator running out of the cluster i.e. for debugging purposes      | `true`                        |
| --test-operator-resilience    | To configure the execution of operator resilience tests, useful for testing operator fault tolerance scenarios                                 | `true`                        |
| --test-webhook                | To configure the execution of tests related to the Operator WebHooks, this is useful to run e2e tests for an operator running out of the cluster i.e. for debugging purposes | `true`                        |
| --deletion-policy             | Specify when to delete `DataScienceCluster`, `DSCInitialization`, and controllers. Valid options are: `always`, `on-failure`, and `never`.                                   | `always`                      |
| --test-components             | Enable testing of individual components specified by --test-component flag                                                                                                   | `true`                        |
| --test-component              | A repeatable (or comma separated no spaces) flag that control which component should be tested, by default all component specific test are executed                          | `all components`              |
| --test-services               | Enable testing of individual services specified by --test-service flag                                                                                                       | `true`                        |
| --test-service                | A repeatable (or comma separated no spaces) flag that control which services should be tested, by default all service specific test are executed                             | `all services`                |
| --test-operator-v2tov3upgrade | To configure the execution of V2 to V3 upgrade tests, useful for testing V2 to V3 upgrade scenarios                                                                       | `true`                        |
| --test-hardware-profile       | To configure the execution of hardware profile tests, useful for testing hardware profile functionality between v1 and v1alpah1                                               | `true`                        |

<details>
<summary>Running E2E tests with custom application namespace</summary>

If you intend to use non-default application namespace while running E2E tests, additional setup is required:
1. create the custom application namespace
```shell
oc create namespace <your-custom-app-namespace>
```
2. ensure your custom namespace has the required label
```shell
oc label namespace <your-custom-app-namespace> opendatahub.io/application-namespace=true
```
3. deploy the operator
```shell
make deploy IMG=<your-operator-image>
```
4. run e2e test suite
```shell
make e2e-test -e E2E_TEST_APPLICATIONS_NAMESPACE=<your-custom-app-namespace> -e ...
```
5. once done with the tests, ensure to clean up the custom namespace
</details>

Example command to run full test suite skipping the DataScienceCluster deletion (useful to troubleshooting tests failures):

```shell
make e2e-test -e E2E_TEST_OPERATOR_NAMESPACE=<namespace> -e E2E_TEST_DELETION_POLICY=never
```

Example commands to run test suite for the dashboard `component` only, with the operator running out of the cluster:

```shell
make run-nowebhook
```
```shell
make e2e-test -e E2E_TEST_OPERATOR_NAMESPACE=<namespace> -e E2E_TEST_OPERATOR_CONTROLLER=false -e E2E_TEST_WEBHOOK=false -e E2E_TEST_SERVICES=false -e E2E_TEST_COMPONENT=dashboard
```

Example commands to run test suite for the monitoring `service` only, with the operator running out of the cluster:

```shell
make run-nowebhook
```
```shell
make e2e-test -e E2E_TEST_OPERATOR_NAMESPACE=<namespace> -e E2E_TEST_OPERATOR_CONTROLLER=false -e E2E_TEST_WEBHOOK=false -e E2E_TEST_COMPONENTS=false -e E2E_TEST_SERVICE=monitoring
```

Example commands to run test suite excluding tests for ray `component`:
```shell
make e2e-test -e E2E_TEST_OPERATOR_NAMESPACE=<namespace> -e E2E_TEST_COMPONENT=!ray
```

Additionally specific env vars can be used to configure tests timeouts

| Timeouts Env var                         | Description                                                                             | Default value |
|------------------------------------------|-----------------------------------------------------------------------------------------|---------------|
| E2E_TEST_DEFAULTEVENTUALLYTIMEOUT        | Timeout used for Eventually; overrides Gomega's default of 1 second.                    | `5m`          |
| E2E_TEST_MEDIUMEVENTUALLYTIMEOUT         | Medium timeout: for readiness checks (e.g., ClusterServiceVersion, DataScienceCluster). | `7m`          |
| E2E_TEST_LONGEVENTUALLYTIMEOUT           | Long timeout: for more complex readiness (e.g., DSCInitialization, KServe).             | `10m`         |
| E2E_TEST_DEFAULTEVENTUALLYPOLLINTERVAL   | Polling interval for Eventually; overrides Gomega's default of 10 milliseconds.         | `2s`          |
| E2E_TEST_DEFAULTCONSISTENTLYTIMEOUT      | Duration used for Consistently; overrides Gomega's default of 2 seconds.                | `10s`         |
| E2E_TEST_DEFAULTCONSISTENTLYPOLLINTERVAL | Polling interval for Consistently; overrides Gomega's default of 50 milliseconds.       | `2s`          |

#### E2E Tips/FAQ

<details>
<summary>Minimum Setup for e2e</summary>

Set `IMAGE_TAG_BASE` (in your environment or in `local.mk`) \- replace `$ORG` with your [quay.io](http://quay.io) org:

```shell
export IMAGE_TAG_BASE=quay.io/$ORG/opendatahub-operator
```

</details>

<details>
<summary>Recommended Setup for e2e</summary>

Turn off post-test cleanup

```shell
export E2E_TEST_DELETION_POLICY=never
```
</details>

<details>
<summary>Typical Workflow</summary>

First, clone [olminstall](https://gitlab.cee.redhat.com/data-hub/olminstall) (Red Hat internal only), because it has a cleanup command you can use to ensure a clean slate:

```shell
~/olminstall/cleanup.sh -t operator
make install
make deploy
make e2e-test  # include other args as needed
```

</details>

<details>
<summary>How do I run only tests for core services (monitoring, etc)?</summary>

```shell
make e2e-test -e E2E_TEST_OPERATOR_CONTROLLER=false -e E2E_TEST_WEBHOOK=false -e E2E_TEST_COMPONENTS=false -e E2E_TEST_SERVICES=true -e E2E_TEST_DELETION_POLICY=never
```

</details>

<details>
<summary>How do I run only tests for a specific component?</summary>

```shell
make e2e-test -e E2E_TEST_OPERATOR_CONTROLLER=false -e E2E_TEST_WEBHOOK=false -e E2E_TEST_COMPONENT="dashboard workbenches" -e E2E_TEST_SERVICES=false -e E2E_TEST_DELETION_POLICY=never
```

##### Alternative using E2E\_TEST\_FLAGS

```shell
make e2e-test -e E2E_TEST_FLAGS="--test-operator-controller=false --test-webhook=false --test-component=dashboard,workbenches --test-services=false --deletion-policy=never"
```

</details>

<details>
<summary>How do I run a specific test?</summary>

```shell
go test -v -run "TestOdhOperator/Operator_Resilience_E2E_Tests/Validate_components_deployment_failure" ./tests/e2e --timeout=15m
```

</details>

### Run Integration tests (Jenkins pipeline)

For instructions on how to run the integration test Jenkins pipeline, please refer to [the following document](docs/integration-testing.md)

### Run Prometheus Unit Tests for Alerts

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

Please refer to [components docs](docs/COMPONENT_INTEGRATION.md)

### Troubleshooting

Please refer to [troubleshooting documentation](docs/troubleshooting.md)

### Upgrade testing

Please refer to [upgrade testing documentation](docs/upgrade-testing.md)

### Release Workflow Guide

Please refer to [release workflow documentation](docs/release-workflow-guide.md)

# opendatahub-operator
// TODO(user): Add simple overview of use/purpose

## Description
// TODO(user): An in-depth paragraph about your project and overview of use

## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

### Running on the cluster


### Installation

The latest version of operator can be installed from the community-operators catalog on OperatorHub. It can also be build
and installed from source manually, see the Developer guide for further instructions.


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

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller
UnDeploy the controller to the cluster:

```sh
make undeploy
```

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/) 
which provides a reconcile function responsible for synchronizing resources untile the desired state is reached on the cluster 

### Test It Out
1. Install the CRDs into the cluster:

```sh
make install
```

2. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.


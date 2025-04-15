# What

This document serves as the knowledge base for troubleshooting the Open Data Hub Operator.
More information can be found at https://github.com/opendatahub-io/opendatahub-operator/wiki

## Troubleshooting

### Upgrade from Operator v2.0/v2.1 to v2.2+

This also applies to any local build deployment from the "main" branch.

To upgrade, follow these steps:

- Disable the component(s) in your DSC instance.
- Delete both the DSC instance and DSCI instance.
- Click "uninstall" Open Data Hub operator.
- If exposed on v1alpha1, delete the DSC CRD and DSCI CRD.

All of the above steps can be performed either through the console UI or via the `oc`/`kubectl` CLI.
After completing these steps, please refer to the installation guide to proceed with a clean installation of the v2.2+ operator.


### Why component's managementState is set to {} not Removed?

Only if managementState is explicitliy set to "Managed" on component level, below configs in DSC CR to component "X" take the same effects:

```console
spec:
components:
    X:
        managementState: Removed

```

```console
spec:
components:
    X: {}
```

### Setting up a Fedora-based development environment

This is a loose list of tools to install on your linux box in order to compile, test and deploy the operator.

```bash
ssh-keygen -t ed25519 -C "<email-registered-on-github-account>"
# upload public key to github

sudo dnf makecache --refresh
sudo dnf install -y git-all
sudo dnf install -y golang
sudo dnf install -y podman
sudo dnf install -y cri-o kubernetes-kubeadm kubernetes-node kubernetes-client cri-tools
sudo dnf install -y operator-sdk
sudo dnf install -y wget
wget https://mirror.openshift.com/pub/openshift-v4/clients/oc/latest/linux/oc.tar.gz
cd bin/; tar -xzvf ../oc.tar.gz ; cd .. ; rm oc.tar.gz
sudo dnf install -y zsh

# update PATH
echo 'export PATH=${PATH}:~/bin' >> ~/.zshrc
echo 'export GOPROXY=https://proxy.golang.org' >> ~/.zshrc
```

### Using a local.mk file to override Makefile variables for your development environment

To support the ability for a developer to customize the Makefile execution to support their development environment, you can create a `local.mk` file in the root of this repo to specify custom values that match your environment.

```
$ cat local.mk
VERSION=9.9.9
IMAGE_TAG_BASE=quay.io/my-dev-env/opendatahub-operator
IMG_TAG=my-dev-tag
OPERATOR_NAMESPACE=my-dev-odh-operator-system
IMAGE_BUILD_FLAGS=--build-arg USE_LOCAL=true
E2E_TEST_FLAGS="--skip-deletion=true" -timeout 15m
DEFAULT_MANIFESTS_PATH=./opt/manifests
PLATFORM=linux/amd64,linux/ppc64le,linux/s390x
```

### When I try to use my own application namespace, I get different errors:

1. Operator pod is keeping crash
Ensure in your cluster, only one application has label `opendatahub.io/application-namespace=true`.  This is similar to case (3).

2. error "DSCI must used the same namespace which has opendatahub.io/application-namespace=true label"
In the cluster, one namespace has label `opendatahub.io/application-namespace=true`, but it is not being set in the DSCI's `.spec.applicationsNamespace`, solutions (any of below ones should work):
- delete existin DSCI, and re-create it with namespace which already has label `opendatahub.io/application-namespace=true`
- remove label `opendatahub.io/application-namespace=true` from the other namespace to the one specified in the DSCI, and wait for a couple of minutes to allow DSCI continue.

3. error "only support max. one namespace with label: opendatahub.io/application-namespace=true"
Refer to (1).

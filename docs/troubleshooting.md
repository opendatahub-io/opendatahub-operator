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
E2E_TEST_FLAGS="--deletion-policy=never" -timeout 15m
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

### Profiling with pprof

If running with the `make run`, or `make run-nowebhook` commands, pprof is enabled.

When pprof is enabled, you can explore collected pprof profiles using commands such as:

- `go tool pprof -http : http://localhost:6060/debug/pprof/heap`
- `go tool pprof -http : http://localhost:6060/debug/pprof/profile`
- `go tool pprof -http : http://localhost:6060/debug/pprof/block`

You can also save a pprof file for use in other tools or offline analysis as follows:

```shell
curl -s "http://127.0.0.1:6060/debug/pprof/profile" > ./cpu-profile.out
```

This is disabled by default outside local development, but can be enabled by setting the `PPROF_BIND_ADDRESS` env var:

```diff
  - name: PPROF_BIND_ADDRESS
    value: 0.0.0.0:6060
```

This can be set in an existing opendatahub-operator-controller-manager deployment, or on the operator subscription per
https://github.com/operator-framework/operator-lifecycle-manager/blob/master/doc/design/subscription-config.md#env

See https://github.com/google/pprof/blob/main/doc/README.md for more details on how to use pprof

### Operator Pod Restarting Frequently

**Alert**: `OperatorPodRestartingFrequently`  
**Severity**: Warning  
**Description**: The operator pod has restarted more than 3 times in a 5-minute period.

#### Symptoms

- Prometheus alert `OperatorPodRestartingFrequently` is firing
- Operator pod restart count is high
- Components may not be reconciling properly
- Operator logs show crash or restart messages

#### Investigation Steps

#### 1. Check operator pod status:
```bash
oc get pods -n redhat-ods-operator
oc describe pod <operator-pod-name> -n redhat-ods-operator
```

#### 2. Check restart count:
```bash
kubectl get pod <operator-pod-name> -n redhat-ods-operator -o jsonpath='{.status.containerStatuses[0].restartCount}'
```

#### 3. Get operator logs (current and previous):
```bash
# Current logs
oc logs -n redhat-ods-operator <operator-pod-name> -c rhods-operator --tail=100

# Previous crashed container logs
oc logs -n redhat-ods-operator <operator-pod-name> -c rhods-operator --previous
```

#### 4. Check for common issues:
```bash
# Check resource limits
oc get pod <operator-pod-name> -n redhat-ods-operator -o jsonpath='{.spec.containers[0].resources}'

# Check events
oc get events -n redhat-ods-operator --sort-by='.lastTimestamp' | grep <operator-pod-name>

# Check for OOM kills
oc get pod <operator-pod-name> -n redhat-ods-operator -o jsonpath='{.status.containerStatuses[0].lastState}'
```

#### Common Causes & Solutions

#### 1. Out of Memory (OOM)
- **Symptom**: `lastState.terminated.reason: OOMKilled`
- **Solution**: Increase memory limits
  ```bash
  oc patch deployment rhods-operator-controller-manager -n redhat-ods-operator \
    --type='json' -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/resources/limits/memory", "value": "2Gi"}]'
  ```

#### 2. CPU Throttling
- **Symptom**: High CPU usage, slow reconciliation
- **Solution**: Increase CPU limits
  ```bash
  oc patch deployment rhods-operator-controller-manager -n redhat-ods-operator \
    --type='json' -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/resources/limits/cpu", "value": "1000m"}]'
  ```

#### 3. Webhook Certificate Issues
- **Symptom**: Logs show certificate errors
- **Solution**: Check certificate secrets
  ```bash
  oc get secret -n redhat-ods-operator | grep webhook
  oc get validatingwebhookconfiguration
  oc get mutatingwebhookconfiguration
  ```

#### 4. Panic or Fatal Errors
- **Symptom**: Logs show panic stack traces or fatal errors
- **Solution**: Review logs for root cause, may need code fix
  ```bash
  oc logs -n redhat-ods-operator <operator-pod-name> -c rhods-operator --previous | grep -A 20 "panic\|fatal"
  ```

#### Resolution Steps

1. **Immediate Action**: If operator is non-functional, restart it:
   ```bash
   oc rollout restart deployment rhods-operator -n redhat-ods-operator
   ```

2. **Increase Resources** (if OOM/CPU throttling):
   ```bash
   oc patch deployment rhods-operator-controller-manager -n redhat-ods-operator \
     --type='json' -p='[
       {"op": "replace", "path": "/spec/template/spec/containers/0/resources/limits/memory", "value": "2Gi"},
       {"op": "replace", "path": "/spec/template/spec/containers/0/resources/requests/memory", "value": "512Mi"},
       {"op": "replace", "path": "/spec/template/spec/containers/0/resources/limits/cpu", "value": "1000m"}
     ]'
   ```

3. **Check for Cluster-Wide Issues**:
   ```bash
   # Check node resources
   oc top nodes
   
   # Check if other operators are also restarting
   oc get pods --all-namespaces | grep -E 'CrashLoop|Error'
   ```

4. **Verify Operator Configuration**:
   ```bash
   # Check DSCI
   oc get dsci -o yaml
   
   # Check DSC
   oc get dsc -o yaml
   
   # Validate no circular dependencies or misconfigurations
   ```

5. **Collect Debug Information**:
   ```bash
   # Get full operator state
   oc get deployment rhods-operator-controller-manager -n redhat-ods-operator -o yaml > operator-deployment.yaml
   oc get pods -n redhat-ods-operator -o yaml > operator-pods.yaml
   oc logs -n redhat-ods-operator <operator-pod-name> --all-containers --previous > operator-previous-logs.txt
   oc logs -n redhat-ods-operator <operator-pod-name> --all-containers > operator-current-logs.txt
   ```

#### When to Escalate

Escalate to the development team if:
- Restarts continue after resource increases
- Logs show unhandled panics or fatal errors
- Issue correlates with specific DSC/DSCI configuration
- Problem started after operator upgrade
- Multiple restarts with no clear cause in logs

**Bug Report**: Include all debug information collected above and steps taken.

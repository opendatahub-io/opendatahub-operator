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

### Scrape Target Down Alerts

> **Note on notifications**: As of this writing, the Data Science `Alertmanager` has no configured receivers other than the default `null` receiver (`oc exec -n opendatahub <alertmanager-pod-name> -c alertmanager -- curl -sS http://localhost:9093/api/v2/receivers`). This means the alerts below will fire and be visible in the Alertmanager/Prometheus UI, but **no external notification (Slack, email, webhook, etc.) will be sent** until a real receiver is configured. See [RHOAIENG-55993](https://redhat.atlassian.net/browse/RHOAIENG-55993) for tracking that separate gap. Do not assume "no notification received" means the alert didn't fire — check the UI/API directly.

### Prometheus Self Scrape Target Down

**Alert**: `PrometheusSelfScrapeTargetDown`  
**Severity**: Critical  
**Description**: The Data Science Prometheus instance has not been able to scrape its own `prometheus-self-fixed` target for more than 10 minutes.

#### Symptoms

- Prometheus alert `PrometheusSelfScrapeTargetDown` is firing
- Prometheus self-monitoring metrics (`up{job="prometheus-self-fixed"}`) are missing or stale
- Downstream SLO alerts that depend on Prometheus self-health may be silently unreliable

#### Investigation Steps

#### 1. Check Prometheus pod status:
```bash
oc get pods -n opendatahub -l app.kubernetes.io/name=prometheus
oc describe pod <prometheus-pod-name> -n opendatahub
```

#### 2. Check the `prometheus-self-fixed` ServiceMonitor and its target in Prometheus:
```bash
oc get servicemonitors.monitoring.rhobs -n opendatahub prometheus-self-fixed -o yaml
oc exec -n opendatahub <prometheus-pod-name> -c prometheus -- curl -sS --cacert /etc/prometheus/configmaps/prometheus-web-tls-ca/service-ca.crt https://localhost:9090/api/v1/targets | jq '.data.activeTargets[] | select(.labels.job=="prometheus-self-fixed")'
```

#### 3. Check TLS certificate validity (this target uses a fixed `serverName` to work around a SAN mismatch):
```bash
oc get configmap prometheus-web-tls-ca -n opendatahub
oc get secret prometheus-operated-tls -n opendatahub
```

#### Common Causes & Solutions

#### 1. Certificate rotation / SAN mismatch
- **Symptom**: TLS handshake errors in Prometheus logs referencing `prometheus-operated`
- **Solution**: Verify the `prometheus-web-tls-ca` ConfigMap and `prometheus-operated-tls` Secret are present and up to date; restart the Prometheus pod to pick up rotated certs

#### 2. Prometheus pod not running
- **Symptom**: Pod in `CrashLoopBackOff` or `Pending`
- **Solution**: Check events and resource availability (`oc describe pod`, `oc get events -n opendatahub`)

#### Resolution Steps

1. **Restart Prometheus** (StatefulSet-managed, safe to cycle one replica at a time):
   ```bash
   oc delete pod <prometheus-pod-name> -n opendatahub
   ```
2. **Verify recovery**: confirm `up{job="prometheus-self-fixed"} == 1` after the pod is back to `Running`
3. **If persistent**: check the Cluster Observability Operator (`MonitoringStack` CR) status for reconciliation errors

#### When to Escalate

Escalate if the target remains down after a Prometheus restart, or if TLS certificate issues recur repeatedly across restarts.

### Collector Telemetry / Prometheus Exporter Scrape Target Down

**Alerts**: `CollectorTelemetryScrapeTargetDown`, `CollectorPrometheusExporterScrapeTargetDown`  
**Severity**: Warning  
**Description**: The OpenTelemetry Collector's own telemetry endpoint (`data-science-collector-collector-monitoring`) or its Prometheus exporter endpoint for application metrics (`data-science-collector-prometheus`) has been unreachable for more than 10 minutes.

#### Symptoms

- One of the above alerts is firing
- Collector health metrics and/or application metrics collected via the Prometheus exporter are missing

#### Investigation Steps

#### 1. Check collector pod status:
```bash
oc get pods -n opendatahub -l app.kubernetes.io/component=opentelemetry-collector
oc describe pod <collector-pod-name> -n opendatahub
```

#### 2. Check the relevant ServiceMonitor and target health:
```bash
oc get servicemonitors.monitoring.rhobs -n opendatahub data-science-collector-monitor -o yaml
oc get servicemonitors.monitoring.rhobs -n opendatahub data-science-prometheus-monitor -o yaml
```

#### 3. Check collector logs:
```bash
oc logs -n opendatahub <collector-pod-name> --tail=100
```

#### Resolution Steps

1. **Restart the collector pod** if it is crashing or unresponsive:
   ```bash
   oc delete pod <collector-pod-name> -n opendatahub
   ```
2. **Check the OpenTelemetry Operator** (`opentelemetry-operator` CSV) is healthy on the cluster
3. **Verify recovery** by re-checking `up{job="data-science-collector-collector-monitoring"}` / `up{job="data-science-collector-prometheus"}`

#### When to Escalate

Escalate if the collector repeatedly crashes or the OpenTelemetry Operator itself is degraded.

### Alertmanager Self Scrape Target Down

**Alert**: `AlertmanagerSelfScrapeTargetDown`  
**Severity**: Warning  
**Description**: The `alertmanager-self` scrape target has been down for more than 10 minutes, meaning Alertmanager's own health cannot be verified.

#### Symptoms

- Alert `AlertmanagerSelfScrapeTargetDown` is firing
- Risk that real alert delivery failures (e.g. to receivers/notification channels) could go unnoticed while Alertmanager health is unknown

#### Investigation Steps

#### 1. Check Alertmanager pod status:
```bash
oc get pods -n opendatahub -l app.kubernetes.io/name=alertmanager
oc describe pod <alertmanager-pod-name> -n opendatahub
```

#### 2. Check Alertmanager logs:
```bash
oc logs -n opendatahub <alertmanager-pod-name> --tail=100
```

#### Resolution Steps

1. **Restart Alertmanager** (StatefulSet-managed):
   ```bash
   oc delete pod <alertmanager-pod-name> -n opendatahub
   ```
2. **Verify recovery** by re-checking `up{job="alertmanager-self"}`

#### When to Escalate

Escalate if Alertmanager remains down after a restart, since this may indicate a broader issue with the `MonitoringStack` deployment.


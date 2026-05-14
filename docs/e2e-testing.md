# How to run E2E Tests

This document covers running E2E tests locally, both on KinD (KServe component) and on
OpenShift (full suite). For CI-triggered integration tests via Jenkins, see
[integration-testing.md](integration-testing.md).

## When E2E Tests are Required

E2E tests are **MANDATORY** for pull requests that modify:

- **Component Controllers** (`internal/controller/components/` changes)
- **Service Controllers** (`internal/controller/services/` changes)
- **API Definitions** (`api/` directory changes)
- **Webhook Implementations** (`internal/webhook/` changes)
- **Operator Configuration** (`config/` directory changes)

E2E tests are **RECOMMENDED** for:
- Multi-component feature implementations
- Reconciliation logic changes
- Manifest or kustomize overlay updates

## Prerequisites

- **Go** (version matching `go.mod`)
- **Podman** or **Docker**
- **kubectl**
- **Red Hat registry credentials** (for `registry.redhat.io` images) — `podman login registry.redhat.io`

For KinD tests additionally:
- **KinD** (`go install sigs.k8s.io/kind@latest`)

For OpenShift tests additionally:
- **OpenShift cluster** with `oc` CLI configured
- **Dependent operators** installed (OpenShift Service Mesh, Serverless, etc.)

## Understanding the Test Suites

The E2E test suite is organized into test groups that run sequentially. Within each group,
component tests run in parallel. The suite supports two main targets:

- **`make e2e-test-xks`** — KServe-only tests on KinD / vanilla Kubernetes.
  This is the CI-equivalent E2E for KinD and covers component enable, update, delete,
  recovery, and versioning.

- **`make e2e-test`** — Full suite across all components, DSC/DSCI lifecycle,
  services, webhooks, and operator resilience. Requires an OpenShift cluster.

## KinD E2E (KServe Only)

### Step-by-step guide

1. **Create the KinD cluster**: `make kind-create`
   - Creates a 2-node cluster (1 control-plane + 1 worker) named `kind-odh` using
     `config/kind/kind-config.yaml`

2. **Build the operator image**: `IMG=localhost/odh-operator:e2e make image-build`
   - Always specify `IMG=localhost/odh-operator:e2e` to avoid conflicts with the
     default image tag
   - The Makefile defaults to `podman` as the image builder. If using Docker, set
     `IMAGE_BUILDER=docker`

3. **Load the image into KinD**: `make image-kind-load IMG=localhost/odh-operator:e2e KIND_CLUSTER_NAME=kind-odh`

4. **Create operator namespace**: `kubectl create namespace opendatahub-operator-system`

5. **Setup pull secrets** (required for `registry.redhat.io` images):
   `make kind-setup-pull-secrets PULL_SECRET=$XDG_RUNTIME_DIR/containers/auth.json`
   - This creates `rhai-pull-secret` in the namespaces: `cert-manager`, `cert-manager-operator`,
     `openshift-lws-operator`, `istio-system`
   - Alternative paths: `~/.docker/config.json` or `$HOME/.config/containers/auth.json`

6. **Deploy Cloud Manager** (installs cert-manager, Gateway API, LWS, Sail/Istio via Helm):
   `IMG=localhost/odh-operator:e2e make deploy-ccm-local-azure`

7. **Wait for Cloud Manager, then deploy AzureKubernetesEngine CR**:
   ```bash
   kubectl rollout status deployment -n opendatahub-cloudmanager-system \
     -l control-plane=controller-manager --timeout=180s
   kubectl apply -f config/cloudmanager/azure/samples/azurekubernetesengine_v1alpha1.yaml
   kubectl wait --for=condition=Ready azurekubernetesengine/default-azurekubernetesengine --timeout=300s
   ```

8. **Deploy operator** (KServe-only mode): `IMG=localhost/odh-operator:e2e make deploy-rhaii-local`

9. **Wait for operator**:
   ```bash
   kubectl wait --for=condition=Available deployment/opendatahub-operator-controller-manager \
     -n opendatahub-operator-system --timeout=600s
   ```

10. **Run E2E tests**: `make e2e-test-xks`

11. **Cleanup**: `make kind-delete`

## Full E2E on KinD (Experimental)

The standard `e2e-test-xks` target runs only KServe tests. It is possible to run the
broader component E2E suite on KinD with additional setup to bridge the gap between
KinD and OpenShift.

After completing the standard KinD setup (steps 1–8 above, using `make deploy` instead
of `make deploy-rhaii-local` — see [Deploying in full mode](#deploying-in-full-mode)):

### Required adjustments

Six adjustments bridge the gap between KinD and OpenShift. This is a high-level overview;
each adjustment requires manual implementation specific to your cluster setup:

1. **Kubelet storage isolation** — KinD disables `localStorageCapacityIsolation` by default,
   so nodes don't advertise ephemeral-storage capacity. The dashboard pod (9 containers)
   requests ephemeral-storage and won't schedule without it.

2. **Ghost CRDs** — The operator's controllers watch OpenShift-specific resource types
   (Route, ConsoleLink, Template, SCC, OperatorCondition, ImageStream, etc.). Stub CRDs
   with `x-kubernetes-preserve-unknown-fields: true` prevent controller startup failures.

3. **Fake OpenShift configs** — The gateway controller reads `ingresses.config.openshift.io/cluster`
   and `authentications.config.openshift.io/cluster`. Minimal stub resources satisfy these lookups.

4. **Webhook CA injection** — On OpenShift, the service-CA operator injects CA bundles into
   webhook configurations and CRD conversion webhooks. On KinD, the CA must be extracted from
   the `opendatahub-operator-controller-webhook-cert` secret and patched manually.

5. **Cert-manager Certificates** — Several operands (kuberay, model-serving-api,
   odh-model-controller, odh-notebook-controller, mlflow-operator) expect TLS secrets that
   are normally provisioned by platform services. Certificate CRs must be created manually
   using the `opendatahub-ca-issuer` ClusterIssuer.

6. **Pull secrets** — Images from `registry.redhat.io` require authentication. The pull secret
   must be present in the `opendatahub` namespace for operand pods.

### Create DSCI and DSC

After applying the adjustments, create the platform resources:

```bash
kubectl apply -f - <<EOF
apiVersion: dscinitialization.opendatahub.io/v2
kind: DSCInitialization
metadata:
  name: default-dsci
spec:
  applicationsNamespace: opendatahub
  monitoring:
    managementState: Removed
    namespace: opendatahub
  trustedCABundle:
    managementState: Removed
EOF

kubectl apply -f - <<EOF
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
    ray:
      managementState: Managed
    workbenches:
      managementState: Managed
    trainingoperator:
      managementState: Managed
    trustyai:
      managementState: Managed
    sparkoperator:
      managementState: Managed
    mlflowoperator:
      managementState: Managed
    feastoperator:
      managementState: Managed
    llamastackoperator:
      managementState: Managed
    modelregistry:
      managementState: Removed
    trainer:
      managementState: Removed
    kueue:
      managementState: Removed
EOF
```

Wait for the DSC to become ready:

```bash
kubectl wait --for=jsonpath='{.status.phase}'=Ready dsc/default-dsc --timeout=600s
```

### Run the tests

```bash
E2E_TEST_CLEAN_UP_PREVIOUS_RESOURCES=false \
E2E_TEST_DEPENDANT_OPERATORS_MANAGEMENT=false \
E2E_TEST_SERVICES=false \
E2E_TEST_WEBHOOK=false \
E2E_TEST_OPERATOR_RESILIENCE=false \
E2E_TEST_OPERATOR_V2TOV3UPGRADE=false \
E2E_TEST_DSC_MANAGEMENT=false \
E2E_TEST_DSC_VALIDATION=false \
E2E_TEST_DELETION_POLICY=never \
E2E_TEST_OPERATOR_CONTROLLER=false \
go test ./tests/e2e/ \
  -run "TestOdhOperator/components/.*/.*/(Validate_(component_enabled|component_conditions|update_operand|operands_have|resource_deletion|argoWorkflow|component_releases))" \
  -timeout=60m -v -count=1 -tags odh
```

### Known limitations

Four components cannot reach `Managed` state on KinD:

| Component | Reason |
|-----------|--------|
| ModelRegistry | Cert-manager secret file permissions (0600) incompatible with non-root container — requires OpenShift SCC `fsGroup` injection |
| Trainer | Requires JobSet operator (not just the CRD) |
| Kueue | Webhook validator rejects `Managed` state |
| ModelsAsService | Requires AuthConfig CRD (`authorino.kuadrant.io/v1beta3`) not available on KinD |

These components should be set to `managementState: Removed` in the DSC. Their tests will
be skipped.

### Deploying in full mode

The standard KinD path uses `make deploy-rhaii-local` which restricts to KServe-only mode.
For the full E2E, use `make deploy` instead, then patch imagePullPolicy:

```bash
IMG=localhost/odh-operator:e2e make deploy

kubectl patch deployment opendatahub-operator-controller-manager \
  -n opendatahub-operator-system --type=json \
  -p='[{"op":"replace","path":"/spec/template/spec/initContainers/0/imagePullPolicy","value":"IfNotPresent"},
       {"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"IfNotPresent"}]'
```

## Full E2E on OpenShift

### Step-by-step guide

1. **Build and push operator image**: `IMG=quay.io/<your-repo>/opendatahub-operator:dev make image-build image-push`

2. **Deploy the operator**: `IMG=quay.io/<your-repo>/opendatahub-operator:dev make deploy`

3. **Wait for operator**:
   ```bash
   oc wait --for=condition=Available deployment/opendatahub-operator-controller-manager \
     -n opendatahub-operator-system --timeout=600s
   ```

4. **Run full E2E tests**: `make e2e-test`

5. **Run a single test** (useful for debugging):
   `make e2e-test-single TEST="TestOdhOperator/Component_Tests/dashboard/Validate component enabled"`

### Running a single component

To test only one component without running the full suite:

```bash
make e2e-test \
  -e E2E_TEST_COMPONENT="dashboard" \
  -e E2E_TEST_SERVICES=false \
  -e E2E_TEST_WEBHOOK=false \
  -e E2E_TEST_OPERATOR_RESILIENCE=false \
  -e E2E_TEST_DSC_MANAGEMENT=false \
  -e E2E_TEST_DSC_VALIDATION=false
```

### Setting up cluster prerequisites only

To create DSCI and DSC resources without running component tests (useful for manual testing):
`make e2e-setup-cluster`

### Namespace defaults

The test suite inherits namespace configuration from the Makefile based on the platform
build tag (`-tags=odh` or `-tags=rhoai`):

| Variable | ODH default | RHOAI default |
|----------|-------------|---------------|
| `E2E_TEST_OPERATOR_NAMESPACE` | `opendatahub-operator-system` | `redhat-ods-operator` |
| `E2E_TEST_APPLICATIONS_NAMESPACE` | `opendatahub` | `redhat-ods-applications` |
| `E2E_TEST_WORKBENCHES_NAMESPACE` | `opendatahub` | `rhods-notebooks` |
| `E2E_TEST_DSC_MONITORING_NAMESPACE` | `opendatahub` | `redhat-ods-monitoring` |

## Troubleshooting

### Rootless Podman: KinD nodes have no internet access

**Symptoms:** Dependency operator pods stuck in `ImagePullBackOff`. `curl` from inside KinD
nodes times out.

**Root cause:** Rootless Podman requires IP forwarding and firewall masquerading to be
enabled for KinD container networking.

**Fix (run once per boot):**

```bash
sudo sysctl -w net.ipv4.ip_forward=1
```

If using `firewalld`, enable masquerading and forwarding for your active zone:

```bash
ZONE=$(firewall-cmd --get-default-zone)
sudo firewall-cmd --zone="$ZONE" --add-masquerade
sudo firewall-cmd --zone="$ZONE" --add-forward
```

To make permanent:

```bash
echo 'net.ipv4.ip_forward = 1' | sudo tee /etc/sysctl.d/99-ip-forward.conf
sudo firewall-cmd --zone="$ZONE" --add-masquerade --permanent
sudo firewall-cmd --zone="$ZONE" --add-forward --permanent
```

If using `iptables`/`nftables` directly instead of `firewalld`, ensure the `FORWARD` chain
accepts traffic and NAT masquerading is configured for the Podman network.

### Rootless Podman: DNS resolution fails inside KinD nodes

**Symptoms:** Pods can connect to IPs but DNS lookups time out. `resolv.conf` inside KinD
nodes points to Podman's `aardvark-dns` which doesn't forward external queries.

**Fix:** Override DNS on both KinD nodes:

```bash
podman exec kind-odh-control-plane bash -c 'echo "nameserver 8.8.8.8" > /etc/resolv.conf'
podman exec kind-odh-worker bash -c 'echo "nameserver 8.8.8.8" > /etc/resolv.conf'
```

> **Note:** This must be done after each cluster creation. The cluster must be created
> **after** enabling IP forwarding and firewall rules above.

### `kind load image-archive` fails with Podman

**Symptom:** `ERROR: failed to detect containerd snapshotter`

**Fix:** Load images manually via `ctr import`:

```bash
podman save -o /tmp/image.tar localhost/odh-operator:e2e
for node in kind-odh-control-plane kind-odh-worker; do
  podman cp /tmp/image.tar "$node":/tmp/img.tar
  podman exec "$node" ctr -n k8s.io images import /tmp/img.tar
  podman exec "$node" rm /tmp/img.tar
done
```

## Reference

### Makefile targets

| Target | Description |
|--------|-------------|
| `make kind-create` | Create KinD cluster (default: `kind-odh`) |
| `make kind-delete` | Delete KinD cluster |
| `make image-build` | Build operator image |
| `make image-kind-load` | Load image into KinD cluster |
| `make kind-setup-pull-secrets` | Configure pull secrets for `registry.redhat.io` |
| `make deploy-ccm-local-azure` | Deploy Cloud Manager with local image pull policy |
| `make deploy-rhaii-local` | Deploy operator in XKS/KServe-only mode |
| `make e2e-test-xks` | Run KinD E2E tests (KServe component) |
| `make e2e-test` | Run full E2E tests (requires OpenShift) |
| `make e2e-test-single TEST="<path>"` | Run a single E2E test |
| `make e2e-setup-cluster` | Create DSCI/DSC without running component tests |
| `make deploy` | Deploy operator via kustomize (OpenShift) |

### Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `IMG` | `quay.io/opendatahub/opendatahub-operator:latest` | Operator image |
| `IMAGE_BUILDER` | `podman` | Container image builder (`podman` or `docker`) |
| `CLUSTER_NAME` | `kind-odh` | KinD cluster name (used by `kind-create`) |
| `KIND_CLUSTER_NAME` | `kind-odh` | KinD cluster name (used by `image-kind-load`) |
| `KIND_CONFIG_PATH` | `config/kind/kind-config.yaml` | KinD cluster config |
| `PULL_SECRET` | *(required)* | Path to container registry auth config |
| `E2E_TEST_COMPONENT` | *(all)* | Single component to test (e.g., `kserve`) |
| `E2E_TEST_SERVICES` | `true` | Enable/disable service tests |
| `E2E_TEST_WEBHOOK` | `true` | Enable/disable webhook tests |
| `E2E_TEST_DSC_MANAGEMENT` | `true` | Enable/disable DSC lifecycle tests |
| `E2E_TEST_DSC_VALIDATION` | `true` | Enable/disable DSC validation tests |
| `E2E_TEST_OPERATOR_RESILIENCE` | `true` | Enable/disable operator resilience tests |
| `E2E_TEST_CLEAN_UP_PREVIOUS_RESOURCES` | `true` | Clean up existing resources before tests |
| `E2E_TEST_DEPENDANT_OPERATORS_MANAGEMENT` | `true` | Enable/disable dependent operator management |
| `E2E_TEST_OPERATOR_CONTROLLER` | `true` | Enable/disable operator controller tests |
| `E2E_TEST_OPERATOR_V2TOV3UPGRADE` | `true` | Enable/disable v2-to-v3 upgrade tests |
| `E2E_TEST_COMPONENTS` | `true` | Enable/disable component tests |
| `E2E_TEST_DELETION_POLICY` | `always` | Deletion policy for test resources (`never`, `always`, `on-failure`) |

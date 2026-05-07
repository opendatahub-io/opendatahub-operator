# Module Handler Developer Guide

This guide walks through the implementation steps required to integrate a new
modular component into the ODH Operator using the `modules` package. It
complements the
[Onboarding Guide](https://docs.google.com/document/d/1FgN_U-6XH8M-Mu6XNeldUlTPsnw7UyPCWg5NVJJdYnw),
which covers the broader architectural contract between the platform and module
operators.

## Prerequisites

Before starting, your module team must have:

1. A **module operator** (controller + manifests) in its own repository.
2. A **CRD** that follows the
   [API requirements](https://docs.google.com/document/d/1FgN_U-6XH8M-Mu6XNeldUlTPsnw7UyPCWg5NVJJdYnw/edit?tab=t.0#heading=h.6cfkan7f6fq)
   in the onboarding guide.
3. A **Helm chart** or **Kustomize overlays** packaging the module operator's
   Deployment, RBAC, CRD, and ConfigMap. The manifests must **not** include a
   CR instance; the platform creates the CR.

## Module CRD API Requirements

Your module CRD is the contract between the platform and your module operator.
The platform creates instances of it, projects fields into `.spec`, and reads
`.status` for aggregation. The following requirements must be met.

### Scope and cardinality

- **Scope:** Cluster-scoped.
- **Cardinality:** Singleton -- the platform creates exactly one instance.
- **Name enforcement:** The CRD must validate `metadata.name` to a single
  allowed value (e.g., `default`) using a CEL rule:
  ```yaml
  x-kubernetes-validations:
    - rule: "self.metadata.name == 'default'"
      message: "Only the name 'default' is allowed"
  ```

### API group and versioning

- **Group:** `components.platform.opendatahub.io` (for user-facing modules) or
  `services.platform.opendatahub.io` (for infrastructure services).
- **Version:** Must reflect the module's support level:
  - Developer preview: `v1alpha1`
  - Technology preview: `v1beta1`
  - General availability: `v1`

### Spec requirements

The CRD `.spec` is the primary source of truth for configuration.

**Zero-config defaults:** Every optional field must have a sensible default
that results in a working configuration. Mandatory fields must have strict
OpenAPI or CEL validation.

**Platform-managed fields:** The platform projects global settings (auth,
certificates, observability) into specific `.spec` fields on your CR. Your CRD
must expose these fields. For example, if your module needs authentication:

```yaml
spec:
  auth:
    enabled: true
    audiences:
      - https://api.openshift.com
```

The platform continuously reconciles these fields via Server-Side Apply and
will revert manual edits. Do not use a ConfigMap for platform-managed settings.

**Module-owned fields:** Your CRD can have additional `.spec` fields that the
platform does not set. These are managed by your module operator's own field
manager. Advanced users can edit the module CR directly for configuration not
exposed in the DSC (e.g., `spec.controllers[].resources` for pod sizing).

### Status requirements

The CRD `.status` must conform to the `PlatformObject` interface so the
platform can parse it generically.

**Required fields:**

| Field | Type | Description |
|---|---|---|
| `observedGeneration` | `int64` | Last `.metadata.generation` the module controller reconciled. The platform treats status as stale when this falls behind `metadata.generation`. |
| `conditions` | `[]metav1.Condition` | Standard Kubernetes conditions (see below). |
| `releases` | `[]Release` | Installed component versions (`name`, `repoUrl`, `version`). |

**Mandatory conditions:**

| Condition | True | False |
|---|---|---|
| `Ready` | Module is fully functional and available | Module is unhealthy, installing, or failed |
| `ProvisioningSucceeded` | Manifests applied successfully | Error during manifest application. Must be aggregated into `Ready`. |
| `Degraded` | Functional but degraded (non-critical sub-component failing) | Operating normally |

**Semantics:**
- `Ready=True, Degraded=True` -- partial availability (e.g., main service up
  but metrics collector crashing). The platform reports `ModulesReady=False`
  with a degraded message.
- `Ready=False` -- unusable. The platform reports `ModulesReady=False`.
- `Ready=True, Degraded=False` -- healthy. The platform reports
  `ModulesReady=True`.

### ConfigMap (strictly minimal)

If your module needs runtime flags that are not user-facing APIs, use a
ConfigMap included in your operator manifests.

| Belongs in `.spec` | Belongs in ConfigMap |
|---|---|
| User-configurable settings (ports, storage classes) | Internal controller flags (feature gates, debug toggles) |
| Platform-managed settings (auth, certs) | Environmental overrides |

The platform applies and enforces the ConfigMap via SSA. Your module operator
decides how to consume it (volume mount, watch, restart).

### Example CRD instance

```yaml
apiVersion: components.platform.opendatahub.io/v1alpha1
kind: MyModule
metadata:
  name: default
spec:
  managementState: Managed
  auth:
    enabled: true
    audiences:
      - https://api.openshift.com
  # Module-specific fields below
  grpcPort: 9090
  restPort: 8080
status:
  observedGeneration: 3
  conditions:
    - type: Ready
      status: "True"
      reason: Available
      message: "All components healthy"
      lastTransitionTime: "2026-04-28T10:00:00Z"
    - type: ProvisioningSucceeded
      status: "True"
      reason: Applied
      message: "Manifests applied successfully"
      lastTransitionTime: "2026-04-28T09:55:00Z"
    - type: Degraded
      status: "False"
      reason: NoWarnings
      message: ""
      lastTransitionTime: "2026-04-28T09:55:00Z"
  releases:
    - name: mymodule
      repoUrl: https://github.com/opendatahub-io/mymodule-operator
      version: "1.2.0"
```

### Dependency management

Modules must discover dependencies dynamically by querying the Kubernetes API
for the existence and status of other module/component CRs.

- **Optional dependency missing:** Disable related functionality, set
  `Degraded=True` if needed, keep `Ready=True`.
- **Critical dependency missing:** Set `Ready=False` with a clear reason.
  Do not crash the controller loop -- wait for the dependency to appear.

### Namespace model

This is an important shift from the in-tree component model. In-tree
components were rendered and applied by the platform operator binary
itself, which runs in the **operator namespace** (e.g.,
`openshift-operators`) but projects operand resources into the
**applications namespace** (e.g., `opendatahub`). With modules, the
module operator is a **separate Deployment** that the platform deploys --
the module operator and its operands are co-located rather than being
managed from a different namespace.

Module operators support two namespace strategies. How the namespace is
determined depends on the manifest format (see "How namespaces flow
through the system" below).

**Applications namespace (default):**

The module operator Deployment and its operands deploy into
`ApplicationsNamespace` (typically `opendatahub` or
`redhat-ods-applications`). This is the simplest model. The handler reads
`platform.ApplicationsNamespace` and uses it as a Helm value / Kustomize
namespace for the operator manifests.

**Dedicated namespace:**

The module operator and its operands deploy into their own namespace
(e.g., `mymodule-system`). This matches the pattern used by OpenShift
operators like Kueue and JobSet, where the operator and its managed
resources are co-located in a single purpose-built namespace. For
Helm-based modules the handler sets the namespace via
`ModuleConfig.Values`. For Kustomize-based modules the handler sets
`ModuleConfig.Namespace`, which is carried on `ManifestInfo.Namespace`
and used by the Kustomize render action instead of the default
`ApplicationsNamespace`.

Even when using a dedicated namespace, the platform injects an
`APPLICATIONS_NAMESPACE` environment variable into the module operator's
Deployment. This tells the module operator where the shared platform
namespace is, which may be needed for cross-namespace resource discovery
or for deploying workloads that interact with other modules.

**How namespaces flow through the system:**

| Manifest format | How the namespace is set |
|---|---|
| **Helm** | The chart templates use `{{ .Values.namespace }}` or similar. The handler sets this via `ModuleConfig.Values` and/or `ModuleConfig.Namespace`. |
| **Kustomize** | The Kustomize render action calls `kustomize.WithNamespace(ns)` where `ns` is `ManifestInfo.Namespace` if set, otherwise `ApplicationsNamespace`. The handler controls this via `ModuleConfig.Namespace`. |
| **Module CR** | `BuildModuleCR` calls `u.SetNamespace(...)` with whatever namespace the module uses. For cluster-scoped module CRs, this is not set. |

**Choosing a strategy:**

| Consideration | Applications namespace | Dedicated namespace |
|---|---|---|
| Simplicity | Simpler -- no extra namespace to manage | Requires namespace creation in manifests |
| Isolation | Shares namespace with other modules | Full isolation of resources and RBAC |
| RBAC | Role/RoleBinding in the applications namespace | Role/RoleBinding in the module's own namespace |
| Migrating from in-tree | Natural fit -- operands stay in the same namespace; the new module operator Deployment joins them there | Requires resource migration to the new namespace |
| Cross-module access | Direct -- all modules in the same namespace | Requires cross-namespace RBAC or Service references |

**Changing namespace after deployment:**

Switching a module's namespace model on an existing deployment is a
**breaking change** that is fundamentally different from the clean
remove-ownership / take-ownership model used for component-to-module
migration. A namespace change cannot be achieved by simply re-rendering
manifests with a new namespace -- Kubernetes resources are identified by
`(namespace, name)`, so changing the namespace creates entirely new
resources rather than updating existing ones. This means:

- **Data loss risk.** PersistentVolumeClaims, Secrets, and ConfigMaps in
  the old namespace are not automatically moved. Custom migration logic
  is needed to copy or recreate stateful resources.
- **DNS breakage.** Service DNS names (`service.namespace.svc`) change
  immediately. All clients -- including other modules, Ingress/Route
  backends, and hardcoded references in ConfigMaps or CRs -- must be
  updated.
- **Orphaned resources.** The platform's garbage collector (`gc.NewAction`)
  tracks resources by the labels it applied. Resources in the old
  namespace will be orphaned (or deleted by GC if they carry platform
  labels), but either outcome requires careful coordination.
- **RBAC re-binding.** RoleBindings are namespace-scoped. The module
  operator's ServiceAccount needs new RoleBindings in the target
  namespace and the old bindings need cleanup.
- **Downtime.** There is no atomic namespace move in Kubernetes. The
  module will be unavailable between teardown in the old namespace and
  full readiness in the new one.

This is not a routine configuration change -- it requires bespoke
migration logic per module. Teams should treat a namespace change as a
major version migration and plan accordingly.

### Certificate management

Module controllers that need internal TLS certificates (e.g., for admission
webhooks or mTLS) should use **cert-manager** for provisioning and rotation.
Do not depend on OpenShift serving certificates.

### Webhook ownership

Module operators are responsible for managing all admission webhooks related
to their workloads. This includes:

- **HardwareProfile injection.** The platform deploys the HardwareProfile CRD
  and default profiles, but the mutating webhook that injects HWP settings
  (resource requests, tolerations, node selectors, Kueue queue labels) into
  workloads is the module's responsibility. If your module creates workloads
  that users attach hardware profiles to (e.g., Notebooks, InferenceServices),
  your module operator must register and maintain the mutating webhook for
  those GVKs.

- **CRD conversion / migration webhooks.** If your module CRD evolves across
  API versions (e.g., `v1alpha1` to `v1beta1`), your module operator owns
  the conversion webhook. The platform does not manage CRD version conversion
  for module CRDs.

- **Validating webhooks.** Any validation logic for resources your module
  manages (e.g., blocking invalid configurations, enforcing naming
  conventions) is your module operator's responsibility.

The platform operator does **not** register or manage webhooks on behalf of
modules. When a component migrates from in-tree to a module, any webhooks the
platform previously managed for that component's workloads must be
re-implemented in the module operator.

---

## Overview

Adding a module to the operator requires changes in five areas:

| Area | What you add | Where |
|---|---|---|
| **Manifest source** | Entry in manifest-gathering script | `get_all_manifests.sh` |
| **Handler** | Go package implementing `ModuleHandler` | `internal/controller/modules/<name>/` |
| **Operand images** | `RELATED_IMAGE_*` declarations in handler + `build/operands-map.yaml` | `ModuleConfig.RelatedImages` |
| **DSC API** | Component stanza on `DataScienceCluster` | `api/datasciencecluster/v2/` |
| **Registration** | Import + map entry | `cmd/main.go` |

The following sections detail each area using a fictional "mymodule" module as
an example.

---

## Step 1: Provide the Manifests

The operator pulls module manifests at image build time via
`get_all_manifests.sh`. Add entries to the `ODH_COMPONENT_CHARTS` (community)
and `RHOAI_COMPONENT_CHARTS` (product) maps:

```bash
# In ODH_COMPONENT_CHARTS
["mymodule"]="opendatahub-io:mymodule-operator:main@<commit-sha>:charts/operator"

# In RHOAI_COMPONENT_CHARTS
["mymodule"]="red-hat-data-services:mymodule-operator:rhoai-X.Y@<commit-sha>:charts/operator"
```

Module teams can package their operator manifests as either **Helm charts** or
**Kustomize overlays**. The manifests will be extracted to `opt/manifests/mymodule/`
inside the operator image. Set `ModuleConfig.ChartDir` for Helm or
`ModuleConfig.ManifestDir` for Kustomize in the handler.

### What the manifests should contain

- `Deployment` for the module controller
- `ServiceAccount`, `ClusterRole`, `ClusterRoleBinding` for RBAC
- The module's `CRD` (so the platform can create instances)
- Optional: `ConfigMap` for controller configuration

### What the manifests must NOT contain

- A CR instance (e.g., `MyModule` kind). The platform operator creates and owns
  the CR via `BuildModuleCR`.

### Example manifest resources

The following examples show the typical resources a module operator's
manifests include. These are what the platform applies via Server-Side Apply
when deploying the module operator.

#### ServiceAccount

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: mymodule-operator
  labels:
    app.kubernetes.io/name: mymodule-operator
```

#### ClusterRole (module CR access only)

The module CR is cluster-scoped, so the module operator needs a ClusterRole
to reconcile it. **This ClusterRole should be limited to the module's own
CRD** -- do not put operand resource permissions here unless required.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: mymodule-operator
rules:
  # Reconcile the module CR (cluster-scoped)
  - apiGroups: ["components.platform.opendatahub.io"]
    resources: ["mymodules"]
    verbs: [get, list, watch]
  - apiGroups: ["components.platform.opendatahub.io"]
    resources: ["mymodules/status"]
    verbs: [get, patch, update]
  - apiGroups: ["components.platform.opendatahub.io"]
    resources: ["mymodules/finalizers"]
    verbs: [update]

  # Read the DSC for platform configuration (read-only)
  - apiGroups: ["datasciencecluster.opendatahub.io"]
    resources: [datascienceclusters]
    verbs: [get, list, watch]

  # Leader election (if using controller-runtime)
  - apiGroups: ["coordination.k8s.io"]
    resources: [leases]
    verbs: [create, get, list, update, watch]
  - apiGroups: [""]
    resources: [events]
    verbs: [create, patch]
```

The DSC read permission allows the module operator to check whether other
modules or components are enabled (inter-module awareness), read
user-facing configuration that is not projected into the module CR, or
display platform-level information in a UI. This access is **read-only**
-- module operators must never write to the DSC.

#### ClusterRoleBinding

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: mymodule-operator
  labels:
    app.kubernetes.io/name: mymodule-operator
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: mymodule-operator
subjects:
  - kind: ServiceAccount
    name: mymodule-operator
```

Note: The `namespace` field on the `subjects` entry is omitted in the
manifests. The platform sets the namespace during Kustomize rendering or
Helm templating based on the applications namespace. If your manifests
use Kustomize, the `namespace` transformer handles this. If using Helm,
template the namespace from chart values.

#### Role (namespace-scoped operand permissions)

Operand resources (Deployments, Services, ConfigMaps, etc.) live in the
applications namespace. Permissions for these **must** use a
namespace-scoped `Role`, not a `ClusterRole`. This follows the principle
of least privilege and prevents module operators from having
cluster-wide access to resources they only manage in a single namespace.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: mymodule-operator
rules:
  # Manage operand Deployments
  - apiGroups: ["apps"]
    resources: [deployments]
    verbs: [create, delete, get, list, patch, update, watch]

  # Manage operand Services, ConfigMaps
  - apiGroups: [""]
    resources: [services, configmaps]
    verbs: [create, delete, get, list, patch, update, watch]

  # Read secrets (e.g., TLS certs, credentials)
  - apiGroups: [""]
    resources: [secrets]
    verbs: [get, list, watch]
```

#### RoleBinding

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: mymodule-operator
  labels:
    app.kubernetes.io/name: mymodule-operator
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: mymodule-operator
subjects:
  - kind: ServiceAccount
    name: mymodule-operator
```

#### RBAC scoping guidelines

**Split cluster vs namespace permissions.** The recommended pattern is to
use a ClusterRole only for cluster-scoped resources (the module CR, leader
election) and a Role for everything else. This gives the module operator
the minimum privilege needed in each scope.

| Use ClusterRole for | Use Role for |
|---|---|
| Module CR (get, list, watch, status, finalizers) | Operand Deployments, Services, ConfigMaps |
| Leader election (coordination.k8s.io/leases) | Secrets, PersistentVolumeClaims |
| Cluster-scoped CRDs the module owns | Namespace-scoped operand CRDs |
| Events (create, patch) | Pods (if the module needs to list/watch them) |

**When cluster-scoped operand permissions are justified:**

Some modules genuinely need cluster-wide access -- for example, a module
that creates ClusterRoles for end users, manages resources across multiple
namespaces, or registers webhooks. In these cases, add the specific rules
to the ClusterRole with a comment explaining why cluster scope is required.
The platform team reviews all cluster-scoped permissions during onboarding.

**Dedicated namespace RBAC.** If the module uses a
[dedicated namespace](#namespace-model), the Role and RoleBinding target
that namespace instead of the applications namespace. The Helm chart or
Kustomize overlays set the namespace on these resources. If the module
also needs to access resources in the applications namespace (e.g., to
read shared ConfigMaps or Secrets), add a second Role + RoleBinding
scoped to the applications namespace with only the specific permissions
required.

**Avoid wildcards.** Never use `*` for API groups, resources, or verbs.
Enumerate exactly what the module needs. The platform team reviews RBAC
as part of the module onboarding process and will request changes if
permissions are overly broad.

#### Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mymodule-operator
  labels:
    app.kubernetes.io/name: mymodule-operator
    control-plane: mymodule-operator
spec:
  replicas: 1
  selector:
    matchLabels:
      control-plane: mymodule-operator
  template:
    metadata:
      labels:
        control-plane: mymodule-operator
      annotations:
        sidecar.istio.io/inject: "false"
    spec:
      serviceAccountName: mymodule-operator
      terminationGracePeriodSeconds: 10
      containers:
        - name: manager
          image: quay.io/opendatahub/mymodule-operator:latest
          command: ["/manager"]
          ports:
            - containerPort: 8080
              name: metrics
            - containerPort: 8081
              name: health
          env:
            - name: MY_POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
          resources:
            requests:
              cpu: 50m
              memory: 128Mi
            limits:
              cpu: 500m
              memory: 512Mi
          livenessProbe:
            httpGet:
              path: /healthz
              port: 8081
            initialDelaySeconds: 15
            periodSeconds: 20
          readinessProbe:
            httpGet:
              path: /readyz
              port: 8081
            initialDelaySeconds: 10
            periodSeconds: 15
          securityContext:
            allowPrivilegeEscalation: false
```

The platform injects `RELATED_IMAGE_*` and `APPLICATIONS_NAMESPACE`
environment variables into the Deployment's `manager` container (or the
first container if no `manager` is found) after rendering and before
deploy (see
[Operand Image Injection](#operand-image-injection-related_image)). You
do not need to declare these env vars in the manifest; the platform adds
them automatically based on the handler's `ModuleConfig.RelatedImages`.

#### ConfigMap (controller configuration)

If your module operator needs runtime configuration flags that are not
user-facing APIs (feature gates, debug toggles, internal tuning), include
a ConfigMap in the manifests. The platform applies it via Server-Side Apply
and enforces it on every reconcile.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: mymodule-operator-config
  labels:
    app.kubernetes.io/name: mymodule-operator
data:
  ENABLE_WEBHOOKS: "true"
  LOG_LEVEL: "info"
  LEADER_ELECT: "true"
  MAX_CONCURRENT_RECONCILES: "1"
```

The module operator consumes this ConfigMap either as environment variables
(via `envFrom` in the Deployment) or as a mounted volume. Choose based on
how your controller framework loads configuration:

**As environment variables:**

```yaml
# In the Deployment container spec
envFrom:
  - configMapRef:
      name: mymodule-operator-config
```

**As a volume mount:**

```yaml
# In the Deployment pod spec
volumes:
  - name: config
    configMap:
      name: mymodule-operator-config
# In the container spec
volumeMounts:
  - name: config
    mountPath: /etc/mymodule
    readOnly: true
```

Refer to the [ConfigMap enforcement](#configmap-enforcement) section for
the platform's enforcement semantics.

---

## Step 2: Implement the Handler

Create a new package under `internal/controller/modules/<name>/`. The handler
embeds `modules.BaseHandler` and only implements two methods: `IsEnabled` and
`BuildModuleCR`.

### File: `internal/controller/modules/mymodule/handler.go`

The `ModuleConfig` determines the manifest format. Set `ChartDir` for Helm or
`ManifestDir` for Kustomize.

**Helm variant:**

```go
package mymodule

import (
    "context"

    operatorv1 "github.com/openshift/api/operator/v1"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "sigs.k8s.io/controller-runtime/pkg/client"

    "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
)

type handler struct {
    modules.BaseHandler
}

func NewHandler() *handler {
    return &handler{
        BaseHandler: modules.BaseHandler{
            Config: modules.ModuleConfig{
                Name:        "mymodule",
                CRName:      "default",
                ChartDir:    "mymodule",
                ReleaseName: "mymodule-operator",
                GVK: schema.GroupVersionKind{
                    Group:   "components.platform.opendatahub.io",
                    Version: "v1alpha1",
                    Kind:    "MyModule",
                },
                RelatedImages: []string{
                    "RELATED_IMAGE_ODH_MYMODULE_CONTROLLER_IMAGE",
                    "RELATED_IMAGE_ODH_MYMODULE_SIDECAR_IMAGE",
                },
            },
        },
    }
}

// Component module: reads DSC component stanza.
func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
    return platform.DSC.Spec.Components.MyModule.ManagementState == operatorv1.Managed
}

// Service module alternative: reads DSCI service configuration.
// func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
//     return platform.DSCI.Spec.Monitoring.ManagementState == operatorv1.Managed
// }

func (h *handler) BuildModuleCR(
    ctx context.Context,
    cli client.Client,
    platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
    u := &unstructured.Unstructured{}
    u.SetGroupVersionKind(h.Config.GVK)
    u.SetName(h.Config.CRName)
    u.SetNamespace(platform.ApplicationsNamespace)

    u.Object["spec"] = map[string]any{
        "managementState": string(platform.DSC.Spec.Components.MyModule.ManagementState),
    }

    return u, nil
}
```

**Kustomize variant** -- only `ModuleConfig` differs:

```go
func NewHandler() *handler {
    return &handler{
        BaseHandler: modules.BaseHandler{
            Config: modules.ModuleConfig{
                Name:        "mymodule",
                CRName:      "default",
                ManifestDir: "mymodule",
                ContextDir:  "operator",
                SourcePath:  "overlays/production",
                GVK: schema.GroupVersionKind{
                    Group:   "components.platform.opendatahub.io",
                    Version: "v1alpha1",
                    Kind:    "MyModule",
                },
                RelatedImages: []string{
                    "RELATED_IMAGE_ODH_MYMODULE_CONTROLLER_IMAGE",
                    "RELATED_IMAGE_ODH_MYMODULE_SIDECAR_IMAGE",
                },
            },
        },
    }
}
```

The `IsEnabled` and `BuildModuleCR` methods are identical regardless of
manifest format.

**Dedicated namespace variant** -- if the module deploys into its own
namespace instead of `ApplicationsNamespace`, set `ModuleConfig.Namespace`.
The module CR is cluster-scoped so it does not get a namespace, but the
module operator's Deployment and operands will render into the declared
namespace. The platform still injects `APPLICATIONS_NAMESPACE` as an env
var on the rendered Deployment so the module operator knows where the
shared platform namespace is.

**Helm dedicated namespace:**

For Helm modules, `Namespace` is also passed through `Values` so the
chart templates can reference it (e.g., `{{ .Values.namespace }}`):

```go
const moduleNamespace = "mymodule-system"

func NewHandler() *handler {
    return &handler{
        BaseHandler: modules.BaseHandler{
            Config: modules.ModuleConfig{
                Name:        "mymodule",
                CRName:      "default",
                ChartDir:    "mymodule",
                ReleaseName: "mymodule-operator",
                Namespace:   moduleNamespace,
                GVK: schema.GroupVersionKind{
                    Group:   "components.platform.opendatahub.io",
                    Version: "v1alpha1",
                    Kind:    "MyModule",
                },
                Values: map[string]any{
                    "namespace": moduleNamespace,
                },
                RelatedImages: []string{
                    "RELATED_IMAGE_ODH_MYMODULE_CONTROLLER_IMAGE",
                },
            },
        },
    }
}
```

**Kustomize dedicated namespace:**

For Kustomize modules, `Namespace` overrides the default
`ApplicationsNamespace` in the Kustomize render action. No changes to
the `kustomization.yaml` are needed -- the platform sets the namespace
programmatically:

```go
const moduleNamespace = "mymodule-system"

func NewHandler() *handler {
    return &handler{
        BaseHandler: modules.BaseHandler{
            Config: modules.ModuleConfig{
                Name:        "mymodule",
                CRName:      "default",
                ManifestDir: "mymodule",
                ContextDir:  "operator",
                SourcePath:  "overlays/production",
                Namespace:   moduleNamespace,
                GVK: schema.GroupVersionKind{
                    Group:   "components.platform.opendatahub.io",
                    Version: "v1alpha1",
                    Kind:    "MyModule",
                },
                RelatedImages: []string{
                    "RELATED_IMAGE_ODH_MYMODULE_CONTROLLER_IMAGE",
                },
            },
        },
    }
}
```

**`BuildModuleCR` for dedicated namespace** (same for both formats):

```go
func (h *handler) BuildModuleCR(
    ctx context.Context,
    cli client.Client,
    platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
    u := &unstructured.Unstructured{}
    u.SetGroupVersionKind(h.Config.GVK)
    u.SetName(h.Config.CRName)
    // Module CR is cluster-scoped -- no SetNamespace call.

    u.Object["spec"] = map[string]any{
        "managementState": string(platform.DSC.Spec.Components.MyModule.ManagementState),
    }

    return u, nil
}
```

### PlatformContext -- available platform fields

`IsEnabled` and `BuildModuleCR` both receive a `*modules.PlatformContext`
that is built once per reconcile and contains all platform-level fields a
handler may need:

| Field | Type | Source | Description |
|---|---|---|---|
| `ApplicationsNamespace` | `string` | `DSCI.Spec.ApplicationsNamespace` | The shared platform namespace. Used as the default for module operators and operands. Modules using a [dedicated namespace](#namespace-model) can ignore this for their own resources but may still need it for cross-namespace discovery. Also injected as an `APPLICATIONS_NAMESPACE` env var on the module operator Deployment. |
| `GatewayDomain` | `string` | `GatewayConfig.Status.Domain` | Cluster ingress domain. Empty if the gateway is not yet provisioned; handlers needing it should check for empty and handle gracefully. |
| `Release` | `common.Release` | `rr.Release` | Platform identity (ODH vs RHOAI) and version. Useful for conditional behaviour. |
| `DSC` | `*dscv2.DataScienceCluster` | reconcile instance | The DSC instance. Component module handlers read their component stanza (e.g., `platform.DSC.Spec.Components.MyModule`). |
| `DSCI` | `*dsciv2.DSCInitialization` | `cluster.GetDSCI()` | The DSCInitialization instance. Service module handlers read their service configuration (e.g., `platform.DSCI.Spec.Monitoring`). Also used by `IsEnabled` for service modules. |

#### How PlatformContext replaces in-tree component patterns

In-tree components access platform data through the
`ReconciliationRequest` (`rr`) object -- fields like `rr.Release.Name`,
`rr.ManifestsBasePath`, `rr.DSCI`, and live API lookups via `rr.Client`.
Module handlers do not receive a `ReconciliationRequest`. Instead,
`PlatformContext` is a curated subset of the same data, designed to give
handlers exactly what they need without exposing reconciler internals.

The following table maps common in-tree patterns to their module handler
equivalents:

| In-tree component pattern | Module handler equivalent |
|---|---|
| `rr.Release.Name` for platform-conditional behaviour (ODH vs RHOAI overlay selection, namespace defaults) | `platform.Release.Name` -- same type, same values. |
| `cluster.GetDSCI(ctx, rr.Client)` to read `ApplicationsNamespace` | `platform.ApplicationsNamespace` -- already resolved, no API call needed. |
| `cluster.GetDSCI(ctx, rr.Client)` to read service config (e.g., monitoring) | `platform.DSCI` -- the full `DSCInitialization` instance, no API call needed. |
| `resources.GetGatewayDomain(ctx, rr.Client)` or `gateway.GetGatewayDomain(ctx, cli)` for ingress URLs | `platform.GatewayDomain` -- already resolved. May be empty if gateway is not provisioned. |
| `rr.Instance.(*componentApi.FeastOperator)` to read the component CR spec | `platform.DSC.Spec.Components.MyModule` -- read your component stanza from the DSC, then project the fields into the module CR via `BuildModuleCR`. |
| `rr.ManifestsBasePath` for Kustomize overlay paths | Not needed in the handler. `ModuleConfig.ChartDir` / `ManifestDir` declare where manifests live; `BaseHandler` wires the paths. |

#### Using PlatformContext in BuildModuleCR

The primary job of `BuildModuleCR` is to **project** platform data into
your module CR's `.spec` so your module operator can consume it without
needing access to platform resources. Every field your module operator
needs from the platform should flow through the module CR spec, not
through direct API lookups by the module operator.

**Minimal example** -- only management state:

```go
func (h *handler) BuildModuleCR(
    ctx context.Context,
    cli client.Client,
    platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
    u := &unstructured.Unstructured{}
    u.SetGroupVersionKind(h.Config.GVK)
    u.SetName(h.Config.CRName)

    u.Object["spec"] = map[string]any{
        "managementState": string(platform.DSC.Spec.Components.MyModule.ManagementState),
    }

    return u, nil
}
```

**Richer example** -- projecting gateway domain, OIDC, and namespace:

```go
func (h *handler) BuildModuleCR(
    ctx context.Context,
    cli client.Client,
    platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
    u := &unstructured.Unstructured{}
    u.SetGroupVersionKind(h.Config.GVK)
    u.SetName(h.Config.CRName)

    spec := map[string]any{
        "managementState": string(platform.DSC.Spec.Components.Feast.ManagementState),
        "namespace":       platform.ApplicationsNamespace,
    }

    if platform.GatewayDomain != "" {
        spec["gateway"] = map[string]any{
            "domain": platform.GatewayDomain,
        }
    }

    u.Object["spec"] = spec
    return u, nil
}
```

#### What belongs in PlatformContext vs. module operator API calls

The design principle is: **PlatformContext carries platform-level
configuration; the module operator discovers cluster-level state.**

| Belongs in PlatformContext (handler reads it) | Module operator discovers on its own |
|---|---|
| Applications namespace | External operator CRD existence (Istio, cert-manager, JobSet) |
| Gateway domain | FIPS mode, disconnected environment detection |
| Platform identity (ODH / RHOAI) | Cluster version, node topology |
| DSC component stanza (management state, user config) | Operand health, pod status, readiness |
| DSCI service configuration (monitoring, trusted CA) | Secrets, certificates in the applications namespace |
| Auth configuration projected by the platform | |

If your handler needs platform data that is not currently in
`PlatformContext`, propose adding it to the struct rather than having the
handler call `cli.Get()` for platform resources. This keeps the handler
contract explicit and testable -- unit tests can construct a
`PlatformContext` directly without a running cluster.

### What BaseHandler provides for free

By embedding `BaseHandler` with a populated `ModuleConfig`, you inherit working
implementations of five interface methods:

| Method | What it does |
|---|---|
| `GetName()` | Returns `Config.Name` |
| `GetGVK()` | Returns `Config.GVK` (used for dynamic watch registration) |
| `GetRelatedImages()` | Returns `Config.RelatedImages` (used by `injectModuleEnv` to inject `RELATED_IMAGE_*` env vars) |
| `GetOperatorManifests()` | Returns `OperatorManifests` with `HelmCharts` (if `ChartDir` set) and/or `Manifests` (if `ManifestDir` set) |
| `GetModuleStatus()` | GETs the module CR by GVK + CRName, parses `.status.conditions` and `.status.observedGeneration`, returns `*ModuleStatus` |

You only need to implement:

- **`IsEnabled`**: Read `PlatformContext` to decide if this module should be deployed. Component modules check `platform.DSC`; service modules check `platform.DSCI`.
- **`BuildModuleCR`**: Construct the module CR as an `unstructured.Unstructured`
  object, projecting platform fields from the `PlatformContext`.

### Overriding defaults

Any default method can be overridden by defining it on your handler struct. For
example, if the module needs custom status parsing:

```go
func (h *handler) GetModuleStatus(ctx context.Context, cli client.Client) (*modules.ModuleStatus, error) {
    // Custom logic...
}
```

### The BuildModuleCR contract

The module CR returned by `BuildModuleCR` is:

1. Added to `rr.Resources` by `provisionModules`.
2. Applied to the cluster via Server-Side Apply by `deploy.NewAction` (field
   manager: `opendatahub-operator`).
3. Cleaned up by `gc.NewAction` when the module is disabled (the CR is no
   longer in `rr.Resources`).

The platform owns the fields it sets in `.spec`. The module operator can own
additional `.spec` fields via its own field manager. This is the shared
ownership model described in the onboarding guide.

### Platform labels and annotations

`deploy.NewAction` automatically sets `platform.opendatahub.io/part-of` and
platform annotations (instance generation, name, UID, release info) on every
resource in `rr.Resources`. Module CRs and module operator resources both
receive these labels without any extra code in the handler. Module teams do
**not** need to set platform labels in `BuildModuleCR`.

### Module CR status contract

The platform reads module CR status per the
[onboarding guide's PlatformObject contract](Onboarding%20Guide%20for%20ODH%20Operator%20Modules.md#23-status-specification-platformobject).
Module teams must ensure their CRD status includes:

- `observedGeneration` (int64): the last `.metadata.generation` the module
  controller has reconciled. The platform treats status as stale when this
  falls behind `metadata.generation`.
- `conditions` ([]metav1.Condition) with at least:
  - `Ready`: aggregate health (`True` = fully functional, `False` = unusable).
  - `ProvisioningSucceeded`: manifest application result (aggregated into
    `Ready`).
  - `Degraded`: `True` when functional but degraded. The platform propagates
    this into the DSC `ModulesReady` condition message even when `Ready=True`.
- `releases` (array of {name, repoUrl, version}): installed component info.

### ConfigMap enforcement

If the module chart includes a ConfigMap for controller configuration, the
platform applies it via `deploy.NewAction` (Server-Side Apply). User edits to
platform-managed ConfigMap fields are automatically reverted on the next
reconcile cycle, matching the enforcement model described in the onboarding
guide (section 2.4).

---

## Step 3: Add the DSC API Stanza

Users enable modules through the `DataScienceCluster` CR. Add a field to the
`Components` struct in `api/datasciencecluster/v2/datasciencecluster_types.go`:

```go
// MyModule component configuration.
MyModule DSCMyModule `json:"mymodule,omitempty"`
```

Define the types (typically in `api/components/v1alpha1/`):

```go
type DSCMyModule struct {
    common.ManagementSpec `json:",inline"`
    MyModuleCommonSpec    `json:",inline"`
}

type MyModuleCommonSpec struct {
    // Module-specific fields exposed through the DSC.
}
```

After modifying API types, regenerate:

```bash
make generate
make manifests
```

---

## Step 4: Register the Handler

In `cmd/main.go`, import the handler and add it to `existingModules`:

```go
import mymodule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/mymodule"

// In the var block:
existingModules = map[string]mr.ModuleHandler{
    "mymodule": mymodule.NewHandler(),
}
```

This registration:

1. Adds the handler to the module registry.
2. Creates a `--disable-mymodule-module` CLI flag for suppression.
3. Enables dynamic watch setup for the module CR's GVK.

---

## How It Works at Runtime

### Reconciliation flow

When the DSC controller reconciles:

1. **`provisionModules`** builds a `PlatformContext` once (fetching
   `ApplicationsNamespace`, `GatewayDomain`, `Release`, the DSC instance,
   and the DSCI instance), stores the DSCI on `rr.DSCI` for reuse, then
   iterates handlers, calling `IsEnabled(&platformCtx)` on each to determine
   which are active:
   - Calls `GetRelatedImages()` on each handler and collects a deduplicated
     union of `RELATED_IMAGE_*` names.
   - Appends the module's manifest descriptors to `rr.HelmCharts` and/or
     `rr.Manifests` (depending on the handler's `ModuleConfig`).
   - Calls `BuildModuleCR(&platformCtx)` and appends the result to `rr.Resources`.
   - Stores the collected `RelatedImages` and `ApplicationsNamespace` on
     `rr.ModuleEnvInjection` for downstream consumption.
2. **`helm.NewAction`** renders Helm charts into Kubernetes resources and
   appends them to `rr.Resources`.
3. **`kustomize.NewAction`** renders Kustomize manifests into Kubernetes
   resources and appends them to `rr.Resources`.
4. **`injectModuleEnv`** iterates `rr.Resources` for `apps/v1 Deployment`
   objects. For each, it finds the `manager` container (falling back to
   the first container if none is named `manager`), reads `RELATED_IMAGE_*`
   values from the platform operator's process environment (via
   `os.Getenv`), and appends them plus `APPLICATIONS_NAMESPACE` to that
   container's `env` list. Variables with empty values are skipped. This
   is a no-op if `rr.ModuleEnvInjection` is nil (no modules enabled).
5. **`deploy.NewAction`** applies everything in `rr.Resources` via
   Server-Side Apply. It automatically sets `platform.opendatahub.io/part-of`
   labels and platform annotations on all resources, including module CRs.
6. **`updateModuleStatus`** builds a `PlatformContext` from `rr` fields
   (reusing `rr.DSCI` to avoid a duplicate fetch), calls
   `IsEnabled(&platformCtx)` to filter to active modules, then reads each
   module CR's status and aggregates it into the DSC's `ModulesReady`
   condition. It performs:
   - **Staleness detection**: if `status.observedGeneration` is behind
     `metadata.generation`, the module is treated as not-ready.
   - **Ready check**: the `Ready` condition must be `True`.
   - **Degraded propagation**: if `Ready=True` but `Degraded=True`, the
     module is reported as degraded (`ModulesReady` is set to `False`).
7. **`gc.NewAction`** deletes resources that were previously managed but are no
   longer in `rr.Resources` (handles disablement and removal).

### Watch infrastructure

`SetupModuleWatches` (called after the DSC controller is built) registers a
watch for each module handler's GVK. When a module operator updates its CR
status, the watch maps the event to a DSC reconcile request so the platform can
pick up the updated status.

### Suppression flags

Each registered module gets a `--disable-<name>-module` flag. When set, the
registry marks the handler as disabled and `provisionModules` skips it. Since
the module's resources were present in a previous reconcile, `gc.NewAction`
detects they are missing and cleans them up.

---

## Operand Image Injection (`RELATED_IMAGE_*`)

Module operators need container image references for the operands they deploy
(controller images, sidecar images, workbench images, runtime images, etc.).
In connected environments a module operator can use hardcoded defaults, but
in **disconnected / air-gapped** clusters the images must come from a mirrored
registry. The platform operator solves this by **injecting `RELATED_IMAGE_*`
environment variables into the module operator's Deployment** after rendering
and before deploy.

### Why `RELATED_IMAGE_*` variables matter

OLM and the Red Hat operator certification pipeline require that every
container image an operator deploys is declared as a **related image** in the
operator bundle (CSV). At runtime the platform operator receives these
references as `RELATED_IMAGE_*` environment variables on its own Deployment.
The platform must then **forward the subset of variables each module needs**
into that module's operator Deployment so the module operator can resolve
images without hard-coding registry URLs.

This mechanism is critical for:

- **Disconnected clusters:** Image mirrors are configured at the cluster
  level; `RELATED_IMAGE_*` values carry the mirrored digests.
- **FIPS and supply-chain compliance:** Digest-pinned references from
  the CSV guarantee provenance.
- **Multi-platform builds (ODH vs RHOAI):** The same module operator binary
  can run with different image sets depending on the platform, because the
  images are injected externally.

### How it works

The platform operator process has all `RELATED_IMAGE_*` environment
variables set on its own Deployment (via `config/manager/manager.yaml`,
populated by CI from the release tracker). Each module handler declares
which variables its module operator needs. After rendering, the
`injectModuleEnv` pipeline action injects those variables into the module
operator's Deployment containers before Server-Side Apply.

```
Platform operator pod
  env:
    RELATED_IMAGE_ODH_TRAINER_IMAGE=quay.io/...@sha256:abc
    RELATED_IMAGE_ODH_TRAINING_CUDA128_TORCH29_PY312_IMAGE=quay.io/...@sha256:def
    ... (all module images)
        │
        │  provisionModules
        │  ├── handler.GetRelatedImages() → collect names
        │  └── store on rr.ModuleEnvInjection
        │
        │  helmrender / kustomizerender
        │  └── render Deployment, RBAC, etc. into rr.Resources
        │
        │  injectModuleEnv
        │  ├── find Deployments in rr.Resources
        │  ├── locate "manager" container (fallback: first container)
        │  ├── os.Getenv() for each RelatedImages name → resolve values
        │  └── append env vars to manager container
        │
        │  deploy (SSA apply)
        ▼
Module operator pod (e.g. trainer-operator)
  env:
    RELATED_IMAGE_ODH_TRAINER_IMAGE=quay.io/...@sha256:abc
    RELATED_IMAGE_ODH_TRAINING_CUDA128_TORCH29_PY312_IMAGE=quay.io/...@sha256:def
    APPLICATIONS_NAMESPACE=opendatahub
```

### Declaring related images in the handler

The `ModuleConfig` struct includes a `RelatedImages` field listing the
`RELATED_IMAGE_*` environment variable names the module operator needs.
`BaseHandler.GetRelatedImages()` returns this list; `provisionModules`
collects it and the `injectModuleEnv` pipeline action reads values from
the platform operator's process environment and injects them into each
module operator Deployment.

```go
func NewHandler() *handler {
    return &handler{
        BaseHandler: modules.BaseHandler{
            Config: modules.ModuleConfig{
                Name:        "trainer",
                CRName:      "default",
                ChartDir:    "trainer",
                ReleaseName: "trainer-operator",
                GVK: schema.GroupVersionKind{
                    Group:   "components.platform.opendatahub.io",
                    Version: "v1alpha1",
                    Kind:    "Trainer",
                },
                RelatedImages: []string{
                    "RELATED_IMAGE_ODH_TRAINER_IMAGE",
                    "RELATED_IMAGE_ODH_TRAINING_CUDA128_TORCH29_PY312_IMAGE",
                    "RELATED_IMAGE_ODH_TRAINING_ROCM64_TORCH29_PY312_IMAGE",
                    "RELATED_IMAGE_ODH_TH06_CUDA130_TORCH291_PY312_IMAGE",
                    "RELATED_IMAGE_ODH_TH06_ROCM64_TORCH291_PY312_IMAGE",
                    "RELATED_IMAGE_ODH_TH06_CPU_TORCH291_PY312_IMAGE",
                },
            },
        },
    }
}
```

The handler does not need to implement any injection logic. The
`BaseHandler.GetRelatedImages()` method returns the list, and the
platform's `injectModuleEnv` pipeline action handles the rest.

### Injection mechanics

When the DSC reconciler processes modules:

1. **`provisionModules`** iterates enabled handlers, calling
   `GetRelatedImages()` on each. It collects a deduplicated union of all
   `RELATED_IMAGE_*` names and stores them (along with
   `ApplicationsNamespace`) on `rr.ModuleEnvInjection`.
2. **`GetOperatorManifests()`** returns the Helm chart or Kustomize
   manifests for the module operator.
3. The **Helm and Kustomize render actions** produce the module
   operator's Kubernetes resources (Deployment, RBAC, etc.) into
   `rr.Resources`.
4. The **`injectModuleEnv` action** runs after rendering. It iterates
   `rr.Resources` looking for `apps/v1 Deployment` objects. For each, it
   finds the `manager` container (falling back to the first container)
   and appends `env` entries for every `RelatedImages` name whose
   `os.Getenv()` returns a non-empty value, plus `APPLICATIONS_NAMESPACE`.
5. **`deploy.NewAction`** SSA-applies the Deployment with the injected
   env vars. On subsequent reconciles the values are kept current -- if
   a platform upgrade changes an image reference, the module operator
   Deployment is updated automatically.

Variables with empty values (not set on the platform operator) are
**skipped**, not injected as empty strings. This allows the module
operator's own defaults to take effect when running in connected
environments or during local development.

### Module operator consumption

The module operator reads `RELATED_IMAGE_*` variables from its own
process environment using `os.Getenv()`, the same pattern the platform
operator uses today for in-tree component images. A typical pattern:

```go
func getControllerImage() string {
    if img := os.Getenv("RELATED_IMAGE_ODH_TRAINER_IMAGE"); img != "" {
        return img
    }
    return "quay.io/opendatahub/trainer:latest"
}
```

Module operators should always provide a fallback default for local
development and community (ODH) environments where `RELATED_IMAGE_*`
variables may not be set.

### Adding new images

When a module adds a new operand image:

1. **Module handler:** Add the new `RELATED_IMAGE_*` name to
   `ModuleConfig.RelatedImages`.
2. **Operands map:** Add a corresponding entry to
   `build/operands-map.yaml` with the `name`, `value` (digest-pinned
   reference), and `component` fields.
3. **Module operator:** Read the new variable via `os.Getenv()` with a
   sensible fallback.
4. **CI pipeline:** The release tracker and `apply-operator-images.sh`
   script automatically pick up new `RELATED_IMAGE_*` entries and add
   them to the platform operator's `config/manager/manager.yaml`.

### Image naming convention

All related image environment variables must follow the naming convention:

```
RELATED_IMAGE_<PRODUCT>_<COMPONENT>_IMAGE
```

Where:
- `<PRODUCT>` is `ODH` (community) or `OSE` (OpenShift product images).
- `<COMPONENT>` is an uppercase, underscore-separated identifier for the
  image (e.g., `TRAINER`, `TRAINING_CUDA128_TORCH29_PY312`,
  `KF_NOTEBOOK_CONTROLLER`).

Examples:
- `RELATED_IMAGE_ODH_TRAINER_IMAGE`
- `RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_OPERATOR_IMAGE`
- `RELATED_IMAGE_OSE_KUBE_RBAC_PROXY_IMAGE`

### Testing image injection

Handler unit tests should verify the `RelatedImages` list is complete:

```go
func TestRelatedImagesAreDeclared(t *testing.T) {
    g := NewWithT(t)
    h := NewHandler()

    // Verify the handler declares the images it needs
    images := h.Config.RelatedImages
    g.Expect(images).Should(ContainElements(
        "RELATED_IMAGE_ODH_TRAINER_IMAGE",
        "RELATED_IMAGE_ODH_TRAINING_CUDA128_TORCH29_PY312_IMAGE",
    ))
}

func TestGetRelatedImages(t *testing.T) {
    g := NewWithT(t)
    h := NewHandler()

    images := h.GetRelatedImages()
    g.Expect(images).Should(HaveLen(6))
    g.Expect(images).Should(ContainElement("RELATED_IMAGE_ODH_TRAINER_IMAGE"))
}
```

E2E tests should verify that the module operator Deployment on the
cluster contains the expected `RELATED_IMAGE_*` environment variables
after the platform reconciles.

---

## Utilities

### ParseConditions

The `modules.ParseConditions(u)` function extracts `[]metav1.Condition` from an
unstructured object's `.status.conditions` field, including all six standard
fields (`Type`, `Status`, `Reason`, `Message`, `ObservedGeneration`,
`LastTransitionTime`). It handles JSON number-to-int64 conversion for
`ObservedGeneration` and RFC3339 string parsing for `LastTransitionTime`. It is
used internally by `BaseHandler.GetModuleStatus` but is also exported for
custom status implementations.

### RegistrationOptions

When registering a handler, optional metadata can be provided for future
DAG-based ordering:

```go
mr.Add(handler, mr.WithRunlevel(2), mr.WithDependencies("other-module"))
```

These options are not enforced by the current implementation but will support
ordered provisioning in the future.

---

## Testing

Module teams must provide tests at three levels: handler unit tests (in this
repository), CRD schema validation tests (in this repository), and module
operator tests (in the module's own repository).

### Handler unit tests

Add unit tests in `internal/controller/modules/<name>/handler_test.go` that
cover:

1. **`IsEnabled`** -- returns `true` when the component/service is
   `Managed` in the DSC (for components) or DSCI (for services), `false`
   otherwise.
2. **`BuildModuleCR`** -- returns a well-formed unstructured object with the
   correct GVK, name, namespace, and `.spec` fields projected from
   `PlatformContext`.
3. **`GetOperatorManifests`** -- returns the expected chart or manifest
   descriptors (inherited from `BaseHandler`, but worth a sanity check).
4. **`GetRelatedImages`** -- returns the expected `RELATED_IMAGE_*` variable
   names (see [Testing image injection](#testing-image-injection) for examples).

Use `PlatformContext` directly in tests -- no real client or cluster needed:

```go
func TestBuildModuleCR(t *testing.T) {
    g := NewWithT(t)
    h := NewHandler()

    platform := &modules.PlatformContext{
        ApplicationsNamespace: "opendatahub",
        GatewayDomain:         "apps.cluster.example.com",
        Release:               common.Release{Name: "Open Data Hub"},
        DSC:                   testDSCWithMyModuleManaged(),
    }

    cr, err := h.BuildModuleCR(context.Background(), nil, platform)
    g.Expect(err).ShouldNot(HaveOccurred())
    g.Expect(cr.GetKind()).Should(Equal("MyModule"))
    g.Expect(cr.GetNamespace()).Should(Equal("opendatahub"))

    spec, _, _ := unstructured.NestedMap(cr.Object, "spec")
    g.Expect(spec["managementState"]).Should(Equal("Managed"))
}
```

For examples of registry and `ParseConditions` testing with mock handlers, see
`internal/controller/modules/registry_test.go`.

### CRD schema validation tests

Because `BuildModuleCR` returns `*unstructured.Unstructured`, there is no
compile-time guarantee the object matches the module CRD. Add a test that
validates the CR against the CRD's OpenAPI schema:

1. Load the module CRD from `opt/manifests/<name>/crd/` (or embed it as a
   test fixture).
2. Build the module CR via `BuildModuleCR` with a realistic `PlatformContext`.
3. Use `apiextensionsv1.CustomResourceValidation` to validate the CR object
   against the CRD schema.

This catches field name typos, missing required fields, and type mismatches
that would otherwise only surface at deploy time. It is the primary safety net
for the typed-to-unstructured boundary.

```go
func TestModuleCRMatchesCRDSchema(t *testing.T) {
    g := NewWithT(t)

    crdBytes, err := os.ReadFile("testdata/mymodule-crd.yaml")
    g.Expect(err).ShouldNot(HaveOccurred())

    crd := &apiextensionsv1.CustomResourceDefinition{}
    g.Expect(yaml.Unmarshal(crdBytes, crd)).Should(Succeed())

    cr, err := NewHandler().BuildModuleCR(ctx, nil, testPlatformContext())
    g.Expect(err).ShouldNot(HaveOccurred())

    errs := validateAgainstSchema(crd, cr)
    g.Expect(errs).Should(BeEmpty(), "CR does not match CRD schema: %v", errs)
}
```

This test should live alongside the handler unit tests and run as part of
`go test ./internal/controller/modules/<name>/...`.

### E2E tests (platform side)

The DSC controller e2e suite (`tests/e2e/`) follows a standard pattern for
component tests. When adding a new module, add a test file
`tests/e2e/<name>_test.go` that covers:

| Test case | What it verifies |
|---|---|
| **Module enabled** | DSC with module `Managed` -> module operator Deployment exists, module CR exists with expected `.spec` fields, `ModulesReady` condition is `True` |
| **Env var injection** | Module operator Deployment contains expected `RELATED_IMAGE_*` and `APPLICATIONS_NAMESPACE` env vars in the `manager` container |
| **Spec projection** | Changing DSC component stanza fields -> module CR `.spec` is updated on next reconcile |
| **Status aggregation** | Simulating `Ready=True` on module CR -> `ModulesReady=True`; simulating `Ready=False` -> `ModulesReady=False` |
| **Degraded propagation** | Setting `Degraded=True` on module CR -> `ModulesReady=False` with degraded message |
| **Staleness detection** | `observedGeneration` behind `metadata.generation` -> module treated as not-ready |
| **Module disabled** | DSC with module `Removed` -> module CR and operator resources are garbage collected |
| **Deletion recovery** | Deleting the module CRD -> platform re-creates it from manifests on next reconcile |

Use the existing `ComponentTestCtx` pattern and `jq` matchers for status
assertions. Tag tests with `Smoke` / `Tier1` as appropriate.

### Module operator tests (module team's responsibility)

The module's own repository should have its own test suite covering:

- **Controller reconciliation**: the module operator correctly reconciles its
  CR and deploys operands.
- **Status reporting**: the module operator sets `Ready`, `Degraded`,
  `ProvisioningSucceeded` conditions and `observedGeneration` correctly.
- **Platform field consumption**: the module operator correctly reads
  platform-projected fields from its CR `.spec` (namespace, gateway domain,
  management state).
- **Image resolution**: the module operator reads `RELATED_IMAGE_*` and
  `APPLICATIONS_NAMESPACE` environment variables from its own process
  environment and uses them to resolve operand images and discover the
  platform namespace.
- **Upgrade/downgrade**: operand resources are updated when the CR spec changes.

These are outside the scope of this repository but are critical for the
end-to-end contract to work. The
[onboarding guide](Onboarding%20Guide%20for%20ODH%20Operator%20Modules.md)
defines the status and API requirements the module operator must satisfy.

---

## Shared Library (`odh-platform-utilities`)

The [odh-platform-utilities](https://github.com/opendatahub-io/odh-platform-utilities)
repository provides shared rendering primitives (`ReconciliationRequest`,
`HelmChartInfo`, Helm/Kustomize/Template action adapters, resource helpers)
intended for **module operator teams** building their own controllers.

This modules package in the ODH operator is **platform-side orchestration
code** -- it is not a consumer of that library. The operator has its own
`ReconciliationRequest` (a superset with `Controller`, `Conditions`, `Release`)
and its own action pipeline (`deploy.NewAction`, `gc.NewAction`,
`helmrender.NewAction`, `injectModuleEnv`) that the modules package
integrates with.

Module teams building their own operators should use `odh-platform-utilities`
for manifest rendering and resource management. The platform operator does not
import it because the type systems serve different roles and forcing a
conversion layer adds complexity without benefit.

If the operator's rendering pipeline is eventually refactored to share a common
base type with the library, the modules package types are already structurally
aligned to make that migration straightforward.

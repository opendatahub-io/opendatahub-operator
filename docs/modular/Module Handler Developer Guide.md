# Module Handler Developer Guide

This guide walks through the implementation steps required to integrate a new
modular component into the ODH Operator using the `modules` package. It
complements the
[Onboarding Guide](Onboarding%20Guide%20for%20ODH%20Operator%20Modules.md),
which covers the broader architectural contract between the platform and module
operators.

## Prerequisites

Before starting, your module team must have:

1. A **module operator** (controller + manifests) in its own repository.
2. A **CRD** that follows the
   [API requirements](Onboarding%20Guide%20for%20ODH%20Operator%20Modules.md#2-api-requirements-crd)
   in the onboarding guide.
3. A **Helm chart** packaging the module operator's Deployment, RBAC, CRD, and
   ConfigMap. The chart must **not** include a CR instance; the platform creates
   the CR.

## Overview

Adding a module to the operator requires changes in four areas:

| Area | What you add | Where |
|---|---|---|
| **Chart source** | Entry in manifest-gathering script | `get_all_manifests.sh` |
| **Handler** | Go package implementing `ModuleHandler` | `internal/controller/modules/<name>/` |
| **DSC API** | Component stanza on `DataScienceCluster` | `api/datasciencecluster/v2/` |
| **Registration** | Import + map entry | `cmd/main.go` |

The following sections detail each area using a fictional "mymodule" module as
an example.

---

## Step 1: Provide the Helm Chart

The operator pulls module charts at image build time via
`get_all_manifests.sh`. Add entries to the `ODH_COMPONENT_CHARTS` (community)
and `RHOAI_COMPONENT_CHARTS` (product) maps:

```bash
# In ODH_COMPONENT_CHARTS
["mymodule"]="opendatahub-io:mymodule-operator:main@<commit-sha>:charts/operator"

# In RHOAI_COMPONENT_CHARTS
["mymodule"]="red-hat-data-services:mymodule-operator:rhoai-X.Y@<commit-sha>:charts/operator"
```

The chart will be extracted to `opt/manifests/mymodule/` inside the operator
image. This path is used by `BaseHandler.GetOperatorCharts()` via
`ModuleConfig.ChartDir`.

### What the chart should contain

- `Deployment` for the module controller
- `ServiceAccount`, `ClusterRole`, `ClusterRoleBinding` for RBAC
- The module's `CRD` (so the platform can create instances)
- Optional: `ConfigMap` for controller configuration

### What the chart must NOT contain

- A CR instance (e.g., `MyModule` kind). The platform operator creates and owns
  the CR via `BuildModuleCR`.

---

## Step 2: Implement the Handler

Create a new package under `internal/controller/modules/<name>/`. The handler
embeds `modules.BaseHandler` and only implements two methods: `IsEnabled` and
`BuildModuleCR`.

### File: `internal/controller/modules/mymodule/handler.go`

```go
package mymodule

import (
    "context"

    operatorv1 "github.com/openshift/api/operator/v1"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "sigs.k8s.io/controller-runtime/pkg/client"

    dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
    dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
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
                ReleaseName: "mymodule-operator",
                ChartDir:    "mymodule",
                GVK: schema.GroupVersionKind{
                    Group:   "components.platform.opendatahub.io",
                    Version: "v1alpha1",
                    Kind:    "MyModule",
                },
            },
        },
    }
}

func (h *handler) IsEnabled(dsc *dscv2.DataScienceCluster) bool {
    return dsc.Spec.Components.MyModule.ManagementState == operatorv1.Managed
}

func (h *handler) BuildModuleCR(
    ctx context.Context,
    cli client.Client,
    dsc *dscv2.DataScienceCluster,
    dsci *dsciv2.DSCInitialization,
) (*unstructured.Unstructured, error) {
    u := &unstructured.Unstructured{}
    u.SetGroupVersionKind(h.Config.GVK)
    u.SetName(h.Config.CRName)

    // Project platform-level fields from DSC/DSCI into the module CR.
    u.Object["spec"] = map[string]any{
        "managementState": string(dsc.Spec.Components.MyModule.ManagementState),
    }

    return u, nil
}
```

### What BaseHandler provides for free

By embedding `BaseHandler` with a populated `ModuleConfig`, you inherit working
implementations of four interface methods:

| Method | What it does |
|---|---|
| `GetName()` | Returns `Config.Name` |
| `GetGVK()` | Returns `Config.GVK` (used for dynamic watch registration) |
| `GetOperatorCharts()` | Builds `HelmChartInfo` from `Config.ChartDir`, `Config.ReleaseName`, and `Config.Values` |
| `GetModuleStatus()` | GETs the module CR by GVK + CRName and parses `.status.conditions` |

You only need to implement:

- **`IsEnabled`**: Read the DSC to decide if this module should be deployed.
- **`BuildModuleCR`**: Construct the module CR as an `unstructured.Unstructured`
  object, projecting platform fields from the DSC and DSCI.

### Overriding defaults

Any default method can be overridden by defining it on your handler struct. For
example, if the module needs custom status parsing:

```go
func (h *handler) GetModuleStatus(ctx context.Context, cli client.Client) ([]metav1.Condition, error) {
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

1. **`provisionModules`** iterates enabled handlers:
   - Appends the module's Helm chart info to `rr.HelmCharts`.
   - Calls `BuildModuleCR` and appends the result to `rr.Resources`.
2. **`helm.NewAction`** renders all Helm charts (component + module) into
   Kubernetes resources and appends them to `rr.Resources`.
3. **`deploy.NewAction`** applies everything in `rr.Resources` via
   Server-Side Apply.
4. **`updateModuleStatus`** reads each module CR's `.status.conditions` and
   aggregates them into the DSC's `ModulesReady` condition.
5. **`gc.NewAction`** deletes resources that were previously managed but are no
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

## Utilities

### ParseConditions

The `modules.ParseConditions(u)` function extracts `[]metav1.Condition` from an
unstructured object's `.status.conditions` field. It is used internally by
`BaseHandler.GetModuleStatus` but is also exported for custom status
implementations.

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

### Unit tests

Add unit tests in the handler package. The `BaseHandler` provides testable
defaults. For examples of testing with mock handlers, see
`internal/controller/modules/registry_test.go`.

### Integration tests

The existing DSC controller e2e tests cover the reconciliation pipeline. When
adding a new module, consider adding a test case that:

1. Creates a DSC with the module enabled.
2. Verifies the module operator Deployment exists.
3. Verifies the module CR exists with the expected `.spec` fields.
4. Simulates a status update on the module CR and verifies the DSC
   `ModulesReady` condition reflects it.

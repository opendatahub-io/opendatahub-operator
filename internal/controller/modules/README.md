# Module Orchestrator

This package implements the platform-side orchestration for modular components.
It provides the `ModuleHandler` interface, a `BaseHandler` with shared helpers,
a registry for handler lifecycle, and watch infrastructure for status
aggregation.

For the full architectural context, see
[docs/modular/Onboarding Guide for ODH Operator Modules.md](../../../docs/modular/Onboarding%20Guide%20for%20ODH%20Operator%20Modules.md).

## Architecture

The modular orchestrator manages **out-of-tree module operators** alongside the
existing in-tree components. The DSC controller's action pipeline handles both:

```
DSC Reconcile
  -> provisionComponents    (component CRs -> rr.Resources)
  -> provisionModules       (module operator charts -> rr.HelmCharts,
                             module CRs -> rr.Resources)
  -> helm.NewAction         (renders charts into rr.Resources)
  -> deploy.NewAction       (SSA-applies everything in rr.Resources)
  -> updateModuleStatus     (reads module CR status -> DSC conditions)
  -> gc.NewAction           (deletes resources missing from rr.Resources)
```

Module CRs follow the same lifecycle as component CRs: they are added to
`rr.Resources` when enabled and removed by the GC action when disabled.

## Adding a New Module

A module team contributes four things to this repository:

### 1. Chart source entry (`get_all_manifests.sh`)

Add the module's chart to the `ODH_COMPONENT_CHARTS` and
`RHOAI_COMPONENT_CHARTS` maps:

```bash
# ODH_COMPONENT_CHARTS
["mymodule"]="opendatahub-io:mymodule-operator:main@<commit-sha>:charts/operator"

# RHOAI_COMPONENT_CHARTS
["mymodule"]="red-hat-data-services:mymodule-operator:rhoai-X.Y@<commit-sha>:charts/operator"
```

The chart must contain the module operator's Deployment, RBAC, CRD, and
ConfigMap. It must **not** contain a CR instance; the platform creates the CR.

### 2. Handler implementation (`internal/controller/modules/<name>/handler.go`)

Embed `BaseHandler` and implement only `IsEnabled` and `BuildModuleCR`:

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

    u.Object["spec"] = map[string]any{
        "managementState": string(dsc.Spec.Components.MyModule.ManagementState),
        // Add other platform field projections here.
    }

    return u, nil
}
```

`BaseHandler` provides default implementations for the remaining four
interface methods:

| Method | Default behaviour |
|---|---|
| `GetName()` | Returns `Config.Name` |
| `GetGVK()` | Returns `Config.GVK` |
| `GetOperatorCharts()` | Builds `HelmChartInfo` from `Config.ChartDir`, `Config.ReleaseName`, `Config.Values` |
| `GetModuleStatus()` | GETs the module CR by `Config.GVK` + `Config.CRName` and parses `.status.conditions` |

### 3. DSC API stanza (`api/datasciencecluster/v2/datasciencecluster_types.go`)

Add a field to the `Components` struct so users can enable/configure the module
through the `DataScienceCluster` CR:

```go
// MyModule component configuration.
MyModule DSCMyModule `json:"mymodule,omitempty"`
```

Define the corresponding types (typically in a new file under
`api/components/v1alpha1/`):

```go
type DSCMyModule struct {
    common.ManagementSpec `json:",inline"`
    MyModuleCommonSpec    `json:",inline"`
}

type MyModuleCommonSpec struct {
    // Module-specific fields exposed in the DSC.
}
```

After modifying the API types, run `make generate` and `make manifests` to
regenerate deepcopy functions and CRD manifests.

### 4. Registration (`cmd/main.go`)

Import the handler package and add it to the `existingModules` map:

```go
import mymodule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/mymodule"

// In the var block:
existingModules = map[string]mr.ModuleHandler{
    "mymodule": mymodule.NewHandler(),
}
```

## Package Reference

### `types.go` -- ModuleHandler interface

The 6-method contract between the platform and each module handler:

- `GetName()` -- unique identifier (registry key, log messages)
- `IsEnabled(dsc)` -- reads DSC spec to determine enablement
- `GetGVK()` -- module CR's GroupVersionKind (used for watch registration)
- `GetOperatorCharts()` -- Helm chart descriptors for the module operator
- `BuildModuleCR(ctx, cli, dsc, dsci)` -- constructs the module CR with
  platform fields projected from the DSC/DSCI
- `GetModuleStatus(ctx, cli)` -- reads module CR status conditions

### `base.go` -- BaseHandler and ModuleConfig

`ModuleConfig` holds static metadata (name, GVK, chart info). `BaseHandler`
provides default implementations for `GetName`, `GetGVK`, `GetOperatorCharts`,
and `GetModuleStatus`. Module teams embed `BaseHandler` and only implement
`IsEnabled` and `BuildModuleCR`.

`ParseConditions(u)` is a shared utility that extracts `[]metav1.Condition`
from an unstructured object's `.status.conditions` field.

### `registry.go` -- Module registry

A singleton registry that stores `ModuleHandler` instances. Handlers are
registered at program startup in `cmd/main.go`. The registry supports:

- `Add(handler, ...RegistrationOption)` -- register a handler
- `Enable(name)` / `Disable(name)` -- CLI suppression flag integration
- `ForEach(fn)` -- iterate enabled handlers (used by `provisionModules`)
- `HasEntries()` -- check if any modules are registered
- `RegistrationOption` -- `WithRunlevel(int)` and `WithDependencies(...string)`
  for future DAG-based ordering

### `watch.go` -- Dynamic watch registration

`SetupModuleWatches(ctx, mgr, controller)` registers a watch for each
module's CR GVK after the DSC controller is built. When a module operator
updates its CR status, the watch maps the event to a DSC reconcile request
so the platform can aggregate the updated status.

## Suppression Flags

Module handlers can be disabled at startup via CLI flags. The flags package
(`pkg/utils/flags/suppression.go`) provides:

- `RegisterModuleSuppressionFlags(names)` -- registers `--disable-<name>-module` flags
- `IsModuleEnabled(name)` -- checks if the flag is set

These integrate with the registry's `Enable`/`Disable` methods in
`cmd/main.go`'s `registerModules()` function.

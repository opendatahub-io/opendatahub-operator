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
  -> provisionModules       (module operator manifests -> rr.HelmCharts
                             and/or rr.Manifests; module CRs -> rr.Resources)
  -> helm.NewAction         (renders Helm charts into rr.Resources)
  -> kustomize.NewAction    (renders Kustomize manifests into rr.Resources)
  -> deploy.NewAction       (SSA-applies everything in rr.Resources)
  -> updateModuleStatus     (reads module CR status -> DSC conditions)
  -> gc.NewAction           (deletes resources missing from rr.Resources)
```

Module CRs follow the same lifecycle as component CRs: they are added to
`rr.Resources` when enabled and removed by the GC action when disabled.

`deploy.NewAction` automatically sets `platform.opendatahub.io/part-of` labels
and platform annotations on all resources in `rr.Resources`, including module
CRs and module operator resources.

`updateModuleStatus` performs staleness detection (comparing
`status.observedGeneration` against `metadata.generation`) and propagates
`Degraded` status. If all modules are `Ready` but some report `Degraded=True`,
`ModulesReady` is set to `False` with a message listing the degraded modules.

## Adding a New Module

A module team contributes four things to this repository:

### 1. Manifest source entry (`get_all_manifests.sh`)

Add the module's manifests (Helm chart **or** Kustomize overlays) to the
`ODH_COMPONENT_CHARTS` and `RHOAI_COMPONENT_CHARTS` maps:

```bash
# ODH_COMPONENT_CHARTS
["mymodule"]="opendatahub-io:mymodule-operator:main@<commit-sha>:charts/operator"

# RHOAI_COMPONENT_CHARTS
["mymodule"]="red-hat-data-services:mymodule-operator:rhoai-X.Y@<commit-sha>:charts/operator"
```

The manifests must contain the module operator's Deployment, RBAC, CRD, and
ConfigMap. They must **not** contain a CR instance; the platform creates the CR.

### 2. Handler implementation (`internal/controller/modules/<name>/handler.go`)

Embed `BaseHandler` and implement only `IsEnabled` and `BuildModuleCR`.
Set **either** `ChartDir` (Helm) or `ManifestDir` (Kustomize) in `ModuleConfig`
to select the manifest format.

**Helm example:**

```go
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
            },
        },
    }
}
```

**Kustomize example:**

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
            },
        },
    }
}
```

Both variants still require `IsEnabled` and `BuildModuleCR` -- see the
[Developer Guide](../../../docs/modular/Module%20Handler%20Developer%20Guide.md)
for the full handler code.

`BaseHandler` provides default implementations for the remaining four
interface methods:

| Method | Default behaviour |
|---|---|
| `GetName()` | Returns `Config.Name` |
| `GetGVK()` | Returns `Config.GVK` |
| `GetOperatorManifests()` | Returns `OperatorManifests` with `HelmCharts` (if `ChartDir` set) and/or `Manifests` (if `ManifestDir` set) |
| `GetModuleStatus()` | GETs the module CR by `Config.GVK` + `Config.CRName`, parses `.status.conditions` and `.status.observedGeneration`, returns a `*ModuleStatus` |

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
- `GetOperatorManifests()` -- returns `OperatorManifests` with Helm charts
  and/or Kustomize manifests for the module operator
- `BuildModuleCR(ctx, cli, dsc, dsci)` -- constructs the module CR with
  platform fields projected from the DSC/DSCI
- `GetModuleStatus(ctx, cli)` -- returns `*ModuleStatus` with conditions and
  generation metadata for staleness detection

### `base.go` -- BaseHandler and ModuleConfig

`ModuleConfig` holds static metadata (name, GVK, manifest info). Set `ChartDir`
for Helm or `ManifestDir` for Kustomize (or both). `BaseHandler` provides
default implementations for `GetName`, `GetGVK`, `GetOperatorManifests`, and
`GetModuleStatus`. Module teams embed `BaseHandler` and only implement
`IsEnabled` and `BuildModuleCR`.

`ModuleStatus` bundles parsed conditions with generation metadata
(`ObservedGeneration`, `Generation`) for staleness detection.

`ParseConditions(u)` is a shared utility that extracts `[]metav1.Condition`
from an unstructured object's `.status.conditions` field, including
`ObservedGeneration` and `LastTransitionTime`.

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

## Relationship to `odh-platform-utilities`

The [odh-platform-utilities](https://github.com/opendatahub-io/odh-platform-utilities)
library provides shared rendering primitives for **module operator teams**
(Helm/Kustomize/Template actions, `ReconciliationRequest`, resource helpers).

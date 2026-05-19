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
  -> provisionComponents       (component CRs -> rr.Resources)
  -> cleanupDisabledModules    (two-phase cleanup of disabled module resources)
  -> provisionModules          (module operator manifests -> rr.HelmCharts
                                and/or rr.Manifests; module CRs -> rr.Resources)
  -> helm.NewAction            (renders Helm charts into rr.Resources)
  -> kustomize.NewAction       (renders Kustomize manifests into rr.Resources)
  -> deploy.NewAction          (SSA-applies everything in rr.Resources)
  -> updateModuleStatus        (reads module CR status -> DSC conditions)
  -> gc.NewAction              (deletes resources missing from rr.Resources)
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

### Module CR ownership and cleanup

`SetupModuleWatches` registers each module's CR GVK as an **owned type** on
the DSC controller via `AddOwnedType`. This ensures:

- `deploy.NewAction` sets the DSC as controller owner of module CRs
- `gc.NewAction` deletes module CRs when they are missing from `rr.Resources`
  (i.e., when the module is disabled)

Module **operator** resources (Deployment, RBAC, etc.) are generic Kubernetes
types that are NOT registered as owned types. They are cleaned up by
`cleanupDisabledModules`, which implements a two-phase approach:

1. **Phase 1**: Module is disabled, CR still exists. GC deletes the CR. The
   module operator Deployment is left running so it can process finalizers
   and Kubernetes can cascade-delete ownerRef'd operands.
2. **Phase 2**: On the next reconcile, the CR is confirmed gone. The action
   renders the module's Helm chart and deletes each operator resource.

### Component-to-module migration

Components already use `components.platform.opendatahub.io` -- the GVK stays
the same when migrating to a module. Migration is a **reconciler handoff**:
the in-tree reconciler stops and the module operator starts reconciling the
same CR. No owner-ref stripping or old-CR deletion is needed. See the
[Component to Module Migration Guide](../../../docs/modular/Component%20to%20Module%20Migration%20Guide.md)
for the full process.

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

Both variants still require `IsEnabled` and `BuildModuleCR`. `BuildModuleCR`
receives a `*PlatformContext` containing all platform-level fields -- see the
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

### `types.go` -- ModuleHandler interface and PlatformContext

The 8-method contract between the platform and each module handler:

- `GetName()` -- unique identifier (registry key, log messages)
- `IsEnabled(platform)` -- reads DSC/DSCI to determine enablement
- `GetGVK()` -- module CR's GroupVersionKind (used for watch and ownership
  registration)
- `GetOperatorManifests(platform)` -- returns `OperatorManifests` with Helm
  charts and/or Kustomize manifests for the module operator
- `BuildModuleCR(ctx, cli, platform)` -- constructs the module CR with
  platform fields projected from `*PlatformContext`
- `GetRelatedImages()` -- returns `RELATED_IMAGE_*` env var names
- `GetModuleStatus(ctx, cli)` -- returns `*ModuleStatus` with conditions and
  generation metadata for staleness detection
- `ModuleCRExists(ctx, cli)` -- checks if the module CR exists on the cluster
  (returns false when the CRD is absent)
- `DeleteOperatorResources(ctx, cli, platform)` -- renders the module's Helm
  chart and deletes each resource from the cluster (for two-phase cleanup)

`OwnedTypeRegistrar` is a single-method interface (`AddOwnedType(gvk)`) used
by `SetupModuleWatches` to register module CR GVKs as owned types on the DSC
controller without importing the reconciler package directly.

`PlatformContext` is built once per reconcile in `provisionModules` and passed
to every handler's `BuildModuleCR`. It exposes:

| Field | Source | Description |
|---|---|---|
| `ApplicationsNamespace` | `DSCI.Spec.ApplicationsNamespace` | Namespace where module operands deploy |
| `GatewayDomain` | `GatewayConfig.Status.Domain` | Cluster ingress domain (empty if not yet provisioned) |
| `Release` | `rr.Release` | Platform identity (ODH/RHOAI) and version |
| `DSC` | reconcile instance | The `DataScienceCluster` instance for reading module-specific component stanzas |

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

`ModuleCRExists` GETs the module CR by GVK + CRName and returns `true` if
found, `false` if not found or if the CRD does not exist.

`DeleteOperatorResources` renders the module's Helm chart via
`GetOperatorManifests`, then deletes each rendered resource from the cluster.
NotFound errors are silently ignored for idempotency.

### `registry.go` -- Module registry

A singleton registry that stores `ModuleHandler` instances. Handlers are
registered at program startup in `cmd/main.go`. The registry supports:

- `Add(handler, ...RegistrationOption)` -- register a handler
- `Enable(name)` / `Disable(name)` -- CLI suppression flag integration
- `ForEach(fn)` -- iterate enabled handlers (used by `provisionModules`)
- `HasEntries()` -- check if any modules are registered
- `RegistrationOption` -- `WithRunlevel(int)` and `WithDependencies(...string)`
  for future DAG-based ordering

### `watch.go` -- Dynamic watch and ownership registration

`SetupModuleWatches(ctx, mgr, controller, owner)` registers a watch for each
module's CR GVK after the DSC controller is built, and calls
`owner.AddOwnedType(gvk)` to register the GVK as an owned type. This ensures
`deploy.NewAction` sets DSC as controller owner and `gc.NewAction` can delete
module CRs when disabled. All registered modules (including CLI-disabled ones)
are processed so cleanup paths work correctly.

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

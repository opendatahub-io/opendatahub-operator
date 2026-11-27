# Module Orchestrator

This package implements the platform-side orchestration for modular components.
It provides the `ModuleHandler` interface, a `BaseHandler` with shared helpers,
a registry for handler lifecycle, and watch infrastructure for status
aggregation. PlatformModule reconciliation actions live in the sibling
`internal/controller/platformmodules` package.

For the full architectural context, see
[docs/modular/Onboarding Guide for ODH Operator Modules.md](../../../docs/modular/Onboarding%20Guide%20for%20ODH%20Operator%20Modules.md).

## Architecture

The modular orchestrator manages **out-of-tree module operators** alongside the
existing in-tree components. The Platform controller coordinates the unified
DAG and creates one tracker CR per managed module; the PlatformModule controller
owns the corresponding operator lifecycle:

```text
Platform Reconcile
  -> upgrade-gate preflight (isolated renders per managed module)
  -> cleanupDisabledPlatformModules
  -> provisionPlatformModules  (tracker CRs -> rr.Resources)
  -> deploy.NewAction          (SSA-applies tracker CRs)
  -> coordinateDAG             (opens ready DAG runlevels)
  -> aggregatePlatformModulesStatus

PlatformModule Reconcile
  -> checkPlatformModuleRunlevelGate
  -> provisionModuleOperator   (operator manifests -> rr.HelmCharts/rr.Manifests)
  -> render, upgrade-gate extraction/check, env/config injection
  -> deploy.NewAction          (SSA-applies operator resources)
  -> updatePlatformModuleStatus (live rollout checks, stale cleanup, inventory/release commit)
```

Platform gate preflight renders managed module sources into a temporary request,
extracts chart gates, merges global gates, and checks acknowledgments before
tracker apply. A broken render is reported and retried per module while healthy
modules continue within DAG ordering. Discovered unacknowledged gates still
block provisioning. PlatformModule extracts and checks gates again before apply
to cover modules whose preflight render failed. Gate ConfigMaps are not deployed.

On OpenShift, DSC/DSCI own and delete module CRs. On xKS, users or GitOps own
that lifecycle; disabling a module waits for their CR deletion. A PlatformModule
tracker CR is the deploy signal for its module operator and its finalizer
performs two-phase operator cleanup when the tracker is removed.

Module registry enablement is static: startup suppression flags disable
registry entries and unified DAG nodes. Platform `spec.modules` is runtime
desired state. Effective module deployment requires both registry enablement
and handler `IsEnabled` result; Platform reconciliation does not mutate the
global registry.

`deploy.NewAction` automatically sets `platform.opendatahub.io/part-of` labels
and platform annotations on resources in `rr.Resources`.

The DSC controller's `ComputeModulesStatusDetailed` action performs staleness
detection (comparing `status.observedGeneration` against
`metadata.generation`) and propagates module conditions. The Platform
controller aggregates the Ready conditions reported by PlatformModule tracker
CRs into `ModulesReady`.

### Ownership and cleanup

Ownership is split between two controllers:

- On OpenShift, DSC/DSCI controllers build and apply module CRs. Module CRs remain
  controller-owned by DSC/DSCI. PlatformModule watches their status and
  publishes tracker status; Platform watches tracker status and does not claim
  module CR GVKs through `OwnsGVK`.
- Platform controller owns `PlatformModule` tracker CRs.
- Each PlatformModule controller owns its module operator resources
  (Deployments, RBAC, ConfigMaps, and similar objects).

CRDs are an exception — the deploy action routes CRDs to a dedicated
`deployCRD()` path that never sets owner references (CRDs are cluster-scoped
singletons that may be shared).

`status.resources` is the inventory for stale cleanup and finalization. Before
deploy, the controller persists the union of the recorded and rendered inventory.
The final action deletes stale resources in reverse order and waits for deletion.
After cleanup succeeds, it publishes the desired inventory and release together.
An interrupted deploy or pending cleanup preserves the inventory and previous
release. Deployment rollout is reported separately. Cleanup checks controller
ownership through the cache and uses UID and resourceVersion preconditions.
CRDs, Namespaces, Leases, and unmanaged objects survive with matching owner
references detached. NotFound and NoMatch are harmless.
Generic GC is not used by PlatformModule.

Module-CR readiness is separate from the operator release commit: missing xKS
module CRs do not block commits, and later runtime failures do not clear them.

PlatformModule finalizer implements two-phase cleanup for explicitly disabled
modules:

Platform explicitly deletes owned trackers and waits for their finalizers; no
generic GC competes with tracker lifecycle. Pending removal is included in
Platform readiness, with waiting/failure details persisted on the tracker.

1. **Phase 1**: The tracker is deleted, but the module CR still exists. The
   finalizer keeps the operator resources alive so it can process finalizers.
2. **Phase 2**: On later reconcile, module CR is confirmed gone. Finalizer
   safely deletes tracked removable resources and waits until they are gone.
   Missing handler keeps finalizer
   because module-CR absence cannot be established.

### Readiness contracts

DAG ordering uses tracker release as authoritative handshake. Non-empty
`status.release.version` is ready when it matches target platform version,
even if tracker `Ready` later becomes false. New trackers have no release until
their first deployment and cleanup complete; an empty release is not ready.
The PlatformModule controller writes this release; module-CR release reporting
is separate.

Platform `ModulesReady` is stricter: every managed tracker must report
`Ready=True`; its committed release must also match current platform
release. A healthy tracker on an older release reports an upgrade-pending message.

### Component-to-module migration

Components already use `components.platform.opendatahub.io` -- the GVK stays
the same when migrating to a module. Migration is a **reconciler handoff**:
the in-tree reconciler stops and the module operator starts reconciling the
same CR. No owner-ref stripping or old-CR deletion is needed. See the
[Component to Module Migration Guide](../../../docs/modular/Component%20to%20Module%20Migration%20Guide.md)
for the full process.

## Current modules

Reference implementations under `internal/controller/modules/`:

| Module | Handler | Manifest format | Runlevel |
|--------|---------|-----------------|----------|
| [AI Gateway](./aigateway/) | `aigateway.NewHandler()` | Kustomize (`SourcePathByPlatform`) | `dag.RL(32)` |
| [Spark Operator](./sparkoperator/) | `sparkoperator.NewHandler()` | Kustomize (`SourcePath: default`) | `dag.RL(32)` |

Both are registered in `cmd/main.go` (`existingModules` / `moduleRunlevels`).
They check `DSC.Spec.Components.<Name>.ManagementState`, project
`*CommonSpec` into the module CR via `BuildModuleCR`, and rely on the modules
controller for deploy + dynamic ownership (no static `.Owns()` on the DSC
controller).

## Adding a New Module

A module team contributes four things to this repository:

### 1. Manifest source entry (`manifests-config.yaml`)

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

Embed `BaseHandler` and implement `PopulatePlatformModule`, `IsEnabled`, and
`BuildModuleCR`.
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

Both variants still require `PopulatePlatformModule`, `IsEnabled`, and
`BuildModuleCR`. `BuildModuleCR` receives `*DSCContext` plus
`*ModuleCRConfig` containing platform-level fields -- see the
[Developer Guide](../../../docs/modular/Module%20Handler%20Developer%20Guide.md)
for the full handler code.

`BaseHandler` provides default implementations for remaining lifecycle
interface methods:

| Method | Default behaviour |
|---|---|
| `GetName()` | Returns `Config.Name` |
| `GetGVK()` | Returns `Config.GVK` |
| `GetOperatorManifests()` | Returns `OperatorManifests` with `HelmCharts` (if `ChartDir` set) and/or `Manifests` (if `ManifestDir` set) |
| `GetModuleStatus()` | GETs the module CR by `Config.GVK` + `Config.CRName`, parses `.status.conditions` and `.status.observedGeneration`, returns a `*ModuleStatus` |
| `GetModuleCRState()` | Reports absent, alive, or deleting module CR state |
| `DeleteModuleCR()` | Idempotently deletes module CR |
| `WriteDSCComponentStatus()` | Projects management state and releases into typed DSC status |

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

### `types.go` -- ModuleHandler interface and contexts

Current `ModuleHandler` contract:

- `GetName()` -- unique identifier (registry key, log messages)
- `IsEnabled(modules)` -- reads projected Platform module state
- `GetGVK()` -- module CR's GroupVersionKind (used for watch and ownership
  registration)
- `GetOperatorManifests(platform)` -- returns `OperatorManifests` with Helm
  charts and/or Kustomize manifests for the module operator
- `PopulatePlatformModule(pm, dscCtx)` -- projects DSC/DSCI management state
- `BuildModuleCR(ctx, cli, dscCtx, cfg)` -- constructs module CR from typed
  DSC/DSCI context plus platform config
- `GetRelatedImages()` -- returns `RELATED_IMAGE_*` env var names
- `GetModuleStatus(ctx, cli)` -- returns `*ModuleStatus` with conditions and
  generation metadata for staleness detection
- `GetModuleCRState(ctx, cli)` -- reports absent/alive/deleting
- `DeleteModuleCR(ctx, cli)` -- idempotently deletes module CR

- `WriteDSCComponentStatus(dsc, enabled, releases)` -- projects typed DSC status

`PlatformContext` is built once per PlatformModule reconcile and passed to
`GetOperatorManifests`. `BuildModuleCR` instead uses `DSCContext` and
`ModuleCRConfig`. PlatformContext exposes:

| Field | Source | Description |
|---|---|---|
| `ApplicationsNamespace` | `DSCI.Spec.ApplicationsNamespace` | Namespace where module operands deploy |
| `MonitoringNamespace` | `DSCI.Spec.Monitoring.Namespace` | Namespace for the monitoring stack (empty when monitoring is unset or in xKS mode with no DSCI) |
| `Release` | `rr.Release` | Platform identity (ODH/RHOAI) and version |

#### Injected environment variables (platform → module operator)

After rendering and before deploy, the `injectModuleEnv` action writes these env
vars onto each module operator's Deployment container (default `manager`). Only
non-empty values are injected. **The names are a contract**: a module operator
that needs these values must read them by these exact, unprefixed names.

| Env var | Source | Notes |
|---|---|---|
| `APPLICATIONS_NAMESPACE` | `PlatformContext.ApplicationsNamespace` | Always injected when set |
| `MONITORING_NAMESPACE` | `PlatformContext.MonitoringNamespace` | Skipped when empty (e.g. monitoring unset, xKS) |
| `RELATED_IMAGE_*` | operator env of the same name | Scoped per module via `GetRelatedImages()` |

Note: the operator's own config env (`ODH_MANAGER_DSC_MONITORING_NAMESPACE`, bound
via viper) is a separate input mechanism and is unrelated to these injected names.

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

`GetModuleCRState` GETs the module CR by GVK + CRName and reports absent,
alive, or deleting. Missing CRDs are treated as absent.

Operator cleanup uses the PlatformModule's committed inventory and live owner
UID verification; handlers do not re-render manifests to delete resources.

### `registry.go` -- Module registry

A singleton registry that stores `ModuleHandler` instances. Handlers are
registered at program startup in `cmd/main.go`. The registry supports:

- `Add(handler, ...RegistrationOption)` -- register a handler
- `Disable(name)` -- startup CLI suppression flag integration
- `ForAll(fn)` -- iterate handlers with static registry-enabled state
- `HasEntries()` -- check if any modules are registered
- `RegistrationOption` -- retained only for source compatibility; DAG ordering
  comes from the unified `pkg/controller/provision` registry

### Module CR watches and ownership

PlatformModule controller registers status watches for every module CR GVK;
Platform controller watches tracker status. Module CRDs created after startup
trigger a tracker reconcile so dynamic watches can be registered.
Those watches enqueue the owning DSC/DSCI reconciliation when module CR status
changes, but do not claim module CRs through `OwnsGVK`. Module CRs remain
DSC/DSCI-owned. The Platform controller watches PlatformModule tracker status;
the PlatformModule controller owns only rendered operator resources recorded
in its tracker inventory.

## Suppression Flags

Module handlers can be disabled at startup via CLI flags. The flags package
(`pkg/utils/flags/suppression.go`) provides:

- `RegisterModuleSuppressionFlags(names)` -- registers `--disable-<name>-module` flags
- `IsModuleEnabled(name)` -- checks if the flag is set (used while registering
  static suppression state)

These integrate with registry `Disable` and unified-DAG `provision.Disable`
calls in `cmd/main.go`'s `registerModules()` function. Runtime Platform module
state does not mutate either registry.

## Relationship to `odh-platform-utilities`

The [odh-platform-utilities](https://github.com/opendatahub-io/odh-platform-utilities)
library provides shared rendering primitives for **module operator teams**
(Helm/Kustomize/Template actions, `ReconciliationRequest`, resource helpers).

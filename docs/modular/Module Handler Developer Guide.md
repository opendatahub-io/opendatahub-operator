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
3. A **Helm chart** or **Kustomize overlays** packaging the module operator's
   Deployment, RBAC, CRD, and ConfigMap. The manifests must **not** include a
   CR instance; the platform creates the CR.

## Overview

Adding a module to the operator requires changes in four areas:

| Area | What you add | Where |
|---|---|---|
| **Manifest source** | Entry in manifest-gathering script | `get_all_manifests.sh` |
| **Handler** | Go package implementing `ModuleHandler` | `internal/controller/modules/<name>/` |
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

    dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
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
            },
        },
    }
}
```

The `IsEnabled` and `BuildModuleCR` methods are identical regardless of
manifest format.

### PlatformContext -- available platform fields

`BuildModuleCR` receives a `*modules.PlatformContext` that is built once per
reconcile and contains all platform-level fields a handler may need:

| Field | Type | Source | Description |
|---|---|---|---|
| `ApplicationsNamespace` | `string` | `DSCI.Spec.ApplicationsNamespace` | Namespace where module operands deploy. All modules need this. |
| `GatewayDomain` | `string` | `GatewayConfig.Status.Domain` | Cluster ingress domain. Empty if the gateway is not yet provisioned; handlers needing it should check for empty and handle gracefully. |
| `Release` | `common.Release` | `rr.Release` | Platform identity (ODH vs RHOAI) and version. Useful for conditional behaviour. |
| `DSC` | `*dscv2.DataScienceCluster` | reconcile instance | The DSC instance. Handlers read their module-specific component stanza (e.g., `platform.DSC.Spec.Components.MyModule`). |

The struct intentionally omits the raw `DSCI` -- the only field modules need
from it is `ApplicationsNamespace`, which is already extracted. This keeps the
handler contract explicit and prevents handlers from reaching into fields they
don't own.

### What BaseHandler provides for free

By embedding `BaseHandler` with a populated `ModuleConfig`, you inherit working
implementations of four interface methods:

| Method | What it does |
|---|---|
| `GetName()` | Returns `Config.Name` |
| `GetGVK()` | Returns `Config.GVK` (used for dynamic watch registration) |
| `GetOperatorManifests()` | Returns `OperatorManifests` with `HelmCharts` (if `ChartDir` set) and/or `Manifests` (if `ManifestDir` set) |
| `GetModuleStatus()` | GETs the module CR by GVK + CRName, parses `.status.conditions` and `.status.observedGeneration`, returns `*ModuleStatus` |

You only need to implement:

- **`IsEnabled`**: Read the DSC to decide if this module should be deployed.
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
   `ApplicationsNamespace`, `GatewayDomain`, `Release`, and the DSC instance),
   then iterates enabled handlers:
   - Appends the module's manifest descriptors to `rr.HelmCharts` and/or
     `rr.Manifests` (depending on the handler's `ModuleConfig`).
   - Calls `BuildModuleCR(&platformCtx)` and appends the result to `rr.Resources`.
2. **`helm.NewAction`** renders Helm charts into Kubernetes resources and
   appends them to `rr.Resources`.
3. **`kustomize.NewAction`** renders Kustomize manifests into Kubernetes
   resources and appends them to `rr.Resources`.
4. **`deploy.NewAction`** applies everything in `rr.Resources` via
   Server-Side Apply. It automatically sets `platform.opendatahub.io/part-of`
   labels and platform annotations on all resources, including module CRs.
5. **`updateModuleStatus`** reads each module CR's status and aggregates it
   into the DSC's `ModulesReady` condition. It performs:
   - **Staleness detection**: if `status.observedGeneration` is behind
     `metadata.generation`, the module is treated as not-ready.
   - **Ready check**: the `Ready` condition must be `True`.
   - **Degraded propagation**: if `Ready=True` but `Degraded=True`, the
     module is reported as degraded (`ModulesReady` is set to `False`).
6. **`gc.NewAction`** deletes resources that were previously managed but are no
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

1. **`IsEnabled`** -- returns `true` when the DSC component stanza is
   `Managed`, `false` otherwise.
2. **`BuildModuleCR`** -- returns a well-formed unstructured object with the
   correct GVK, name, namespace, and `.spec` fields projected from
   `PlatformContext`.
3. **`GetOperatorManifests`** -- returns the expected chart or manifest
   descriptors (inherited from `BaseHandler`, but worth a sanity check).

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
`helmrender.NewAction`) that the modules package integrates with.

Module teams building their own operators should use `odh-platform-utilities`
for manifest rendering and resource management. The platform operator does not
import it because the type systems serve different roles and forcing a
conversion layer adds complexity without benefit.

If the operator's rendering pipeline is eventually refactored to share a common
base type with the library, the modules package types are already structurally
aligned to make that migration straightforward.

# Component to Module Migration Guide

This guide describes how an existing in-tree component migrates to a modular
architecture where it becomes an out-of-tree module operator managed by the ODH
Operator as a platform orchestrator.

For the module contract and handler implementation details, see the
[Module Handler Developer Guide](Module%20Handler%20Developer%20Guide.md) and
the
[Onboarding Guide](Onboarding%20Guide%20for%20ODH%20Operator%20Modules.md).

---

## Why migration is non-trivial

When a component moves from the `existingComponents` registry to
`existingModules`, two problems arise:

1. **Ownership cascade.** The old component CR has `ownerReferences` on its
   operand resources (Deployments, Services, etc.). If the GC action deletes the
   old CR (because it is no longer in `rr.Resources`), Kubernetes cascades that
   deletion to all owned operands -- causing downtime.

2. **Ordering.** The existing `pkg/upgrade` pattern runs cleanup functions as
   `LeaderElectionRunnableFunc`, which executes *concurrently* with reconcilers.
   There is no ordering guarantee that migration completes before GC runs.

The solution is a dedicated **pipeline action** inside the DSC reconcile loop,
running before component and module provisioning, with version guardrails to
prevent unsafe migrations.

---

## Pipeline design

The `migrateComponentsToModules` action runs early in the DSC pipeline, before
both `provisionComponents` and `provisionModules`. This guarantees that by the
time GC runs (last action), the old component CR and its ownership have been
cleanly resolved.

```
DSC Reconcile Pipeline:

  initialize
  -> checkPreConditions
  -> updateStatus
  -> migrateComponentsToModules    <-- strips owner refs, deletes old CR
  -> provisionComponents           <-- old component no longer provisioned
  -> provisionModules              <-- new module handler takes over
  -> helmrender / kustomizerender
  -> deploy                        <-- SSA applies module operator + module CR
  -> updateModuleStatus
  -> gc                            <-- safe: old CR already gone, operands orphaned
```

---

## Migration mechanics

For each registered migration, the action performs the following steps:

### 1. Check for old component CR

GET the old component CR by its GVK and singleton name. If it is not found,
the component has already been migrated (or was never installed). Return
immediately -- the migration is a no-op.

### 2. Version guardrails

Read the `platform.opendatahub.io/version` annotation from the old CR. This
annotation records which operator version last applied the resource.

| Condition | Behaviour |
|---|---|
| Annotation missing | Skip migration, log error. The CR predates the annotation era and operands may be in an unknown state. |
| Version < `MinSourceVersion` | Skip migration, log error. The operator version is too old; operands may have a layout incompatible with the new module. |
| Version >= `MaxSourceVersion` | Skip migration, log error. Unexpected -- the CR was applied by a newer operator than expected. |
| Version in range `[min, max)` | Proceed with migration. |

Skipping is a **safe failure mode**: the old component continues to be
reconciled by `provisionComponents` as before. The operator logs a clear
message explaining why migration was skipped.

### 3. Strip owner references

List all resources labelled
`platform.opendatahub.io/part-of=<lowercased-old-kind>`. For each resource,
issue a JSON merge patch to remove the `ownerReferences` entry whose `uid`
matches the old component CR.

This is done atomically per-resource. If any patch fails, the migration aborts
**before** deleting the old CR, leaving everything in a recoverable state.

### 4. Delete old component CR

With owner references stripped, deleting the old CR no longer triggers cascade
deletion. The operand resources remain running and are immediately adoptable by
the module operator via Server-Side Apply with `ForceOwnership`.

### 5. Module takes over

On the same reconcile cycle (or the next), `provisionModules` deploys the
module operator and creates the module CR. The module operator picks up the
existing operand resources and reconciles them under its own ownership.

---

## ComponentMigration struct

Each migration is described by a static entry in the migration registry:

```go
type ComponentMigration struct {
    // OldGVK is the GroupVersionKind of the component CR being replaced.
    OldGVK schema.GroupVersionKind

    // OldCRName is the singleton name of the old component CR.
    OldCRName string

    // ModuleName is the name of the module replacing this component.
    ModuleName string

    // MinSourceVersion is the minimum platform version (inclusive, semver)
    // the old component CR must have been applied by.
    MinSourceVersion string

    // MaxSourceVersion is the maximum platform version (exclusive, semver).
    // Empty means no upper bound.
    MaxSourceVersion string
}
```

The registry starts empty. Teams add entries when they are ready to migrate:

```go
var componentMigrations = []ComponentMigration{}
```

---

## What changes for each migrating component

### Platform team checklist

For each component being migrated, the platform team must:

- [ ] Add a `ComponentMigration` entry to the migration registry with
      appropriate version guardrails
- [ ] Remove the component's `Owns()` line from the DSC controller builder
      (so the old CR type is no longer watched as an owned resource)
- [ ] Remove the component from `existingComponents` in `cmd/main.go`
- [ ] Add the module handler to `existingModules` in `cmd/main.go`
- [ ] Verify that the old component's DSC API stanza is preserved (users
      should not need to reconfigure anything)
- [ ] Add or update e2e tests for the migration path (upgrade from old
      version, verify operands survive, verify module takes over)

### Module team checklist

The module team (who may also be the component team) must:

- [ ] Build a module operator that reconciles the new module CR and manages
      the same operands the old component controller managed
- [ ] Ensure the module operator handles **pre-existing resources** gracefully
      (resources created by the old component will already exist on the
      cluster when the module operator first reconciles)
- [ ] Use Server-Side Apply with `ForceOwnership` to adopt pre-existing
      operand resources without deleting and re-creating them
- [ ] Implement the `ModuleHandler` interface in the operator repo (handler,
      DSC API stanza, registration -- see the Developer Guide)
- [ ] Package module operator manifests as Helm charts or Kustomize overlays
      and add them to `get_all_manifests.sh`
- [ ] Provide the module CRD in the manifests (the platform creates the CR)
- [ ] If the component currently uses the platform's HardwareProfile mutating
      webhook, implement your own webhook in the module operator. The platform
      will only deploy the HardwareProfile CRD and default profiles; modules
      handle their own webhook registration and injection lifecycle.

---

## CRD identity and naming

The new module CRD **must** use a different API group and/or kind from the old
component CRD. This is required because:

- Both CRDs may coexist on the cluster during the migration window
- Kubernetes does not allow two CRDs with the same GVK

**Convention:**

| | Old component | New module |
|---|---|---|
| API group | `components.opendatahub.io` | `components.platform.opendatahub.io` |
| Kind | e.g., `Dashboard` | e.g., `Dashboard` (same human name is fine) |
| Singleton name | e.g., `default-dashboard` | e.g., `default` |

The human-readable name (what users see in the DSC spec) can stay the same.
The DSC API stanza (`spec.components.dashboard`) does not change. Only the
underlying CRD identity is different.

---

## DSC API continuity

Users configure components through the `DataScienceCluster` CR. Migration must
be **transparent** to users:

- The `spec.components.<name>` stanza remains unchanged
- `ManagementState` (Managed/Removed) continues to work the same way
- Any module-specific fields in the DSC stanza are projected into the new
  module CR by `BuildModuleCR`
- Users do not need to edit their DSC after an upgrade that migrates a
  component to a module

---

## Shared concerns during migration

### Hardware profiles

The platform operator deploys the HardwareProfile CRD and default profiles.
When a component becomes a module, the module operator takes responsibility
for its own HardwareProfile mutating webhook (for workload types it manages).

### Platform labels and annotations

`deploy.NewAction` automatically applies `platform.opendatahub.io/part-of`
labels and platform annotations to all resources in `rr.Resources`. Module
operator resources and module CRs receive these labels without extra handler
code.

After migration, the module operator's operand resources should also carry
appropriate ownership labels. The module operator is responsible for labelling
its own operands.

### TrustedCABundle, auth, monitoring

These remain platform services. Module operators consume them by reading
cluster resources (ConfigMaps, secrets) from the applications namespace. No
change during migration.

---

## Rollback considerations

If a module migration goes wrong after the old CR has been deleted:

1. **Operand resources are still running** -- they were orphaned, not deleted.
2. **Rolling back the operator version** to a release that still has the
   component in `existingComponents` will cause `provisionComponents` to
   re-create the old component CR and resume managing the operands.
3. The migration action will not run (the migration entry would not exist in
   the older operator version).

For this reason, migration entries should only be added in a release that
also ships the module handler. Never add a migration entry without the
corresponding module handler being ready.

---

## Version guardrail examples

### Migrating Dashboard in operator v2.20

```go
var componentMigrations = []ComponentMigration{
    {
        OldGVK:           gvk.Dashboard,               // components.opendatahub.io/v1alpha1, Dashboard
        OldCRName:        "default-dashboard",
        ModuleName:       "dashboard",
        MinSourceVersion: "2.18.0",                     // oldest version with known-good operand layout
        MaxSourceVersion: "2.21.0",                     // should not migrate from future versions
    },
}
```

- Upgrading from v2.18.x or v2.19.x: migration proceeds
- Upgrading from v2.17.x: migration skipped, component continues as-is
  (user must first upgrade to v2.18+, then to v2.20)
- Fresh install (no old CR): migration is a no-op

---

## Testing the migration path

### Unit tests (migration logic)

| Test case | What it verifies |
|---|---|
| Version in range | Old CR with version in `[min, max)` -- owner refs stripped, CR deleted |
| Version too old | Old CR below `MinSourceVersion` -- CR and operands untouched, error logged |
| Version too new | Old CR at or above `MaxSourceVersion` -- CR and operands untouched, error logged |
| Missing annotation | Old CR without `platform.opendatahub.io/version` -- CR untouched, error logged |
| Already migrated | Old CR not found -- no-op, nil error |
| No migrations registered | Empty registry -- immediate return |
| Idempotent re-run | After successful migration, running again is a no-op |
| Partial failure | One operand patch fails -- old CR is NOT deleted, migration aborts safely |

### E2E tests (upgrade scenario)

1. Deploy the operator at version N (component is in-tree)
2. Create a DSC with the component enabled, verify operands are running
3. Upgrade the operator to version N+1 (component migrated to module)
4. Verify:
   - Old component CR is deleted
   - Operand pods were **not** restarted (no downtime)
   - Module operator Deployment exists
   - Module CR exists with correct spec fields
   - `ModulesReady` condition becomes `True`
   - Module operator reconciles operands successfully


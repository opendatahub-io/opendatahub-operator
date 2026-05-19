# Component to Module Migration Guide

This guide describes how an existing in-tree component migrates to a modular
architecture where it becomes an out-of-tree module operator managed by the ODH
Operator as a platform orchestrator.

For the module contract and handler implementation details, see the
[Module Handler Developer Guide](Module%20Handler%20Developer%20Guide.md) and
the
[Onboarding Guide](https://docs.google.com/document/d/1FgN_U-6XH8M-Mu6XNeldUlTPsnw7UyPCWg5NVJJdYnw).

---

## Same-GVK reconciler handoff

Components already use `components.platform.opendatahub.io` and services use
`services.platform.opendatahub.io`. When a component moves out of tree, the
**GVK stays the same**. Migration is a reconciler handoff: the in-tree
reconciler stops, the module operator starts reconciling the same CR.

This means:

- CR kind, group, version, and singleton name are **unchanged**.
- No owner-reference stripping is needed -- the CR is never deleted during
  migration. `provisionModules` adds it to `rr.Resources` on the same
  reconcile where `provisionComponents` stops adding it.
- The module operator adopts pre-existing operand resources via Server-Side
  Apply with `ForceOwnership`.

---

## How component lifecycle works today

Understanding the ownership chain is essential for the migration design.

The DSC controller provisions a component CR and registers it as an owned type
via `Owns()`. The deploy action sets the DSC as controller owner. The GC action
deletes the CR if it is missing from `rr.Resources` (only for owned types). A
separate in-tree component reconciler watches the CR and manages operands.

Key mechanisms:

- **DSC `Owns()` lines** in `datasciencecluster_controller.go` drive both
  watch registration and the deploy/GC ownership contract.
- **`shouldOwn`** in `deploy/action_deploy.go` checks
  `rr.Controller.Owns(objGVK)`. Only GVKs registered via `Owns()` (or
  `AddOwnedType`) get DSC as controller owner.
- **DSC GC TypePredicate** is `rr.Controller.Owns(objGVK)`. Only owned
  types are subject to GC.
- **Component reconcilers** have their own separate GC for operands.

---

## What changes during migration

When a component migrates to a module:

1. The component is removed from `existingComponents` and its in-tree
   reconciler is deregistered.
2. A `ModuleHandler` is added to `existingModules`. The handler's
   `BuildModuleCR` replaces the old `NewCRObject`.
3. The hardcoded `Owns()` line for the component CR is **removed** from
   the DSC controller. `SetupModuleWatches` + `AddOwnedType` replaces it
   uniformly for all modules, providing both watch registration and
   ownership/GC semantics.
4. The module operator (deployed via Helm chart) takes over reconciliation
   of the same CR and adopts existing operand resources.

Since the GVK is identical, there is no window where two CRDs coexist and
no period where operands are orphaned or deleted.

---

## Module disable and cleanup lifecycle

When a module is disabled (user sets `Removed` or CLI flag suppresses it),
the platform runs a **two-phase cleanup** to safely remove both the module CR
and the module operator resources.

### Phase 1 -- Delete the module CR (operator still running)

When a module is first detected as disabled, the `cleanupDisabledModules`
pipeline action detects the CR still exists and does nothing. The GC action
(later in the pipeline) deletes the CR because it is an owned type that is
missing from `rr.Resources`. The module operator Deployment is left running
so it can:

- Process any finalizer on the CR (cleaning up operands that cannot use
  ownerReferences)
- Kubernetes cascade-deletes operands that DO have ownerReferences from
  the CR

### Phase 2 -- Delete module operator resources (CR confirmed gone)

On a subsequent reconcile, `cleanupDisabledModules` confirms the module CR
no longer exists. It then renders the module's Helm chart and deletes each
discovered resource (Deployment, ServiceAccount, RBAC, CRD, ConfigMap).

### Pipeline order

```
initialize -> checkPreConditions -> updateStatus
  -> cleanupDisabledModules    (phase-gated operator resource cleanup)
  -> provisionComponents
  -> provisionModules
  -> helm/kustomize render -> deploy -> updateModuleStatus -> gc
```

---

## DSC API continuity

Users configure components through the `DataScienceCluster` CR. Migration must
be **transparent** to users:

- The `spec.components.<name>` stanza remains unchanged
- `ManagementState` (Managed/Removed) continues to work the same way
- Any module-specific fields in the DSC stanza are projected into the
  module CR by `BuildModuleCR`
- Users do not need to edit their DSC after an upgrade that migrates a
  component to a module

---

## Platform team checklist

For each component being migrated, the platform team must:

- [ ] Remove the component from `existingComponents` in `cmd/main.go`
- [ ] Add the module handler to `existingModules` in `cmd/main.go`
- [ ] **Remove** the `Owns()` line for the component CR from the DSC
      controller builder (`SetupModuleWatches` + `AddOwnedType` replaces
      it uniformly for all modules)
- [ ] Remove the component's `NewComponentReconciler` registration
- [ ] Add the module operator's Helm chart to `get_all_manifests.sh`
- [ ] Verify that the old component's DSC API stanza is preserved (users
      should not need to reconfigure anything)
- [ ] Add or update e2e tests for the migration path (upgrade from old
      version, verify operands survive, verify module takes over)

## Module team checklist

The module team (who may also be the component team) must:

- [ ] Build a module operator that reconciles the **same CR kind** (same
      GVK) the in-tree component used
- [ ] Ensure the module operator handles **pre-existing resources**
      gracefully (resources created by the old component will already
      exist on the cluster when the module operator first reconciles)
- [ ] Use Server-Side Apply with `ForceOwnership` to adopt pre-existing
      operand resources without deleting and re-creating them
- [ ] **Required:** Set ownerReferences from the module CR to all operand
      resources so Kubernetes cascade deletion handles cleanup when the
      CR is deleted
- [ ] **Recommended:** Add a finalizer on the module CR as a safety net
      for cleanup of resources that cannot use ownerReferences (e.g.,
      resources needing graceful shutdown logic or cross-namespace
      cleanup). The platform's two-phase cleanup ensures the module
      operator is still running when the CR is marked for deletion,
      giving finalizers time to execute.
- [ ] Implement the `ModuleHandler` interface in the operator repo
      (handler, DSC API stanza, registration -- see the Developer Guide)
- [ ] Package module operator manifests as a Helm chart and add them to
      `get_all_manifests.sh`. The chart must comply with chart compliance
      rules (Deployment, ServiceAccount, RBAC, ConfigMap, CRD only).
- [ ] Provide the module CRD in the manifests (the platform creates the CR)
- [ ] If the component currently uses the platform's HardwareProfile
      mutating webhook, implement your own webhook in the module operator

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

For trusted CA bundles specifically, the platform creates an
`odh-trusted-ca-bundle` ConfigMap in every non-reserved namespace (including
module dedicated namespaces). Module operators that need custom CA
certificates should mount this ConfigMap with `optional: true`. See the
[Trusted CA bundle](Module%20Handler%20Developer%20Guide.md#trusted-ca-bundle)
section in the Module Handler Developer Guide for the full contract and
consumption pattern.

---

## Rollback considerations

If a module migration goes wrong after upgrade:

1. **Operand resources are still running** -- they were not deleted during
   migration. The module operator adopted them via SSA.
2. **Rolling back the operator version** to a release that still has the
   component in `existingComponents` will cause `provisionComponents` to
   resume managing the CR and operands.
3. The module handler will not exist in the older operator version, so
   `provisionModules` will not run for that component.

For this reason, migration entries should only be added in a release that
also ships the module handler. Never add a module handler without the
corresponding module operator being ready.

---

## Testing the migration path

### Unit tests (handler logic)

| Test case | What it verifies |
|---|---|
| IsEnabled returns true when Managed | DSC/DSCI stanza correctly drives enablement |
| IsEnabled returns false when Removed | Module is correctly disabled |
| BuildModuleCR projects fields | GVK, name, and spec fields match expectations |
| BuildModuleCR handles empty state | Default management state is applied |
| Already migrated | Old component reconciler removed, module handler active |

### E2E tests (upgrade scenario)

1. Deploy the operator at version N (component is in-tree)
2. Create a DSC with the component enabled, verify operands are running
3. Upgrade the operator to version N+1 (component migrated to module)
4. Verify:
   - Operand pods were **not** restarted (no downtime)
   - Module operator Deployment exists
   - Module CR exists with correct spec fields
   - `ModulesReady` condition becomes `True`
   - Module operator reconciles operands successfully
5. Set component to `Removed`, verify:
   - Module CR is deleted (phase 1 -- GC)
   - Module operator resources are deleted (phase 2 -- `cleanupDisabledModules`)

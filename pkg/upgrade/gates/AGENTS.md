# Upgrade Gates Notes

This directory contains component-specific upgrade gate checks used by the
operator to decide whether an upgrade ack can be auto-acknowledged.

## Jira Tracking

- The main tracking item for this work is `RHOAIENG-82327` ("Implement one-time
  upgrade gate for 2.x -> 3.5").
- Keep repo-local planning notes for that epic in `pkg/upgrade/gates/TASKS.md`.
- `TASKS.md` is a read-only mirror of relevant Jira status/comments for local
  implementation tracking. Jira is the source of truth. Agents must never
  create, edit, transition, or comment on Jira issues from this workflow.
- When a tracked gate change materially changes implementation scope or status,
  update `TASKS.md` in the same change.

## What These Gates Are For

- Gates are evaluated by the running 3.x operator.
- They should focus on **live blocking conditions still visible in the cluster**.
- They are a poor fit for:
  - advisory-only checks
  - “someone should have done a prep step” checks
  - migration hints that do not represent a hard blocker

Good examples:

- legacy `CodeFlare` CRs or `AppWrapper` CRs still present
- `DataSciencePipelinesApplication` CRD still storing removed API versions
- `RayCluster` workloads still carrying the CodeFlare OAuth finalizer
- KServe workloads using removed/deprecated deployment modes
- Kueue-labeled workloads living outside Kueue-managed namespaces

Poor examples:

- backup-marker annotations
- “please review this before upgrade” warnings
- broad data-integrity lint checks for user-managed subsystems unless they are
  intentionally promoted to hard blockers

## Registration Pattern

- Implement a plain `Check` function with the existing signature:

```go
func Check(ctx context.Context, reader client.Reader, component, namespace string) error
```

- Add it to the shared registration map in `pkg/upgrade/gates/gates.go`:

```go
componentApi.<ComponentName>: <gatepkg>.Check,
```

- `cmd/main.go` wires these registrations through `upgradegates.Register()`.
- Do not add a package-local `Register()` helper unless there is a strong reason.

Upgrade gates are ensured for the target gate version, then auto-acknowledged
only after the registered component check passes. A failing check leaves the
gate unacknowledged and blocks the upgrade path.

## Reader / API Access

- Prefer `client.Reader` for gate checks.
- These checks are meant to look at live cluster state, so avoid cache-coupled
  behavior when possible.
- In controller code, prefer the controller's API reader when available; tests
  may use the client-backed reader supplied by the test environment.
- Use the standard error branch pattern:
  - found
  - `IsNotFound` / `IsNoMatchError`
  - other error

## Unmanaged Components

Gate keys that resolve to a known DSC component are auto-acknowledged when that
component resolves to `Removed`.

Gate keys that do **not** resolve to a DSC component remain in scope and still
run the registered upgrade check. This allows non-component gate keys to keep
enforcing custom blockers without being auto-acked just because they are absent
from the DSC component map.

## Existing Gate Semantics

### `kserve`

- covers blocking workload categories
- ModelMesh internal CR is handled separately by `modelmeshserving`

### `modelmeshserving`

- blocks on the legacy internal `ModelMeshServing` CR

### `codeflare`

- blocks on the legacy internal `CodeFlare` CR
- also blocks on leftover `AppWrapper` CRs

### `datasciencepipelines`

- checks the `DataSciencePipelinesApplication` CRD directly
- blocks when `status.storedVersions` still contains `v1alpha1`

### `ray`

- blocks on `RayCluster` objects with `ray.openshift.ai/oauth-finalizer`
- this was intentionally simplified to finalizer-only; it does **not** use the
  `odh.ray.io/pre-upgrade-backup-taken` annotation

### `kueue`

- reads Kueue management state from the user-facing `DataScienceCluster` CR
- the separate `dependencies-kueue-operator` gate blocks when `managementState=Managed`
- the dependency gate requires the `kueue-operator` OLM subscription when `managementState=Unmanaged`
- validates that kueue-labeled workloads do not live in namespaces missing
  `kueue.openshift.io/managed=true` once the `Unmanaged` operator prerequisite is met

Most checks inspect cluster-scoped or cross-namespace resources and therefore
intentionally ignore the `namespace` argument. Treat the argument as meaningful
only when the check explicitly lists namespaced resources; do not narrow a
cluster-wide check to the application namespace by accident.

## Test Conventions

### Unit tests

- Use typed custom errors and assert with `errors.As(...)`.
- Prefer minimal fixture creation through embedded templates in
  `resources/*.tmpl.yaml`.
- Use the shared `tp.RenderObject(...)` helper.

### Gomega notes

- Prefer plain Gomega with `g := NewWithT(t)`.
- For typed gate errors, use:
  - `g.Expect(errors.As(err, &blockingErr)).To(BeTrue())`
  - then assert fields directly on the typed error
- Prefer direct field assertions over substring matching on `err.Error()`.
- Use `t.Helper()` in shared assertion helpers so failures point at the caller.
- Prefer `t.Context()` over `context.Background()` when the test already has a
  context available.
- When ignoring benign `NotFound` cleanup errors in tests, prefer a helper such
  as `client.IgnoreNotFound(err)` plus a Gomega assertion over manual branching.

### Integration tests

- Use `pkg/utils/test/envt`.
- Prefer the common structure:
  - `Test<Component>Gates`
  - one shared test context
  - `t.Run(..., tc.test...)`
- Register every CRD the check may list, not just the one used in the current
  fixture. This matters for checks that iterate workload families.
- Use `t.Context()` where the test already has one.

### Provision tests

- `pkg/controller/provision/gates_action_test.go` must account for the in-tree
  gate set now loaded by `CheckUpgradeGatesInNamespace(...)`.
- Do **not** assume the test owns the full gate universe.
- When a provision test wants to isolate only its synthetic gates, seed the
  in-tree gates as already acknowledged and layer the test gates on top.
- Do not use that pattern for tests that are specifically meant to validate the
  embedded in-tree gate inventory itself.

## Versioning Gotchas

- Upgrade gate matching always includes the `3.5` minor scope while upgrading
  from 2.x, even if OLM still exposes only the old CSV.
- When a DSC-owning CSV in the range `>=3.5.0 <3.6.0` is present, the highest
  matching version is also used so exact patch gates such as `ack-3.5.2-*` run.
- `CheckUpgradeGatesInNamespace(...)` and auto-ack must use the same resolved
  version scopes; otherwise a gate can block without its health check running.
- `rr.Release` is the **current running operator release**, not the previous
  deployed release.
- `cluster.GetDeployedRelease()` reads previous deployed state from:
  - `DSC.Status.Release` first
  - `DSCI.Status.Release` second (only when the DSC CR is absent)

## Example E2E Strategy

For true migration coverage, an illustrative reliable E2E flow is:

1. install/run 2.25.x
2. create legacy/blocking state
3. upgrade to the current 3.x build
4. assert the gate blocks

Synthetic approaches that inject versions manually are fragile because:

- `cluster.GetRelease()` may short-circuit to `0.0.0` when `CI=true`
- `RHAI_VERSION` changes the running operator identity, not just the “old”
  version
- DSC/DSCI status may be restamped by the new controller during reconcile

Use synthetic version injection only with care, and keep unit/envtest coverage
as the main protection for the gate logic itself.

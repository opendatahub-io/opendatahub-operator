# Upgrade Gates Notes

This directory contains component-specific upgrade gate checks used by the
operator to decide whether an upgrade ack can be auto-acknowledged.

## Jira Tracking

- The main tracking item for this work is `RHOAIENG-82327` ("Implement one-time
  upgrade gate for 2.x -> 3.5").
- Keep repo-local planning notes for that epic in `pkg/upgrade/gates/TASKS.md`.
- `TASKS.md` is a read-only mirror of relevant Jira status/comments for local
  implementation tracking. Do not treat it as the source of truth for Jira, and
  do not edit Jira unless explicitly asked.
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

- Register it directly in `cmd/main.go` with:

```go
provision.RegisterUpgradeCheck(componentApi.<ComponentName>, <gatepkg>.Check)
```

- Do not add a package-local `Register()` helper unless there is a strong reason.

## Reader / API Access

- Prefer `client.Reader` for gate checks.
- These checks are meant to look at live cluster state, so avoid cache-coupled
  behavior when possible.
- Use the standard error branch pattern:
  - found
  - `IsNotFound` / `IsNoMatchError`
  - other error

## Unmanaged Components

Some problematic components still need a gate check even when the DSC does not
mark them as `Managed`.

If a gate must still run for unmanaged components, update:

- `pkg/controller/provision/auto_ack_action.go`

Specifically, add the component to `requiresCheckWhenUnmanaged(...)` only when
the blocker is derived from runtime/internal resources and DSC management state
is not authoritative enough to decide whether auto-ack is safe.

Current examples include:

- `modelmeshserving`
- `codeflare`
- `kueue`

## Existing Gate Semantics

### `kserve`

- covers blocking workload categories
- ModelMesh internal CR is handled separately by `modelmeshserving`

### `modelmeshserving`

- blocks on the legacy internal `ModelMeshServing` CR

### `codeflare`

- blocks on the legacy internal `CodeFlare` CR
- also blocks on leftover `AppWrapper` CRs

### `ray`

- blocks on `RayCluster` objects with `ray.openshift.ai/oauth-finalizer`
- this was intentionally simplified to finalizer-only; it does **not** use the
  `odh.ray.io/pre-upgrade-backup-taken` annotation

### `kueue`

- checks the runtime internal `kueue.openshift.io/v1` `Kueue` CR
- blocks immediately when `managementState=Managed`
- requires the `kueue-operator` OLM subscription when `managementState=Unmanaged`
- validates that kueue-labeled workloads do not live in namespaces missing
  `kueue.openshift.io/managed=true` once the `Unmanaged` operator prerequisite is met

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

- `pkg/controller/provision/gates_action.go` currently hardcodes the in-tree
  gate lookup version via `gateVersion = "3.5.1"`.
- Tests around `CheckUpgradeGatesInNamespace(...)` must respect that behavior.
- `rr.Release` is the **current running operator release**, not the previous
  deployed release.
- `cluster.GetDeployedRelease()` reads previous deployed state from:
  - `DSCI.Status.Release` first
  - `DSC.Status.Release` second

## E2E Strategy Notes

For true migration coverage, the most reliable E2E is still:

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

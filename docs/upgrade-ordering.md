# Upgrade Ordering

The ODH operator provisions components and modules in a deterministic order
using a directed acyclic graph (DAG). This document explains how the ordering
system works and how to integrate your component or module into it.

## Overview

Every component and module participates in a single shared DAG. The graph is
resolved into ordered batches at reconciliation time, and the orchestrator
advances through the batches sequentially. A batch cannot begin until every
entry in all prior batches reports `Ready=True`. This applies across types:
a component will not be provisioned if a module in a prior batch is not ready,
and vice versa.

```text
Startup
  cmd/main.go registers components and modules into
  the unified provisioning registry (pkg/controller/provision)

Reconcile (both DSC and module controllers follow the same sequence)

  1. Admin ack gate check
     If any unacknowledged upgrade gates exist, provisioning is blocked.

  2. Iterate unified batches
     For each batch:
       - gate: are ALL prior-batch entries ready? (cross-type)
       - provision only entries whose kind matches this controller
```

## Concepts

### Runlevels

A **runlevel** is an ordered provisioning tier. Every component and module
is assigned to exactly one runlevel. Lower-order runlevels are provisioned
first; all entries in a runlevel must be `Ready` before the next runlevel
begins.

The pre-defined runlevels are:

| Expression     | Purpose                                    |
|----------------|--------------------------------------------|
| `dag.RL(20)`   | Core AI/ML components (dashboard, pipelines, workbenches, ray, etc.) |
| `dag.RL(31)`   | Extension foundations (KServe, Kueue)      |
| `dag.RL(32)`   | Independent extensions (Feast, MLflow, OGX, Spark) |
| `dag.RL(33)`   | KServe-dependent extensions (ModelController, ModelsAsService, TrustyAI) |
| `dag.RL(99)`   | Fallback for unassigned entries (provisioned last) |

Pick any integer; lower values provision first. These are the current
assignments, not fixed constants. Lower values (e.g. 0, 10) are
available for future foundational or platform-level services. Use
adjacent values (e.g. 31, 32, 33) for fine-grained ordering within a
range.

### Readiness

An entry is considered ready when its CR has a `Ready=True` condition in its
status. For modules, the orchestrator also checks that `observedGeneration`
is not stale (i.e., the module controller has processed the current spec).

Readiness is checked by a `CompositeChecker` that dispatches to the
appropriate per-type checker depending on whether the name belongs to a
component or a module. This is transparent to your integration; you only need
to ensure your controller writes `Ready=True` when provisioning is complete.

### Status conditions

The DAG orchestrator writes to dedicated provisioning progress conditions,
separate from the component/module health rollup:

| Condition | Purpose |
|-----------|---------|
| `ProvisioningProgress` | DAG gating state — where is the orchestrator in the upgrade sequence? |
| `ComponentsReady` | Health rollup — are all managed components `Ready=True`? |
| `ModulesReady` | Health rollup — are all managed modules `Ready=True`? |

Both the DSC and module controllers write to `ProvisioningProgress` on
their respective CRs. The condition is `True` when all runlevels have
been processed, and `False` when the orchestrator is blocked on a
runlevel (reason `AwaitingReadiness`) or has timed out (reason
`RunlevelTimeoutExceeded`). The `lastTransitionTime` on the condition
indicates when the state was first entered.

### Timeout policy

Every runlevel defaults to a 10-minute wall-clock timeout
(`dag.DefaultTimeout`). If a runlevel remains not-ready for longer than
its timeout, the orchestrator advances past it and sets
`ProvisioningProgress=False` with reason `RunlevelTimeoutExceeded`. This
prevents a single stuck entry from blocking the entire platform
indefinitely.

To override the default for a specific runlevel, use `dag.SetRunlevelPolicy`:

```go
dag.SetRunlevelPolicy(0, dag.RunlevelPolicy{Timeout: 0})                    // block forever
dag.SetRunlevelPolicy(33, dag.RunlevelPolicy{Timeout: 10 * time.Minute})    // 10 min
```

Setting `Timeout` to `0` means the runlevel blocks forever (strict mode).

### Admin ack gates

Before any batch is processed, the orchestrator checks for unacknowledged
upgrade gates. Gates are collected from three sources:

1. **In-tree gates** — embedded YAML files under
   `pkg/controller/gates/resources/`. Used for components not yet migrated
   to modules. Remove when migration is complete.
2. **Cluster-discovered gates** — ConfigMaps in the operator namespace with
   the label `platform.opendatahub.io/upgrade-gate: "true"`.
3. **Chart-extracted gates** — ConfigMaps rendered from module Helm charts
   that carry the same label. These are intercepted before deployment and
   never applied to the cluster as standalone objects.

All collected gate entries are merged and written to a single
`odh-upgrade-acks` ConfigMap. Each key follows the pattern
`ack-<version>-<description>`. The value is either a human-readable
description (unacknowledged) or `"true"` (acknowledged by an admin).
Entries already set to `"true"` are never overwritten — this preserves
prior acknowledgments across reconciles.

An administrator acknowledges a gate by patching the ConfigMap:

```bash
kubectl patch cm odh-upgrade-acks -n <operator-namespace> \
  --type=merge -p '{"data":{"ack-2.20.0-api-removal":"true"}}'
```

If any gates for the current version remain unacknowledged, provisioning
is blocked entirely, `ProvisioningProgress` is set to `False` with reason
`AdminAckRequired`, and the condition message lists the unacked keys. The
operator watches the `odh-upgrade-acks` ConfigMap so provisioning resumes
automatically once all gates are acknowledged — no manual reconcile
trigger is needed.

## How to integrate your component or module

### Step 1: Choose a runlevel

Ask yourself:

- Does my component provide foundational infrastructure that others depend on?
  Use a low runlevel like `dag.RL(10)` or `dag.RL(20)`.
- Is it a core AI/ML capability with no dependencies on extension components?
  Use `dag.RL(20)`.
- Is it an extension that others depend on? Use `dag.RL(31)`.
- Is it an independent extension? Use `dag.RL(32)`.
- Does it depend on another extension like KServe? Use `dag.RL(33)`.

Pick whatever integer fits your position in the sequence. Use adjacent
numbers (31, 32, 33) for fine-grained ordering within a range.

### Step 2: Register in cmd/main.go

#### For in-tree components

Add an entry to the `componentRunlevels` map:

```go
componentRunlevels = map[string]dag.Runlevel{
    // ... existing entries ...
    componentApi.MyComponentName: dag.RL(32),
}
```

The `registerComponents()` function handles registration into both the
per-type component registry and the unified provisioning registry.

#### For out-of-tree modules

Add an entry to the `moduleRunlevels` map:

```go
moduleRunlevels = map[string]dag.Runlevel{
    "my-module": dag.RL(25),
}
```

Cross-runlevel gating ensures all lower-runlevel entries are ready before
higher runlevels begin. The `registerModules()` function handles the rest.

### Step 3: Ensure your controller reports Ready

The ordering system gates on the `Ready` condition. Your component or
module controller must set `Ready=True` in its CR status when all resources
are deployed and healthy. If your component never becomes ready, all entries
in later runlevels will be blocked.

## Current component ordering

The current assignments produce the following provisioning sequence:

```text
Batch 1 — RL(20):
  dashboard, datasciencepipelines, modelregistry, ray,
  trainer, trainingoperator, workbenches

Batch 2 — RL(31):
  kserve, kueue

Batch 3 — RL(32):
  feastoperator, mlflowoperator, ogx, sparkoperator

Batch 4 — RL(33):
  modelcontroller, modelsasservice, trustyai
```

Within each batch, entries are sorted alphabetically for determinism.

## Decision guide

| Scenario | Runlevel |
|----------|----------|
| Independent core component | `dag.RL(20)` |
| Extension that others depend on | `dag.RL(31)` |
| Extension, independent | `dag.RL(32)` |
| Extension that needs KServe ready first | `dag.RL(33)` |
| Module needing core components ready | `dag.RL(25)` |
| Infrastructure service | `dag.RL(0)` |

## Key files

| File | Purpose |
|------|---------|
| `pkg/controller/dag/dag.go` | DAG types, runlevel constants, graph resolution, readiness checker interface |
| `pkg/controller/provision/unified.go` | Unified registry merging components and modules into one graph |
| `pkg/controller/provision/readiness.go` | Composite readiness checker constructor |
| `pkg/controller/provision/gates_action.go` | Pre-provisioning admin ack gate action |
| `pkg/controller/gates/gates.go` | Admin ack gate checker |
| `cmd/main.go` | Runlevel declarations, registration |
| `internal/controller/components/registry/readiness.go` | Component readiness checker |
| `internal/controller/modules/readiness.go` | Module readiness checker |

## FAQ

**How does ordering work between components and modules?**
Both components and modules declare only a runlevel. Cross-runlevel gating
ensures all entries in lower runlevels are ready before higher runlevels
begin. To ensure your entry provisions after another, assign it a higher
runlevel.

**What happens if my component is disabled?**
Disabled entries are excluded from the graph. Other entries at higher
runlevels are unaffected since ordering is purely runlevel-based.

**What happens if I don't assign a runlevel?**
Your entry defaults to `dag.RL(99)` (order 99), which is provisioned
last. This is safe but may not reflect the intended ordering.

**Can I create a new runlevel?**
Yes — just use `dag.RL(n)` with any integer. No changes to the DAG package
are needed. Pick a value that places your entry in the right position
relative to existing assignments. Coordinate with the platform team if
the ordering affects shared infrastructure.

**How do I declare an admin ack gate?**
Include a ConfigMap in your module's Helm chart with the label
`platform.opendatahub.io/upgrade-gate: "true"`. The operator extracts gate
entries from rendered charts before deployment and merges them into the
`odh-upgrade-acks` ConfigMap. Each key must follow the pattern
`ack-<version>-<description>` and the value should be a human-readable
message explaining what the admin needs to acknowledge. For in-tree
components that have not yet migrated to modules, add entries to
`pkg/controller/gates/resources/gates.yaml` instead.

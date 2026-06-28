# DAG-Based Provisioning

The ODH operator provisions components in dependency order using a
runlevel-based DAG. Three gating layers control when components are
deployed: **admin acknowledgment gates**, **runlevel readiness gates**,
and **per-controller deploy gates**.

## Runlevel Assignments

Components are assigned a runlevel that determines provisioning order.
Lower runlevels provision first; components at the same runlevel form a
batch and are provisioned together.

| Runlevel | Components | Purpose |
|----------|------------|---------|
| 20 | Dashboard, DataSciencePipelines, ModelRegistry, Ray, Trainer, TrainingOperator, Workbenches | Core AI/ML — no inter-component dependencies |
| 31 | Kserve, Kueue | Extension foundations |
| 32 | FeastOperator, MLflowOperator, OGX, SparkOperator | Independent extensions |
| 33 | ModelController, ModelsAsService, TrustyAI | Require Kserve Ready |

Assignments are registered in `cmd/main.go` via `provision.Add()`.
Components not explicitly assigned default to runlevel 99.

## Provisioning Flow

```
DSC reconcile
  │
  ├─ 1. CheckUpgradeGates   ← admin ack layer
  │     ├─ unacked → StopError, ProvisioningProgress=False (AdminAckRequired)
  │     └─ all acked → continue
  │
  └─ 2. WalkBatches          ← runlevel readiness layer
        │
        ├─ Batch 20: provision core components
        │   └─ wait for all Ready (up to 10min timeout)
        │
        ├─ Batch 31: provision extension foundations
        │   └─ wait for all Ready
        │
        ├─ Batch 32: provision independent extensions
        │   └─ wait for all Ready
        │
        └─ Batch 33: provision KServe-dependent components
```

Each component also has its own controller. When that controller
reconciles, a `RunlevelGateAction` checks whether the DAG has reached
its runlevel before allowing resource deployment.

## Layer 1: Admin Acknowledgment Gates

Before the DAG walk begins, the operator checks for upgrade gates that
require explicit admin approval.

**Gate sources** (merged):
- In-tree gates compiled into the operator binary (`gates/resources/gates.yaml`)
- Labeled ConfigMaps on the cluster (`platform.opendatahub.io/upgrade-gate: "true"`)
- Gates extracted from rendered Helm charts

**How it works:**
1. The operator writes gate descriptions to the `odh-upgrade-acks` ConfigMap.
2. Each gate key follows the pattern `ack-<version>-<description>`.
3. An admin acknowledges a gate by setting its value to `"true"`.
4. Until all gates for the current version are acknowledged, provisioning
   is blocked with `ProvisioningProgress=False (AdminAckRequired)`.

Gates never overwrite a key already set to `"true"`, so acknowledgments
survive operator restarts.

## Layer 2: Runlevel Readiness Gates (WalkBatches)

The DSC controller walks batches in order. Before processing batch N, it
checks that **all entries in prior batches** are Ready.

**If prior entries are not ready:**
- `ProvisioningProgress=False (AwaitingReadiness)` with a message
  listing the blocked entries and remaining timeout.
- The controller requeues for the remaining duration.

**Timeout behavior:**
- Default: 10 minutes per runlevel (configurable per runlevel).
- When the timeout expires, the operator advances past the stuck entries
  with `ProvisioningProgress=False (RunlevelTimeoutExceeded)`.
- Timed-out entries are skipped in subsequent readiness checks.

**RunlevelTracker:**
As each batch clears, the DSC controller calls
`RunlevelTracker.MarkCleared(version, order)`. This in-memory singleton
records which runlevels have been provisioned at the current operator
version:
- On operator restart (pod restart, upgrade), the tracker is empty —
  all component controllers block until the DSC re-walks the DAG.
- On version change, the tracker resets — components block until the new
  version's DAG walk reaches them.

## Layer 3: Per-Controller Deploy Gate (RunlevelGateAction)

Each component controller includes `RunlevelGateAction` as its first
action. It checks the RunlevelTracker before allowing resource
deployment.

**When the runlevel is NOT cleared:**
- Sets `rr.SkipDeploy = true` — render, deploy, and GC actions return
  early without applying any resources.
- Sets `PlatformReady=False` (reason: `RunlevelNotCleared`) on the
  component CR.
- Returns `RequeueAfterError(30s)` so the controller periodically
  rechecks.

**When the runlevel IS cleared:**
- Sets `PlatformReady=True` on the component CR.
- The full render/deploy/GC chain executes normally.

**Key property:** Status-reporting actions always run regardless of
`SkipDeploy`. This means a component that is already deployed and
healthy will continue to report `Ready=True` even while gated from
deploying new manifests. The gate prevents new resource application
without hiding existing health.

## Status Conditions

### DSC-level: `ProvisioningProgress`

| Status | Reason | Meaning |
|--------|--------|---------|
| `True` | — | All batches walked; provisioning complete |
| `False` | `AdminAckRequired` | Upgrade gates not acknowledged |
| `False` | `AwaitingReadiness` | Prior-batch components not yet Ready |
| `False` | `RunlevelTimeoutExceeded` | Timeout elapsed; advancing past stuck entries |
| `False` | `DAGResolutionFailed` | DAG could not be resolved |

### Component CR-level: `PlatformReady`

| Status | Reason | Meaning |
|--------|--------|---------|
| `True` | — | Platform orchestrator has reached this runlevel |
| `False` | `RunlevelNotCleared` | Runlevel not yet reached; deploy skipped |

`PlatformReady` has **Info severity** — it does not affect the
component's `Ready` condition. A component can be `Ready=True` and
`PlatformReady=False` simultaneously: the existing deployment is
healthy, but new manifests will not be applied until the gate lifts.

## Upgrade Scenario

When the operator upgrades from version N to N+1:

1. New operator pods start → RunlevelTracker is empty.
2. All component controllers reconcile → `IsCleared` returns false →
   `SkipDeploy=true` on all components (existing deployments untouched).
3. `CheckUpgradeGates` runs → if gates exist, blocks until acknowledged.
4. `WalkBatches` starts → batch 20 provisions first →
   `MarkCleared("N+1", 20)`.
5. Component controllers at runlevel 20 reconcile → `IsCleared` returns
   true → full deploy with new manifests.
6. DAG proceeds through remaining batches in order.

This ensures new manifests are applied in dependency order, even though
all component controllers start simultaneously.

# Upgrade Gate Tasks

This file tracks the local follow-up for Jira epic `RHOAIENG-82327`
("Implement one-time upgrade gate for 2.x -> 3.5").

It is a read-only snapshot of the relevant Jira child work items and comments as
reviewed on 2026-08-18. Do not edit Jira from here.

`Task status` is repo-local and reflects the current state of this repository,
not the live Jira workflow.

Legend:
- `✅` implemented locally
- `📝` todo
- `🚫` not needed
- `⚠️` requires confirmation against Jira comments or scope

## Status Table

| Jira | Component | Jira status | Task status | Jira comment alignment |
| --- | --- | --- | --- | --- |
| `RHOAIENG-82350` | DataSciencePipelines | In Progress | `✅ Implemented locally` | `⚠️ Confirm scope` |
| `RHOAIENG-82351` | Kueue | New | `✅ Implemented locally` | `⚠️ Confirm scope` |
| `RHOAIENG-82352` | ModelRegistry | New | `📝 Todo` | `-` |
| `RHOAIENG-82353` | Ray | New | `✅ Implemented locally` | `⚠️ Confirm scope` |
| `RHOAIENG-82354` | SparkOperator | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82355` | Trainer | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82356` | TrainingOperator | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82357` | TrustyAI | In Progress | `📝 Todo` | `-` |
| `RHOAIENG-82359` | Dashboard | In Progress | `📝 Todo` | `-` |
| `RHOAIENG-82360` | KServe | New | `✅ Implemented locally` | `⚠️ Confirm scope` |
| `RHOAIENG-82361` | Workbenches | New | `📝 Todo` | `-` |
| `RHOAIENG-82370` | FeastOperator | New | `📝 Todo` | `-` |
| `RHOAIENG-82371` | LlamaStackOperator | New | `📝 Todo` | `-` |
| `RHOAIENG-82372` | OGX | New | `📝 Todo` | `-` |
| `RHOAIENG-82373` | MLflowOperator | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82374` | AIGateway | Resolved | `🚫 Not needed` | `-` |
| `RHOAIENG-82380` | MCPLifecycleOperator | New | `🚫 Not needed` | `-` |
| `-` | `dependencies-cert-manager` | `-` | `✅ Implemented locally` | `-` |
| `-` | `dependencies-servicemeshoperatorv2` | `-` | `✅ Implemented locally` | `-` |

## Task Entries

### `RHOAIENG-82350` DataSciencePipelines

- Jira status: `In Progress`
- Task status: `Implemented locally`
- Jira comment alignment: `Requires confirmation`
- Jira comments: no actionable comments found beyond the generic spike request.
- Checks implemented in repo:
  - block when the `DataSciencePipelinesApplication` CRD still reports
    `v1alpha1` in `status.storedVersions`
- Follow-up note:
  - confirm that the stored-version blocker is the intended Jira scope for this
    child item and that no separate DSP migration gate is expected

### `RHOAIENG-82351` Kueue

- Jira status: `New`
- Task status: `Implemented locally`
- Jira comment alignment: `Requires confirmation`
- Jira comments:
  - clarify whether `kueue <-> kserve` integration validation belongs under the
    Kueue task or the KServe task
- Checks implemented in repo:
  - block when the internal `kueue.openshift.io/v1` `Kueue` CR is still in
    `Managed` state
  - require the `kueue-operator` OLM subscription when the internal Kueue CR is
    in `Unmanaged` state
  - block when kueue-labeled workloads live in namespaces missing
    `kueue.openshift.io/managed=true`
- Follow-up note:
  - if a future integration-specific `kueue <-> kserve` blocker is needed,
    scope it explicitly rather than folding it into the current workload and
    namespace-label validation by accident

### `RHOAIENG-82352` ModelRegistry

- Jira status: `New`
- Task status: `Todo`
- Jira comments: no actionable comments found beyond the generic spike request.
- Checks to implement:
  - determine whether ModelRegistry has any persisted `2.25` state that becomes
    a hard blocker during `3.5` reconciliation
  - only add a gate if that state can be detected from live cluster resources

### `RHOAIENG-82353` Ray

- Jira status: `New`
- Task status: `Implemented locally`
- Jira comment alignment: `Requires confirmation`
- Jira comments: no actionable comments found beyond the generic spike request.
- Checks implemented in repo:
  - block when `RayCluster` objects still carry the
    `ray.openshift.ai/oauth-finalizer`
- Notes:
  - the repo intentionally does not block on the
    `odh.ray.io/pre-upgrade-backup-taken` annotation because that is an
    advisory/prep-step marker rather than a live blocking condition

### `RHOAIENG-82354` SparkOperator

- Jira status: `Closed`
- Task status: `Not needed`
- Jira comments:
  - no upgrade validation needed because Spark Operator did not exist in `2.x`
- Local interpretation:
  - keep this as a documented no-op unless new evidence shows a real `2.x`
    compatibility surface

### `RHOAIENG-82355` Trainer

- Jira status: `Closed`
- Task status: `Not needed`
- Jira comments:
  - no upgrade validation needed because Trainer is a `3.x` component and was
    not present in `2.25`

### `RHOAIENG-82356` TrainingOperator

- Jira status: `Closed`
- Task status: `Not needed`
- Jira comments:
  - no validation check needed because `2.25` workloads should continue to work
    on `3.5`

### `RHOAIENG-82357` TrustyAI

- Jira status: `In Progress`
- Task status: `Todo`
- Jira comments: no actionable comments found beyond the generic spike request.
- Checks to implement:
  - determine whether TrustyAI has any upgrade-blocking runtime state that
    remains visible to the `3.5` operator
  - avoid adding advisory-only or operator-absent checks

### `RHOAIENG-82359` Dashboard

- Jira status: `In Progress`
- Task status: `Todo`
- Jira comments:
  - prior investigation of the same upgrade path may help with testing, but no
    concrete validation checks were specified
- Checks to implement:
  - identify any live Dashboard-owned resources that should hard-block
    auto-acknowledgement during `2.25 -> 3.5`
  - keep testing notes separate from actual blocking conditions

### `RHOAIENG-82360` KServe

- Jira status: `New`
- Task status: `Implemented locally`
- Jira comment alignment: `Requires confirmation`
- Jira comments: no actionable comments found beyond the generic spike request.
- Checks implemented in repo:
  - block on `InferenceService` objects using
    `serving.kserve.io/deploymentMode=Serverless`
  - block on `InferenceService` objects using
    `serving.kserve.io/deploymentMode=ModelMesh`
  - block on `ServingRuntime` objects with `spec.multiModel=true`
  - block on `InferenceService` objects that reference removed runtimes
- Related note:
  - the legacy internal `ModelMeshServing` CR is handled by a separate local
    gate implementation even though it is not tracked by a matching Jira child
    issue in this list

### `RHOAIENG-82361` Workbenches

- Jira status: `New`
- Task status: `Todo`
- Jira comments: no actionable comments found beyond the generic spike request.
- Checks to implement:
  - determine whether Workbenches have any live `2.25` workload state that
    needs to block `3.5` upgrade auto-ack
  - prefer a direct workload/resource check over a synthetic migration checklist

### `RHOAIENG-82370` FeastOperator

- Jira status: `New`
- Task status: `Todo`
- Jira comments: no actionable comments found beyond the generic spike request.
- Checks to implement:
  - determine whether FeastOperator has any cluster-visible `2.x` state that
    should hard-block auto-ack during upgrade

### `RHOAIENG-82371` LlamaStackOperator

- Jira status: `New`
- Task status: `Todo`
- Jira comments: no actionable comments found beyond the generic spike request.
- Checks to implement:
  - determine whether there is any real `2.x` upgrade surface for this
    component before adding a gate

### `RHOAIENG-82372` OGX

- Jira status: `New`
- Task status: `Todo`
- Jira comments: no actionable comments found beyond the generic spike request.
- Checks to implement:
  - determine whether OGX exposes any live upgrade-blocking state that is still
    visible to the `3.5` operator

### `RHOAIENG-82373` MLflowOperator

- Jira status: `Closed`
- Task status: `Not needed`
- Jira comments:
  - no upgrade validation needed because MLflow/MLflow Operator did not exist
    in `2.x`

### `RHOAIENG-82374` AIGateway

- Jira status: `Resolved`
- Task status: `Not needed`
- Jira comments:
  - no upgrade validation needed because MaaS/AIGateway was not present in `2.x`
    and the operator did not exist there

### `RHOAIENG-82380` MCPLifecycleOperator

- Jira status: `New`
- Task status: `Not needed`
- Jira comments:
  - no upgrade validation is needed because MCPLifecycleOperator was not present
    in RHOAI `2.25`

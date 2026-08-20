# Upgrade Gate Tasks

This file tracks the local follow-up for Jira epic `RHOAIENG-82327`
("Implement one-time upgrade gate for 2.x -> 3.5").

It is a read-only snapshot of the relevant Jira child work items and comments as
reviewed on 2026-08-20. Do not edit Jira from here.

`Task status` is repo-local and reflects the current state of this repository,
not the live Jira workflow.

Legend:
- `✅` implemented locally
- `📝` todo
- `🚫` not needed
- `⚠️` follow-up needed / discrepancy remains
- `⚠️` requires confirmation against Jira comments or scope

## Status Table

| Jira | Component | Jira status | Task status | Jira comment alignment |
| --- | --- | --- | --- | --- |
| `RHOAIENG-82350` | DataSciencePipelines | Review | `✅ Implemented locally` | `Live Jira RBAC blocker now implemented locally` |
| `RHOAIENG-82351` | Kueue | Resolved | `✅ Implemented locally` | `Removed+queued workload blocker now implemented locally` |
| `RHOAIENG-82352` | ModelRegistry | Closed | `🚫 Not needed` | `Live Jira says no domain-specific checks` |
| `RHOAIENG-82353` | Ray | Resolved | `✅ Implemented locally` | `Aligned with latest Jira comments` |
| `RHOAIENG-82354` | SparkOperator | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82355` | Trainer | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82356` | TrainingOperator | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82357` | TrustyAI | Resolved | `✅ Implemented locally` | `PVC storage blocker implemented; remaining spike items are advisory` |
| `RHOAIENG-82359` | Dashboard | In Progress | `⚠️ Follow-up needed` | `Comment scope looks broader than operator gates` |
| `RHOAIENG-82360` | KServe | Resolved | `✅ Implemented locally` | `Aligned with latest Jira comments` |
| `RHOAIENG-82361` | Workbenches | Backlog | `⚠️ Follow-up needed` | `No text summary yet; Jira comment is image-based` |
| `RHOAIENG-82370` | FeastOperator | Resolved | `🚫 Not needed` | `-` |
| `RHOAIENG-82371` | LlamaStackOperator | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82372` | OGX | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82373` | MLflowOperator | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82374` | AIGateway | Resolved | `🚫 Not needed` | `-` |
| `RHOAIENG-82380` | MCPLifecycleOperator | Closed | `🚫 Not needed` | `-` |
| `-` | `dependencies-cert-manager` | `-` | `✅ Implemented locally` | `-` |
| `-` | `dependencies-kueue-operator` | `-` | `✅ Implemented locally` | `-` |
| `-` | `dependencies-servicemeshoperatorv2` | `-` | `✅ Implemented locally` | `-` |

## Task Entries

### `RHOAIENG-82350` DataSciencePipelines

- Jira status: `Review`
- Task status: `Implemented locally`
- Jira comment alignment: `Aligned with latest Jira blocker list`
- Jira comments:
  - latest live Jira comment adds a second blocking requirement beyond CRD
    stored versions: scan custom Roles that still grant the old Route-based API
    access but do not grant `datasciencepipelinesapplications/api`
- Checks implemented in repo:
  - block when the `DataSciencePipelinesApplication` CRD still reports
    `v1alpha1` in `status.storedVersions`
  - block when namespace `Role` objects still grant legacy
    `route.openshift.io/routes` access without the replacement
    `datasciencepipelinesapplications/api` permission
- Follow-up note:
  - if Jira review narrows this back to odh-cli-only scope later, revisit whether
    the RBAC blocker should stay in the operator gate set

### `RHOAIENG-82351` Kueue

- Jira status: `Resolved`
- Task status: `Implemented locally`
- Jira comment alignment: `Aligned with latest Jira comments`
- Jira comments:
  - clarify whether `kueue <-> kserve` integration validation belongs under the
    Kueue task or the KServe task
  - latest live Jira follow-up says queued workloads require Kueue to be
    `Unmanaged`, not `Removed`; if queued workloads exist, `Removed` should also
    fail the gate
- Checks implemented in repo:
  - block when kueue-labeled workloads live in namespaces missing
    `kueue.openshift.io/managed=true`
- Related dependency gate:
  - `dependencies-kueue-operator` blocks when the internal
    `kueue.openshift.io/v1` `Kueue` CR is still `Managed`
  - `dependencies-kueue-operator` requires the `kueue-operator` OLM
    subscription when the internal Kueue CR is `Unmanaged`
- Follow-up note:
  - if a future integration-specific `kueue <-> kserve` blocker is needed,
    scope it explicitly rather than folding it into the current workload and
    namespace-label validation by accident
  - repo now also blocks when queued workloads exist while Kueue is `Removed`,
    matching the latest Jira clarification

### `RHOAIENG-82352` ModelRegistry

- Jira status: `Closed`
- Task status: `Not needed`
- Jira comments:
  - live Jira comment says the team decided there are no domain-specific upgrade
    validation checks for ModelRegistry
- Local interpretation:
  - keep this as a documented no-op unless a later Jira reopen introduces a
    concrete live-cluster blocker

### `RHOAIENG-82353` Ray

- Jira status: `Resolved`
- Task status: `Implemented locally`
- Jira comment alignment: `Aligned`
- Jira comments:
  - latest live Jira thread confirms AppWrappers are advisory/non-blocking
  - Kueue management-state blocking is tracked under `RHOAIENG-82351`, not this
    Ray gate
- Checks implemented in repo:
  - block when `RayCluster` objects still carry the
    `ray.openshift.ai/oauth-finalizer` and do not have the
    `odh.ray.io/pre-upgrade-backup-taken` annotation
- Notes:
  - AppWrappers remain advisory/non-blocking and are intentionally not encoded
    as an operator upgrade gate
  - Kueue management-state blocking remains tracked under `RHOAIENG-82351`, not
    the Ray gate

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

- Jira status: `Resolved`
- Task status: `Implemented locally`
- Jira comments:
  - spike doc says the only blocking TrustyAI lint condition is
    `TrustyAIService.spec.storage.format == "PVC"`
  - the other TrustyAI spike items are advisory-only (`impacted workloads`,
    `DATABASE` storage, scheduled metrics backup)
- Checks implemented in repo:
  - block when any `TrustyAIService` uses `spec.storage.format: PVC`
- Notes:
  - advisory-only TrustyAI spike items are intentionally not encoded as
    operator upgrade blockers at this time
  - the spike's GPU-deadlock blocker was reviewed, but it is currently hard to
    implement safely here because it depends on exact TrustyAI annotation
    matching plus cross-component webhook-health detection for KServe

### `RHOAIENG-82359` Dashboard

- Jira status: `In Progress`
- Task status: `Follow-up needed`
- Jira comments:
  - latest live Jira comment contains a broad dashboard upgrade analysis with
    proposed checks for CRD conversion webhooks, CRD storedVersions, finalizer
    orphaning, oauth-proxy transition, route topology, resource capacity, and
    `OdhDashboardConfig`
- Discrepancy to resolve later:
  - many of those proposals appear to target odh-cli lint or manual
    delete/recreate upgrade paths rather than live operator-side upgrade gates
  - no dashboard-specific gate package exists in this repo yet
- Resume note:
  - triage the Jira list into:
    - valid operator-side live blockers
    - odh-cli/manual-upgrade checks that should stay out of this repo
  - do not start implementing dashboard gates until that scope split is made

### `RHOAIENG-82360` KServe

- Jira status: `Resolved`
- Task status: `Implemented locally`
- Jira comment alignment: `Aligned`
- Jira comments:
  - latest live Jira comments say the existing KServe checks are sufficient
  - Kueue-related follow-up was reviewed and does not currently require extra
    KServe-side gate logic
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

- Jira status: `Backlog`
- Task status: `Follow-up needed`
- Jira comments:
  - live Jira comments show schedule movement and an attached image-based
    evaluation, but there is no text summary in Jira comments that can be used
    as an implementation spec here
- Discrepancy to resolve later:
  - repo has no workbenches-specific upgrade gate package
  - current Jira text is not enough to tell whether a real operator-side gate is
    needed
- Resume note:
  - ask for the image contents to be summarized in text or capture the exact
    proposed blockers before implementing anything in this repo

### `RHOAIENG-82370` FeastOperator

- Jira status: `Resolved`
- Task status: `Not needed`
- Jira comments: no actionable comments found beyond the generic spike request.
- Checks to implement:
  - determine whether FeastOperator has any cluster-visible `2.x` state that
    should hard-block auto-ack during upgrade

### `RHOAIENG-82371` LlamaStackOperator

- Jira status: `Closed`
- Task status: `Not needed`
- Jira comments: no actionable comments found beyond the generic spike request.
- Checks to implement:
  - determine whether there is any real `2.x` upgrade surface for this
    component before adding a gate

### `RHOAIENG-82372` OGX

- Jira status: `Closed`
- Task status: `Not needed`
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

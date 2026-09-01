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
| `RHOAIENG-82350` | DataSciencePipelines | Review | `✅ Done` | `Jira blockers implemented; odh-cli advisory items intentionally left out` |
| `RHOAIENG-82351` | Kueue | Resolved | `✅ Done` | `Jira blockers implemented; broader odh-cli invariants remain out of scope` |
| `RHOAIENG-82352` | ModelRegistry | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82353` | Ray | Resolved | `✅ Done` | `Primary Jira blocker implemented; Kueue prerequisite handled separately` |
| `RHOAIENG-82354` | SparkOperator | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82355` | Trainer | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82356` | TrainingOperator | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82357` | TrustyAI | Resolved | `✅ Done` | `PVC blocker implemented; migration-only spike actions intentionally left out` |
| `RHOAIENG-82359` | Dashboard | In Progress | `⚠️ Follow-up needed` | `Comment scope looks broader than operator gates` |
| `RHOAIENG-82360` | KServe | Resolved | `✅ Done` | `Current operator scope now includes the Authorino TLS blocker from odh-cli` |
| `RHOAIENG-82361` | Workbenches | Review | `✅ Done` | `CLI-aligned blocking rules implemented; advisory and user-action checks left out` |
| `RHOAIENG-82370` | FeastOperator | Resolved | `🚫 Not needed` | `-` |
| `RHOAIENG-82371` | LlamaStackOperator | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82372` | OGX | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82373` | MLflowOperator | Closed | `🚫 Not needed` | `-` |
| `RHOAIENG-82374` | AIGateway | Resolved | `🚫 Not needed` | `-` |
| `RHOAIENG-82380` | MCPLifecycleOperator | Closed | `🚫 Not needed` | `-` |
| `-` | `dependencies-cert-manager` | `-` | `✅ Done` | `Local-only repo gate; no matching Jira child issue` |
| `-` | `dependencies-kueue-operator` | `-` | `✅ Done` | `Local-only repo gate; split out from Kueue Jira scope` |
| `-` | `dependencies-servicemeshoperatorv2` | `-` | `✅ Done` | `Local-only repo gate; no matching Jira child issue` |

## Task Entries

### `RHOAIENG-82350` DataSciencePipelines

- Jira status: `Review`
- Task status: `Done`
- Jira comment alignment: `Repo scope now keeps only the CRD blocker; custom-role migration stays out of the operator gate`
- Jira comments:
  - latest live Jira comment adds a second blocking requirement beyond CRD
    stored versions: scan custom Roles that still grant the old Route-based API
    access but do not grant `datasciencepipelinesapplications/api`
- Checks implemented in repo:
  - block when the `DataSciencePipelinesApplication` CRD still reports
    `v1alpha1` in `status.storedVersions`
- Detailed behavior:
  - the stored-version check is a straight CRD status read; if the DSPA CRD is
    absent, this branch is treated as non-blocking because there is nothing left
    to migrate for this API surface
- Left out from Jira / odh-cli:
  - the custom-role migration check (`route.openshift.io/routes` without
    `datasciencepipelinesapplications/api`) was removed from the operator gate
    because a cluster-wide passive Role scan produced false positives on
    non-DSP roles in otherwise empty clusters
  - remediation for that gap remains the odh-cli
    `ai-pipelines.update-dsp-role` migrate action rather than a hard operator
    blocker
  - odh-cli also carries advisory checks for removed
    `.spec.apiServer.managedPipelines.instructLab` fields and the
    `datasciencepipelines -> aipipelines` DSC rename notice
  - those are intentionally not encoded as operator blockers because they do not
    represent a live-cluster hard stop for the auto-ack path
- Notes:
  - the repo implementation is cluster-state based, so it does not need a
    separate DSC-v2 rename rewrite to find the CRD

### `RHOAIENG-82351` Kueue

- Jira status: `Resolved`
- Task status: `Done`
- Jira comment alignment: `Jira blockers implemented; repo scope is narrower than full odh-cli lint`
- Jira comments:
  - clarify whether `kueue <-> kserve` integration validation belongs under the
    Kueue task or the KServe task
  - latest live Jira follow-up says queued workloads require Kueue to be
    `Unmanaged`, not `Removed`; if queued workloads exist, `Removed` should also
    fail the gate
- Checks implemented in repo:
  - block when queued workloads (`kueue.x-k8s.io/queue-name`) live in
    namespaces missing `kueue.openshift.io/managed=true`
  - block when queued workloads exist while Kueue is `Removed` in the DSC
- Detailed behavior:
  - workload discovery is cross-component: `Notebook`, `InferenceService`,
    `LLMInferenceService`, `RayCluster`, `RayJob`, and `PyTorchJob` resources
    are all scanned for the Kueue queue label
  - the `Removed` case is treated as a hard blocker only when queued workloads
    still exist; an idle cluster with Kueue already removed is allowed through
  - the namespace-label branch caches namespace lookups, so repeated queued
    workloads in the same namespace do not multiply API reads
- Related dependency gate:
  - `dependencies-kueue-operator` blocks when Kueue is still `Managed` in the
    DSC
  - `dependencies-kueue-operator` requires the `kueue-operator` OLM
    subscription when Kueue is `Unmanaged` in the DSC
- Left out from odh-cli:
  - odh-cli's `workloads.kueue.data-integrity` lint is broader: it also checks
    workloads inside Kueue-managed namespaces that are missing the queue label,
    and ownership-tree consistency for queue labels across descendants
  - those broader invariants are not mentioned in the Jira blocker text and are
    intentionally left out of the operator gate for now
- Notes:
  - if a future integration-specific `kueue <-> kserve` blocker is needed,
    scope it explicitly rather than folding it into the current workload and
    namespace-label validation by accident
  - the repo scope is split across the main `kueue` gate and the local-only
    `dependencies-kueue-operator` gate; odh-cli expresses those as separate lint
    checks as well

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
- Task status: `Done`
- Jira comment alignment: `Aligned with Jira blocking behavior`
- Jira comments:
  - latest live Jira thread confirms AppWrappers are advisory/non-blocking
  - Kueue management-state blocking is tracked under `RHOAIENG-82351`, not this
    Ray gate
- Checks implemented in repo:
  - block when `RayCluster` objects still carry the
    `ray.openshift.ai/oauth-finalizer` and do not have the
    `odh.ray.io/pre-upgrade-backup-taken` annotation
- Detailed behavior:
  - the gate checks both supported API versions (`v1` first, then `v1alpha1`)
    and treats `NoMatch` as "resource type not present", not as a failure
  - a `RayCluster` with the finalizer but with the backup annotation already set
    is treated as migrated and does not block
  - a `RayCluster` without the finalizer is ignored by this gate even if other
    Ray migration concerns exist, because the Jira blocker narrowed the operator
    scope to the pre-upgrade backup acknowledgement
- Left out from Jira / odh-cli:
  - the Ray gate itself does not encode the Kueue `Unmanaged` prerequisite; that
    behavior is handled under `RHOAIENG-82351`
- Local-only related gate not covered by this Jira child:
  - the separate `removed-codeflare` repo gate now blocks only when the
    internal `CodeFlare` CR still exists

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
- Task status: `Done`
- Jira comment alignment: `Blocking spike item implemented; migration actions intentionally left out`
- Jira comments:
  - spike doc says the only blocking TrustyAI lint condition is
    `TrustyAIService.spec.storage.format == "PVC"`
  - the other TrustyAI spike items are advisory-only (`impacted workloads`,
    `DATABASE` storage, scheduled metrics backup)
- Checks implemented in repo:
  - block when any `TrustyAIService` uses `spec.storage.format: PVC`
- Detailed behavior:
  - the gate scans both `TrustyAIService` `v1` and `v1alpha1`, preferring the
    newer API when both CRDs are available
  - only the literal `PVC` storage format blocks; other storage modes are not
    treated as operator-side blockers here
  - malformed or unreadable `spec.storage.format` content fails the gate with an
    error because the operator cannot safely infer whether backup is required
- Left out from Jira / odh-cli:
  - advisory-only spike items (`DATABASE` storage, metrics backup scheduling,
    impacted workloads) are intentionally not encoded as operator blockers
  - odh-cli also carries active migration actions for Guardrails patching and
    GPU deadlock resolution; those are not lint-style passive blockers
- Notes:
  - the GPU-deadlock flow was reviewed, but it is currently hard to implement
    safely as an operator gate because it depends on exact TrustyAI/KServe
    runtime state and would naturally want mutation-style remediation

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
- Task status: `Done`
- Jira comment alignment: `Current operator scope now includes the clearly blocking odh-cli Authorino TLS check`
- Jira comments:
  - latest live Jira comments say the existing KServe checks are sufficient
  - Kueue-related follow-up was reviewed and does not currently require extra
    KServe-side gate logic
  - earlier odh-cli summary also mentioned Service Mesh / ModelMesh enablement,
    hardware-profile migration, and Authorino TLS readiness
- Checks implemented in repo:
  - block on `InferenceService` objects using
    `serving.kserve.io/deploymentMode=Serverless`
  - block on `InferenceService` objects using
    `serving.kserve.io/deploymentMode=ModelMesh`
  - block on `ServingRuntime` objects with `spec.multiModel=true`
  - block on `InferenceService` objects that reference removed runtimes
  - block when `LLMInferenceService` workloads exist but `Authorino` is missing,
    TLS is disabled/misconfigured, or the `Authorino` resource is not `Ready`
- Detailed behavior:
  - the `InferenceService` pass counts serverless, modelmesh, and removed
    runtime references in one read over the resource list, so a single object
    can contribute to multiple blocking categories
  - removed runtime detection is based on the predictor model runtime name and
    currently blocks `ovms`, `caikit-standalone-serving-template`, and
    `caikit-tgis-serving-template`
  - the `ServingRuntime` branch only blocks explicit `spec.multiModel=true`;
    missing or false values are treated as compatible
  - the Authorino branch only activates when `LLMInferenceService` objects are
    present; otherwise it stays out of the way for non-llm-d clusters
- Left out from Jira / odh-cli:
  - odh-cli also carries `dependencies.shared-ossm.shared-usage` and
    `dependencies.shared-serverless.shared-usage` checks, plus migration-style
    hardware-profile handling
  - those are not encoded in the repo's `kserve` gate because the final Jira
    follow-up accepted the narrower operator-side scope and the remaining items
    are shared-usage or migration concerns rather than direct KServe workload
    blockers
- Local-only related gate not covered by this Jira child:
  - the separate `removed-modelmeshserving` repo gate blocks when the legacy
    internal `ModelMeshServing` CR still exists
- Notes:
  - Service Mesh dependency handling also has a repo-local gate
    (`dependencies-servicemeshoperatorv2`) for the legacy OLM subscription
  - latest Jira follow-up did not request any new KServe-side gate beyond the
    currently implemented workload blockers

### `RHOAIENG-82361` Workbenches

- Jira status: `Review`
- Task status: `Done`
- Jira comment alignment: `Blocking image-reviewed checks implemented; advisory and user-action items remain out of scope`
- Jira comments:
  - the attached image evaluation marks these as operator-gate candidates:
    `hardware-profile-integrity`, `connection-integrity`,
    `hardwareprofile-migration`, `container-name-mismatch`, and
    `workloads.kueue.data-integrity`
  - the same image marks `accelerator-migration` as informational and
    `non-stopped-workloads` / `impacted-workloads` as not suitable for
    operator-side blocking because they require pre-upgrade user action
- Checks implemented in repo:
  - block when a `Notebook` already references a missing `HardwareProfile`
  - block when a `Notebook` references connection `Secret` objects that do not
    exist on the cluster
  - block when a Dashboard-managed `Notebook` has exactly one workload
    container and its name does not match the `Notebook` CR name
- Detailed behavior:
  - HardwareProfile integrity uses the explicit
    `opendatahub.io/hardware-profile-namespace` when present; otherwise it
    checks only the Notebook namespace for the blocking integrity rule
  - connection integrity parses comma-separated Secret references, supports both
    `name` and `namespace/name`, and flags the Notebook once any referenced
    Secret is missing
  - container-name mismatch is only evaluated for Dashboard-managed Notebooks
    (accelerator or size-selection annotations) and only when exactly one
    workload container remains after filtering legacy `oauth-proxy` sidecars
- Left out from Jira / odh-cli:
  - `hardwareprofile-migration` stays advisory/migration-only
  - `accelerator-migration` is informational only
  - `non-stopped-workloads` and `impacted-workloads` require explicit user
    action, so they are not a good fit for a passive operator blocker
  - `workloads.kueue.data-integrity` is intentionally left under the shared
    `kueue` gate rather than duplicated here
- Notes:
  - the `container-name-mismatch` blocker follows odh-cli semantics:
    Dashboard-managed Notebooks only, ignore legacy `oauth-proxy` sidecars,
    and only evaluate the single-workload-container shape
  - the `connection-integrity` blocker accepts either `namespace/name` or
    same-namespace Secret references, matching odh-cli parsing behavior
  - the `hardware-profile-integrity` blocker defaults to the Notebook namespace
    unless `opendatahub.io/hardware-profile-namespace` is explicitly set,
    matching odh-cli's blocking check rather than the broader migration search

### Local-only repo gates

- `dependencies-cert-manager`
  - blocks when the `openshift-cert-manager-operator` `Subscription` is missing
    from namespace `cert-manager-operator`
  - no matching Jira child issue exists in this list; this is a repo-local
    dependency prerequisite
- `dependencies-cert-manager` detail:
  - this is a strict presence check on the expected OLM subscription and does
    not currently inspect CSV health or channel/version skew
- `dependencies-kueue-operator`
  - blocks when Kueue is still `Managed` in the `DataScienceCluster` spec
  - blocks when Kueue is `Unmanaged` in the `DataScienceCluster` spec but the `kueue-operator`
    `Subscription` is missing
  - this is split out locally rather than tracked by a dedicated Jira child
- `dependencies-kueue-operator` detail:
  - the gate is intentionally state-aware: it does nothing when no
    `DataScienceCluster` exists, treats `Managed` as unsupported for the upgrade
    path, and only requires the subscription in the `Unmanaged` handoff case
- `dependencies-servicemeshoperatorv2`
  - blocks when a `Subscription` for the `servicemeshoperator` package still
    exists
  - no matching Jira child issue exists in this list; this is a repo-local
    dependency cleanup gate
- `dependencies-servicemeshoperatorv2` detail:
  - the gate keys off `Subscription.spec.name`, so custom Subscription resource
    names are supported and unrelated OLM packages do not trip it
- `removed-codeflare`
  - blocks when the internal `CodeFlare` CR still exists
  - this does not map cleanly to `RHOAIENG-82353`, but it no longer conflicts
    with the Ray Jira/odh-cli AppWrapper advisory
- `removed-codeflare` detail:
  - this is intentionally narrower than the previous implementation: the gate no
    longer scans `AppWrapper` CRs and now treats only the internal `CodeFlare`
    CR itself as blocking migration residue
- `removed-modelmeshserving`
  - blocks when the legacy internal `ModelMeshServing` CR still exists
  - this is related to KServe / ModelMesh cleanup but is not tracked by a
    matching Jira child issue in this file
- `removed-modelmeshserving` detail:
  - the check is a single object existence probe against the known cluster-scoped
    internal CR name; it does not inspect descendant ModelMesh resources

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

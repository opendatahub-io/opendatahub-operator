# DataScienceCluster v3 operator tasks

This is the repository-local task tracker for implementing DSC v3 under
RHOAIENG-94812. It embeds the relevant task descriptions, spike conclusions,
comments, dependencies, current code behavior, and acceptance criteria as of
**2026-09-24**. Jira, Google Docs, and wiki access is not required. Issue keys
are stable work-package identifiers and optional provenance only.

Read [development.md](development.md), [decisions.md](decisions.md), and
[PLAN.md](PLAN.md) before selecting a task. Accepted entries in `decisions.md`
are normative; proposed decisions are hard gates and must not be guessed.
The PLAN contains a cross-cutting high-risk file map, and every implementation
task contains indicative starting files, ownership boundaries, and test entry
points so an agent can begin without Jira discovery or a preliminary scan of
the entire repository.

## How an autonomous agent uses this tracker

1. Select the first numbered task with `status: not_started` whose `depends_on`
   tasks are complete and whose required decisions are all `Accepted` in
   `decisions.md`.
2. Read the task's embedded inputs, current repository state, steps, and
   definition of done. External issue reading is not an entry condition.
3. Change the numbered task file's status: `not_started` -> `in_progress` ->
   `completed`, and mirror it in this summary. Add branch/commit and
   verification evidence to that task's Outcomes.
4. If a required decision is pending, set `status: blocked`, add the decision
   ID to `blocked_by`, and proceed to another independent task. Do not invent a
   lifecycle status such as `Blocked (Gx)` or copy a provisional example into
   the public API.
5. When an approved decision is provided, record the exact normative contract
   in `decisions.md` first. Then update PLAN/TASKS summaries and clear the
   corresponding blocker. PLAN or TASKS never becomes a competing authority.
6. Preserve unrelated worktree changes and keep garbage collection last in
   every controller action chain.
7. For every v3 data-model change, update or explicitly confirm both conversion
   directions and add focused forward/backward/round-trip tests in the same
   change. Handler tests alone are insufficient.
8. A code task is not complete until required generated changes are included
   and its Outcomes contain the commands and results.

Legend:

- `✅ Complete`: definition of done and evidence recorded
- `🚧 In progress`: actively being implemented
- `📝 Todo`: ready when its listed gates/dependencies are satisfied
- `⛔ Blocked`: a required contract or delivery is unavailable
- `⚠️ Decision`: owner decision must be encoded locally before implementation
- `🚫 Out of scope`: recorded for clarity; do not implement here

## Status and dependency table

The external issues that existed at the 2026-09-18 snapshot were in `New`
state. Later Jira states are recorded in the corresponding task files. Local
status below, not external workflow state, controls agent execution.
In the Jira hierarchy, `RHOAIENG-96548` and `RHOAIENG-96549` are standard Tasks
directly under the `RHOAIENG-94804` Epic, not subtasks of `RHOAIENG-94812`.

| Work package | Local status | Blocked by | Repository output |
| --- | --- | --- | --- |
| [`DSC-V3-001`](tasks/001.md) v3 machinery and v1 removal | `✅ Completed` | None; Task 010 separately blocks upgrade release | Identical v3 API, v2 <-> v3 identity conversion, typed v3 runtime, and v1-free API |
| [`RHOAIENG-94805`](tasks/002.md) Dashboard contract | `✅ Completed` | None; DEC-027 | Freeze G1 and Dashboard parts of G7 |
| [`RHOAIENG-95339`](tasks/003.md) Data contract | `✅ Completed` | None; resolved 2026-09-22 | DEC-029; freeze G3 and Data parts of G7 |
| [`RHOAIENG-95340`](tasks/004.md) AI Hub contract | `✅ Completed` | None; DEC-025/DEC-028 | Freeze G2/G4 and AI Hub parts of G7 |
| [`DSC-V3-012`](tasks/012.md) KServe/MaaS migration ([`RHOAIENG-95857`](https://redhat.atlassian.net/browse/RHOAIENG-95857)) | `✅ Completed` | None | DEC-024 literal marker/normalization; implementation `25c80b63a` (following `dd8472bbc`), tests and outcomes in Task 012 |
| [`RHOAIENG-94809`](tasks/005.md) consolidated contract | `📝 Todo` | DSC-V3-014, DSC-V3-015, DSC-V3-016 | Complete the consolidated matrix after interim verification and the accepted removal outcomes; specify v2 retirement guards and v2/v3 conversion behavior |
| [`RHOAIENG-94814`](tasks/010.md) v1 retirement qualification | `⛔ Blocked` | odh-cli implementation, DEC-018 promotion control, DEC-020 execution matrix | Produce release evidence and qualify the gate/rollback boundary |
| [`RHOAIENG-95342`](tasks/007.md) Dashboard handler/CRD | `✅ Completed` | DEC-027; dashboard-specific API/conversion work included | Dashboard projection, status, conversion, and focused coverage |
| [`RHOAIENG-95344`](tasks/008.md) AI Hub handler/CRD | `✅ Completed` | Task 006 completed for current scope; final qualification in Task 011 | AI Hub projection, namespace conversion, status, and focused coverage; implementation `ecc43e106` |
| [`RHOAIENG-95346`](tasks/009.md) Data model/conversion | `✅ Completed` | Runtime projection remains separate handler work | v2/v3 Data model, conversion, and focused coverage |
| [`RHOAIENG-96167`](tasks/013.md) Platform CR naming alignment | `🚧 In progress` | DEC-026; DSC-V3-001 | Add Platform v1alpha2 `aiHub`/`data`, v1alpha1 conversion spoke, and preserve internal module identities |
| [`RHOAIENG-94812`](tasks/006.md) contract implementation | `✅ Completed for current scope` | Data Registry runtime projection deferred by user direction on 2026-09-23 | Accepted Dashboard, AI Hub, Data, and MaaS implementation is complete; DEC-033/DEC-034 authorize later deprecated-stanza removal via a separate validation/conversion task |
| [`RHOAIENG-96548`](tasks/015.md) LlamaStackOperator v3 removal decision and follow-up (standard Task under `RHOAIENG-94804`) | `🚧 In progress` | Separate follow-up Jira link | DEC-033 accepted; v2 guard, v3 removal, and conversion implemented; Jira-link follow-up remains |
| [`RHOAIENG-96549`](tasks/016.md) TrainingOperator v3 removal decision and follow-up (standard Task under `RHOAIENG-94804`) | `🚧 In progress` | Separate follow-up Jira link | DEC-034 accepted; v2 guard, v3 removal, and conversion implemented; Jira-link follow-up remains |
| [`DSC-V3-017`](tasks/017.md) reconcile records | `✅ Completed` | None | Current-state docs corrected; interim history preserved |
| [`DSC-V3-018`](tasks/018.md) simulated retirement migration | `🚧 In progress` | 017 complete | Envtest v2-storage-to-v3 simulation |
| [`DSC-V3-019`](tasks/019.md) conversion protocol and v2 RMW | `🚧 In progress` | 017 complete | Direct `/convert` and API-server integration evidence |
| [`DSC-V3-020`](tasks/020.md) Dashboard v3 E2E path | `🚧 In progress` | 017 complete | Correct v3 spec helper and focused regression test |
| [`DSC-V3-021`](tasks/021.md) retired-operator conversion E2E | `🚧 In progress` | 018 in progress | Fresh-install retirement coverage |
| [`DSC-V3-022`](tasks/022.md) Dashboard/AIHub v2 DSC component E2E | `🚧 In progress` | 019, 020 in progress | Tier3 component-suite smoke coverage |
| [`DSC-V3-023`](tasks/023.md) Data/AI Gateway v2 DSC component E2E | `🚧 In progress` | 019, 020 in progress | Tier3 component-suite smoke coverage |
| [`DSC-V3-024`](tasks/024.md) supported-upgrade retirement E2E | `⛔ Blocked` | 010, DEC-020 supported matrix | Pinned-source ODH/RHOAI upgrade evidence unavailable |
| [`DSC-V3-025`](tasks/025.md) CodeRabbit review findings from PR 4137 | `✅ Completed` | None | Detailed findings recorded with direct review conversation links |
| [`RHOAIENG-94812`](tasks/011.md) final qualification | `📝 Not started` | Waits for Tasks 006-010, 012, 015-024 | Integrate and qualify all targeted E2E additions |

```text
001-01 v3 API ─┬─> 001-02 atomic conversion/v1 removal ─┐
               └─> 001-03 runtime migration ──────────┼─> 001-04 artifacts ─> 001-05 integration
001-05 ──> 001 roll-up complete

002 Dashboard contract ─┐
003 Data contract ──────┼─> 005 consolidated G1-G7 matrix
004 AI Hub contract ────┘
001 complete ─> 014 interim Training/Llama API verification ────────────────┐
001 complete ─> 015 LlamaStackOperator removal decision/follow-up ─────────┼─> 005 consolidated matrix
001 complete ─> 016 TrainingOperator removal decision/follow-up ────────────┘

001 complete ─> 012 KServe/MaaS ─┐
005 complete ────────────────────┴─> 006 API/conversion ─┬─> 007 Dashboard projection ─┐
                                                        ├─> 008 AI Hub projection ────┼─> 011 final qualification
                                                        └─> 009 Data projection ──────┤
001 complete ──> 013 Platform naming alignment ──────────┘
001 complete ─> 010 future odh-cli qualification ─────────────────────────────────────┘
```

Task 001 is completed under DEC-023 without waiting for component contracts.
It copied the v2 shape to v3, added identity conversion, moved production code
to v3, and removed v1. The external odh-cli gate runs before
OLM installs that v1-free CRD, but its implementation and qualification are the
separate future Task 010 and do not block Task 001 implementation completion.
DEC-022/DEC-024 make Task 012's KServe/MaaS stanza change executable without further
Jira or component feedback. The Dashboard and Data shapes are accepted under
DEC-027 and DEC-029. Task 006 completes the accepted in-scope transforming
conversions. Training Operator and Llama Stack Operator were identity-mapped
only in the interim DEC-032 baseline. Current v3 omits both spec/status entries
under DEC-033/DEC-034; conversion drops them forward and reports `Removed` for
v2 spec/status on reverse. V2 remains retirement-only. Tasks 018/019/021/024
extend migration and cluster evidence. Data
Registry runtime projection remains explicitly deferred.
Platform naming is a separate accepted contract under DEC-026 and is tracked
by the in-progress Task 013; it does not change the DSC v2/v3 component gates.

## Numbered task execution order

Each file is a living execution record. Dependencies, not numeric order alone,
control concurrency.

`promotion_requires` is not a completion dependency: it prevents promotion of
an artifact after implementation completes. Task orchestrators must exclude it
from DAG cycle detection. Task 001 uses it to point to future Task 010.

| Task | Purpose | Depends on |
| --- | --- | --- |
| [001](tasks/001.md) | Roll up the initial machinery milestone | 001-01 through 001-05 |
| [001-01](tasks/001-01.md) | Capture baseline and introduce identical v3 API | None |
| [001-02](tasks/001-02.md) | Atomically switch hub/storage, add conversion/webhooks, and remove v1 | 001-01 |
| [001-03](tasks/001-03.md) | Migrate production runtime from typed v2 to typed v3 | 001-01 |
| [001-04](tasks/001-04.md) | Deprecate v2 and finalize v1-free generated artifacts | 001-02, 001-03 |
| [001-05](tasks/001-05.md) | Integrate and verify the machinery milestone | 001-02 through 001-04 |
| [002](tasks/002.md) | Freeze Dashboard public contract | Owner approval |
| [003](tasks/003.md) | Freeze Data public contract | Owner approval |
| [004](tasks/004.md) | Freeze AI Hub public contract | Owner approval |
| [005](tasks/005.md) | Consolidate G1-G7 and conversion matrix | 002-004, 012, and 014-016 |
| [006](tasks/006.md) | Implement accepted in-scope v3 API, conversion, and admissions | 001, accepted portions of 005, 012; Data Registry runtime follow-up deferred |
| [007](tasks/007.md) | Implement Dashboard projection/status | 006 |
| [008](tasks/008.md) | Implement AI Hub projection/status | 006 |
| [009](tasks/009.md) | Implement Data projection/status | 006 |
| [013](tasks/013.md) | Align Platform CR module names with DSC v3 | 001, 008, 009, DEC-026 |
| [014](tasks/014.md) | Verify interim Training Operator and Llama Stack Operator v3 stanzas | 001, DEC-032 |
| [015](tasks/015.md) | Record LlamaStackOperator removal decision and track validation/conversion follow-up | 001; [RHOAIENG-96548](https://redhat.atlassian.net/browse/RHOAIENG-96548) |
| [016](tasks/016.md) | Record TrainingOperator removal decision and track validation/conversion follow-up | 001; [RHOAIENG-96549](https://redhat.atlassian.net/browse/RHOAIENG-96549) |
| [017](tasks/017.md) | Reconcile retired-operator records | None |
| [018](tasks/018.md) | Simulate retirement across storage migration | 017 |
| [019](tasks/019.md) | Complete ConversionReview and v2 read-modify-write integration | 017 |
| [020](tasks/020.md) | Repair Dashboard v3 E2E state updates | 017 |
| [021](tasks/021.md) | Expand retired-operator conversion E2E | 018 |
| [022](tasks/022.md) | Add Dashboard/AIHub v2 DSC component E2E | 019, 020 |
| [023](tasks/023.md) | Add Data/AI Gateway v2 DSC component E2E | 019, 020 |
| [024](tasks/024.md) | Prove retirement in a supported real upgrade | 010, 018 |
| [010](tasks/010.md) | Future: qualify odh-cli gate, storage migration, and rollback | 001 plus external gate |
| [011](tasks/011.md) | Integrate and qualify the final release | 006-010, 012, 015-024 |
| [012](tasks/012.md) | Migrate legacy KServe MaaS to canonical v3 AI Gateway MaaS | 001 only; [MaaS e2e scenarios](../../tests/e2e/webhooks/) |

## Completion evidence template

Copy this block into the active task and fill it in before marking complete:

```text
Implementation:
- Branch/commit:
- Accepted decision IDs used:
- Main changed areas:

Verification:
- make generate manifests api-docs:
- make fmt:
- make lint:
- make unit-test:
- make build:
- focused tests:
- git diff --check:

E2E scenarios handed to final Task 011:
-
```

## Mandatory checklist for every future v3 data change

Use this checklist for any addition, removal, move, rename, type change, JSON
tag, optionality, default, validation, status field, or condition change under
the v3 DSC API. All boxes must be satisfied in the same change:

- [ ] Record the v2 field path/type and the new v3 field path/type.
- [ ] Define and implement `v2 -> v3` for populated, absent, empty, and legacy
      inputs.
- [ ] Define and implement `v3 -> v2`, including lossless preservation through
      a v2 read-modify-write when v2 cannot represent the value directly.
- [ ] Add focused direct-conversion tests in both directions.
- [ ] Add `v2 -> v3 -> v2` and `v3 -> v2 -> v3` round-trip tests.
- [ ] Cover changed status fields and condition names, not only spec fields.
- [ ] Review generated v2/v3 schemas for changed fields and cover each intended
      difference with focused direct, round-trip, and API-server conversion tests.
- [ ] List and test any accepted v2 wire-contract change separately; DEC-035
      removed the automated schema baseline and difference registry.
- [ ] Update defaulting, validation, generated CRDs, samples, and API docs when
      the wire contract changes.
- [ ] Run the focused conversion suite and mandatory repository gates and
      record the results in the task evidence.

If converter code remains unchanged because the new data uses an existing
explicit identity-copy path, add a test for the new field and record that
decision. “No converter change required” never means “no conversion test
required.”

For MaaS, DEC-024 defines explicit normalized round-trip fixtures: canonical C
and AI Gateway parent A stay projected, original C/A are not restored, legacy
Managed survives iff marked, and legacy Removed remains Removed. Record the
expected result and rationale for each normalization and marker retirement case;
reject unexplained differences. Unrelated metadata remains exact.

## First executable work package

### `DSC-V3-001`: establish v3 machinery

- Local status: `Completed` under DEC-023; see Task 001 Outcomes
- Blocked by: nothing
- Must not include: any unresolved Dashboard, AI Hub, Data, MaaS, Training
  Operator, or Llama Stack schema/behavior change

Detailed executable instructions, lifecycle metadata, acceptance criteria, and
implementation outcomes are maintained in [tasks/001.md](tasks/001.md). That
file is the task execution record; this section is its summary.

Purpose:

- Establish the final versioning architecture before component contracts are
  resolved. V3 initially has exactly the v2 API shape, making v2/v3 conversion
  a semantic no-op. Move the operator to typed v3 and remove v1 from the new
  code/artifacts. Odh-cli work is deferred to separate Task 010.

Terminology:

- The new operator converter is **v2 <-> v3**. It does not need a v1 <-> v3
  converter. Future Task 010 qualifies the external gate that completes v1 ->
  v2 storage migration while the old operator is still installed.

#### Step 1: introduce an identical v3 API

- Add `api/datasciencecluster/v3/groupversion_info.go` for version `v3`.
- Add v3 `DataScienceCluster`, list, spec, status, and component types by
  reproducing the current v2 public shape exactly, including deprecated fields,
  JSON tags, markers, status entries, release fields, conditions, related
  objects, and error message.
- Do not apply any provisional component proposal. In particular, v3 still
  uses the current v2 Dashboard, `modelregistry`, `feastoperator`, legacy MaaS,
  `trainingoperator`, and `llamastackoperator` fields in this milestone.
- Leave v2 as the temporary hub/storage version during 001-01. In 001-02, move
  `+kubebuilder:storageversion` and `conversion.Hub` to v3 atomically with v2
  conversion activation and v1 deletion.
- Generate v3 deepcopy/object code.
- Register v3 in `PROJECT`, `cmd/main.go`, webhook/envtest schemes, and any
  scheme builders or object factories that enumerate DSC versions.
- Change `cmd/component-codegen/cmd/generator/generator.go` so scaffolding uses
  the v3 DSC types file.
- Keep v3 top-level types version-owned. Do not edit shared component types to
  create v3 behavior; introduce v3-owned nested types when a stanza diverges.

#### Step 2: add no-op v2 <-> v3 conversion

- Make v2 the conversion spoke and implement `ConvertTo`/`ConvertFrom` against
  the v3 hub.
- Copy complete object metadata, spec, and status without renaming, dropping,
  defaulting, validation, normalization, or cluster calls. The implementation
  must use explicit typed copy/conversion code so later accepted mappings have a
  clear extension point; do not use JSON serialization or `unsafe`. Equality
  of externally observable fields is the acceptance contract.
- Add whole-object round-trip tests with every component/status populated,
  conditions, releases, related objects, annotations/labels/finalizers,
  empty/Managed/Removed management states, optional values, and deprecated
  fields.
- Assert both `v2 -> v3 -> v2` and `v3 -> v2 -> v3` semantic equality and
  conversion idempotence, ignoring only the expected target-version
  `TypeMeta`/GVK representation.
- Add direct one-hop equality tests so mutually inverse but incorrect mappings
  cannot pass only through round-trip cancellation.
- Move controller-runtime `/convert` registration to the v2 spoke, use distinct
  v2/v3 GVK constants, and test each version's admissions independently.
- Delete the v1 API/converter in this same compiling change because the current
  v1 converter asserts that v2 is the hub. Do not add a temporary v1/v3
  converter.
- Generate `conversionReviewVersions: [v1]`; it is the Kubernetes protocol
  version and must not contain DSC versions `v2` or `v3`.

#### Step 3: amend production code to use v3

- Replace typed v2 usage in `internal/controller/datasciencecluster` and its
  reconciliation/status tests.
- Replace v2 in `internal/controller/components/registry/registry.go` and every
  in-tree component handler signature/fixture.
- Replace v2 in `internal/controller/modules/types.go`, `base.go`, module
  registry/lifecycle/status code, and every module handler/test.
- Replace v2 in status helpers, readiness aggregation, predicates,
  initial-install helpers, comparison utilities, upgrade helpers, E2E object
  factories, and component-codegen.
- Preserve behavior because v3 is shape-identical. Do not opportunistically
  rename fields or change defaults/validation.
- Keep garbage collection last in every touched action chain.

After migration, this search must return only conversion, v2 admission, and
explicit compatibility tests, with each remaining production hit justified in
the completion record:

```bash
rg -n 'datasciencecluster/v2|dscv2' api cmd internal pkg tests -g '*.go'
```

#### Step 4: remove v1 and hand off upgrade gating

- Do not implement or qualify odh-cli in Task 001. Future Task 010 owns the
  DEC-009 gate and must complete before the v1-free CRD is promoted or installed
  as an upgrade.
- Move shared conversion webhook registration to v2 before deleting the v1
  registration path.
- Remove the v1 API, converter, scheme, admissions, `PROJECT` entries,
  generated serving, fixtures, samples, and obsolete version-specific tests.
- Preserve useful compatibility behavior in v2/v3 tests rather than deleting
  it with v1. Delete the AI Gateway conversion annotation only if it has no
  remaining consumer.
- Record the v1-free artifacts and assumptions handed to Task 010. Task 010
  owns stored-version migration, OLM ordering, retry, and rollback tests.

#### Step 5: amend admissions, tests, and generated assertions

- Add v3 defaulting/validation registration by porting the existing v2 rules
  unchanged, because the schemas are identical. Retain v2 admissions for v2
  clients and identify v2 as deprecated in supported warnings/documentation.
- Cover v2/v3 conversion through API and webhook unit/integration tests. The
  structural OpenAPI guard added during Task 001 was later removed by DEC-035.
- Update unit/envtest/integration tests so the controller operates on v3 and a
  v2 API request converts losslessly to/from v3 storage.
- Assert the generated DSC CRD has exactly:
  - v2: `served: true`, `storage: false`, `deprecated: true`, with DEC-019's
    exact `deprecationWarning` literal;
  - v3: `served: true`, `storage: true`;
  - no v1 entry;
  - webhook conversion path `/convert`; and
  - `conversionReviewVersions: [v1]`.
- Update the primary ODH/RHOAI DSC samples to `apiVersion:
  datasciencecluster.opendatahub.io/v3` without changing their component
  stanza shapes.
- Record for Task 011 that final E2E must actually verify DSC v2 API requests
  against v3 storage; the existing `tests/e2e/v2tov3upgrade_test.go` name alone
  is not evidence.

Definition of done:

- V3 and v2 schemas are structurally identical in generated CRDs.
- V3 is the hub, only storage version, and only typed DSC version used by
  production reconciliation.
- V2 is served but deprecated, with lossless identity conversion and compatible
  admissions.
- V1 code, conversion, admission, scheme, and generated serving are absent.
- The artifact is explicitly ineligible for upgrade promotion until Task 010
  qualifies the external odh-cli gate; that qualification is not Task 001 work.
- The structural coverage guard proves that v2/v3 are identical and establishes
  the required inventory for future differences.
- No post-machinery component contract, including accepted G5, is implemented
  inside Task 001; Task 012 owns G5 after Task 001 completes.
- All focused tests and mandatory repository gates pass, and completion
  evidence is recorded.

## Independent accepted migration

### `DSC-V3-012`: migrate KServe MaaS to AI Gateway

- Local status: `Completed`; Task 001 prerequisite completed
- Dedicated Jira: [`RHOAIENG-95857`](https://redhat.atlassian.net/browse/RHOAIENG-95857);
  broader implementation issue: [`RHOAIENG-94812`](https://redhat.atlassian.net/browse/RHOAIENG-94812)
- Assignee/start: Codex, 2026-09-18, branch `RHOAIENG-94812-DSC-v3`,
  starting HEAD `75c2018d4`; implementation commit `25c80b63a` (following
  `dd8472bbc`), completed
  2026-09-18 with verification and behavior examples in the Outcomes
- Detailed task and outcome record: [tasks/012.md](tasks/012.md)
- Executable MaaS scenarios: [tests/e2e/webhooks/](../../tests/e2e/webhooks/)
- Decision authority: accepted DEC-022 forward precedence and DEC-024
  preservation/admission; no further Jira or component-owner
  feedback required
- Completed input to Task 006 integration and final Task 011

Purpose:

- Remove deprecated `kserve.modelsAsService` only from v3 while retaining the
  field and its update guard in served v2; remove the custom MaaS field warning
  from both versioned admissions.
- Convert legacy v2 input to canonical `aigateway.modelsAsAService`: canonical
  v2 `Managed` is preserved; legacy `Managed` selects canonical `Managed` only
  when `kserve.managementState` is `Managed`. A disabled KServe parent leaves
  canonical MaaS `Removed`, while both public v2 MaaS fields remain defaulted
  and the contract does not include absent or empty MaaS object shapes.
- Preserve legacy intent. Set reserved
  `conversion.opendatahub.io/maas-v2-state` to literal `legacy-managed` when
  legacy MaaS is `Managed` and canonical MaaS is `Removed`, including when a
  Removed or empty KServe parent gates effective MaaS off. Ignore incoming
  markers and reset from current C/L. Reverse keeps C/A migrated and returns
  legacy Managed iff marked, otherwise Removed.
- V3 UPDATE trusts `OldObject`, preserves unrelated and parent edits, and
  permanently deletes the marker on canonical MaaS management-state change,
  even after a revert. Native CREATE strips supplied reserved metadata; reject
  unknown marker values. Reuse componentApi value types without canonical
  pointers or duplicated AI Gateway wrappers. Original C/A/absence are not
  restored; there is no JSON payload or snapshot and OpenAPI remains unchanged.
- Remove obsolete v3 runtime/admission fallback, register the OpenAPI
  difference, and land direct, both-round-trip, `/convert`, handler, schema,
  CEL, generated-artifact, and envtest integration coverage. Record the final
  E2E scenario for Task 011. Only E2E compile fixture fixes belong to Task 012.
- Assume no supported installed intermediate-v3 release with the legacy field;
  supported legacy input enters through v2. Task 010 retains upgrade gating.

This task is deliberately separate from the blocked consolidated contract. It
must not alter Dashboard, AI Hub/Model Registry, Data/Feast, Training Operator,
Llama Stack Operator, or their pending decisions.

## Contract work packages

### `RHOAIENG-94805`: freeze the Dashboard v3 contract

- Local status: `Completed; DEC-027`
- Detailed task and outcome record: [tasks/002.md](tasks/002.md)
- Decision owners: Dashboard, Workbenches, and Platform API owners
- Unblocks: G1, Dashboard parts of G7, `94809`, and `95342`

Purpose:

- Produce one exact public Dashboard v3 spec/status/condition contract while
  preserving the behavior of both current v2 lifecycle controls.

Embedded source record:

- The accepted v3 shape is `dashboard.standard` for the core Dashboard and
  `dashboard.maasPortal` for the independently managed portal. The parent is
  structural and has no management state.
- The latest RHOAIENG-94805 comment (18567363, 2026-09-22) records agreement
  among Dashboard, Workbenches, and Platform participants.

Current repository state:

- `api/components/v1alpha1/dashboard_types.go` defines canonical v3
  `DSCDashboard` with independent `standard` and `maasPortal` children, plus
  `DSCDashboardV2` for the legacy v2 shape; the portal defaults to `Removed`.
- `internal/controller/modules/dashboard/handler.go` enables the Dashboard
  operator when either core Dashboard or portal is managed.
- The handler projects the current Dashboard spec plus component maps,
  notebooks/model-registry namespaces, and gateway domain into the existing
  module CR.
- It mirrors module condition `MaaSConsumerPortalAvailable` to the same DSC
  condition and status field `MaaSConsumerPortal`.

Accepted decision record:

- `dashboard.standard.managementState` maps to/from v2
  `dashboard.managementState`.
- `dashboard.maasPortal.managementState` maps to/from v2
  `dashboard.maasConsumerPortal.managementState`.
- Both children are independent; all four Managed/Removed combinations are
  valid, and empty is effectively Removed.
- Existing status fields, `MaaSConsumerPortalAvailable`, aggregation,
  readiness, removal, defaults, and internal Dashboard CR projection are
  preserved.
- Update/validation rules and the behavior of a v2 read-modify-write.

Definition of done:

- G1 and the Dashboard portion of G7 have accepted decisions with exact shapes,
  mappings, approval date, and owner evidence in DEC-027.
- No proposed Dashboard name remains in the normative contract.
- `95342` can implement every state without making another product decision.

### `RHOAIENG-95339`: freeze the Data, Feature Store, and Data Registry contract

- Local status: `Completed; DEC-029; Jira resolved 2026-09-22`
- Detailed task and outcome record: [tasks/003.md](tasks/003.md)
- Decision owners: Data, Feast, Data Registry, and Platform API owners
- Unblocks: G3, Data parts of G7, `94809`, and `95346`

Accepted contract shape (implemented by DSC-V3-009):

```yaml
# v2 compatibility
components:
  feastoperator:
    managementState: Managed
    dataRegistry:
      managementState: Managed
```

```yaml
# v3
components:
  data:
    featureStore:
      managementState: Managed
    dataRegistry:
      managementState: Managed
```

Implementation and open-decision record:

- DSC-V3-009 implements `data` as a structural group with no parent state and
  independent Feature Store and Data Registry lifecycle fields. Its two
  explicit v2 fields map directly, so the implementation adds no annotation.
- An older consolidated proposal gave `data` a parent state whose `Removed`
  value overrode its children. The accepted contract is structural and does
  not use that behavior; both children are independent.
- The earlier spike proposed an annotation stash for v3-only Data Registry
  state. The accepted contract instead adds an explicit v2 compatibility field;
  no annotation is required.
- DCH and internal Data/Feast CR and metadata renames are deferred beyond 3.6.

Current runtime boundary:

- V2 `Components` exposes `feastoperator`, now including its compatibility
  `dataRegistry` child; v3 exposes `data.featureStore` and `data.dataRegistry`.
- `internal/controller/modules/feastoperator/handler.go` enables the existing
  Feast module from that state.
- Its internal FeastOperator CR spec currently projects only external-OIDC
  issuer configuration derived from GatewayConfig. It does not project Feature
  Store or Data Registry lifecycle.

Accepted decision record:

- `DEC-029` records the exact v2 compatibility and v3 YAML shapes above.
- `data` has no parent management state; child lifecycle is independent.
- Managed, Removed, empty, and absent values map directly in both directions.
- The explicit v2 compatibility field makes conversion lossless without an
  annotation.

Definition of done:

- Owner approval has closed G3 and the Data portion of G7; the implemented
  structural shape and independent-child lifecycle are normative.
- The approved v2 compatibility mechanism is exact and lossless; if the
  implemented direct mapping is not approved, specify the required alternative.
- Any future runtime projection task has an approved lifecycle/status contract
  and does not add DCH or rename internal resources.

### `RHOAIENG-95340`: freeze the AI Hub/Model Registry contract

- Local status: `Completed; DEC-025 and DEC-028`
- Detailed task and outcome record: [tasks/004.md](tasks/004.md)
- Decision owners: AI Hub, Model Registry, and Platform API owners
- Unblocks: G2, G4, AI Hub parts of G7, `94809`, and `95344`; DEC-028's
  namespace field-name follow-up is implemented in Task 008.

Latest proposal:

```yaml
# v2
components:
  modelregistry:
    managementState: Managed
    registriesNamespace: model-registry
```

```yaml
# v3
components:
  aiHub:
    managementState: Managed
    applicationNamespace: model-registry
```

Embedded source record:

- The latest component proposal requires public name `aiHub` and excludes both
  `hub` and `modelregistry` from v3.
- The older epic/consolidated description uses `hub`; that is a conflict, not
  an alternative an implementation agent may choose.
- Internal module CR, GVK, manifest directory, and metadata naming stay as-is
  for 3.6; their convergence is deferred to 3.7.

Current repository state:

- V2 uses `modelregistry` in spec/status and condition `ModelRegistryReady`;
  v3 uses `aiHub` and `AIHubReady`.
- When Model Registry is managed with an empty `registriesNamespace`, v2
  defaulting chooses `odh-model-registries` for ODH or
  `rhoai-model-registries` for RHOAI.
- Validation keeps v2 `registriesNamespace` immutable while Model Registry is
  managed; v3 exposes the mapped field as `applicationNamespace`.
- `internal/controller/modules/modelregistry/handler.go` manages internal GVK
  `AIHub`, CR name `default-aihub`, module/manifest name `modelregistry`, and
  projects v3 `applicationNamespace` to internal `instancesNamespace`.
- If the public namespace is empty, the handler falls back to the applications
  namespace. It also mirrors the namespace into legacy DSC status.

Accepted decision record:

- V3 uses `aiHub.applicationNamespace`; `hub`, `modelregistry`, and
  `registriesNamespace` are not v3 public names.
- V2 `registriesNamespace` maps directly to v3 `applicationNamespace` in spec
  and status; `ModelRegistryReady` maps to `AIHubReady`.
- Existing namespace optionality, build-specific defaults, fallback,
  validation, immutability, update behavior, and Removed/empty semantics are
  retained.

Definition of done:

- G2/G4 and the AI Hub portion of G7 have accepted decisions with an exact
  bidirectional matrix in DEC-025/DEC-028.
- Public and internal names are clearly separated.
- `95344` implemented the handler using the accepted defaults and status
  names; see [tasks/008.md](tasks/008.md) for its outcomes.

### `RHOAIENG-94809`: freeze the consolidated API and conversion matrix

- Local status: `Todo`; Jira resolved 2026-09-23 and is not being reopened.
  G1-G6 are accepted; Training Operator/Llama Stack implementation pieces of
  G7 remain open pending a separate validation/conversion task. G5 is accepted
  as DEC-022/DEC-024 and implemented independently by Task 012.
- Detailed task and outcome record: [tasks/005.md](tasks/005.md)
- Decision owners: Platform plus all affected component API owners
- Downstream: final qualification in `94812`/Task 011; component implementation
  tasks already record their scoped outcomes.

Purpose:

- Merge component decisions into one authoritative contract and eliminate
  contradictions before types, conversion, or generated CRDs encode them.

Inputs already captured locally:

- All accepted decisions, including DEC-017, DEC-019, and DEC-035, plus the
  complete proposals/discrepancies in PLAN. DEC-018 and DEC-020 apply only to
  future Task 010/release work.
- Dashboard decision record from `94805`.
- Data decision record from `95339`.
- AI Hub decision record from `95340`.
- Accepted MaaS behavior comes from DEC-022/DEC-024 and Task 012; import its
  marker/normalization matrix now and implementation evidence when complete,
  without reopening the policy.
- Interim deprecated v3 stanzas under DEC-032 were removed from current v3 by
  DEC-033/DEC-034; `ogx` remains separate. V2 compatibility fields are
  retirement-only. Conversion drops retired fields forward and reports
  `Removed` in v2 spec/status on reverse.

Required matrix columns for every changed field:

| Field/condition | v2 JSON/type | v3 JSON/type | v2 -> v3 | v3 -> v2 | absent/empty/default | validation/update | fidelity mechanism |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Dashboard core | `dashboard.managementState` | `dashboard.standard.managementState` | Direct | Direct | Empty is Removed | Existing validation/update rules | No fidelity annotation; direct child mapping |
| Dashboard portal | `dashboard.maasConsumerPortal.managementState` | `dashboard.maasPortal.managementState` | Direct | Direct | Removed default; empty is Removed | Existing validation/update rules | No fidelity annotation; direct child mapping |
| Model Registry/AI Hub | `modelregistry.registriesNamespace`; `ModelRegistryReady` | `aiHub.applicationNamespace`; `AIHubReady` | Direct field/condition mapping | Direct field/condition mapping | Existing build-specific default and Removed/empty semantics | Existing validation and immutability | No fidelity annotation; explicit namespace rename |
| Feast/Feature Store | `feastoperator.managementState` | `data.featureStore.managementState` | Direct | Direct | Empty is effectively Removed; no added default or parent state | No new field-specific validation/update rule | No fidelity annotation |
| Data Registry | `feastoperator.dataRegistry.managementState` compatibility field | `data.dataRegistry.managementState` | Direct | Direct | Empty is effectively Removed; no added default or parent state | No new field-specific validation/update rule | Explicit v2 compatibility field; no annotation |
| MaaS | From DEC-022/DEC-024 | From DEC-022/DEC-024 | Canonical v2 C=Managed is preserved; legacy L=Managed selects v3 Managed only when K=Managed; marker is emitted for C=Removed/L=Managed even when K gates effective MaaS off | Keep v3 C/A; L=Managed iff marker, else Removed | Reuse component types; C/L default to Removed; no absent/empty object contract | Remove custom warning; retain CEL; canonical MaaS edits retire marker and parent edits preserve it | `conversion.opendatahub.io/maas-v2-state: legacy-managed`; no JSON/snapshot; Task 012 |
| Training Operator | Deprecated `trainingoperator` spec/status retained for v2 compatibility | No v3 `trainingoperator` spec/status entry | Drop spec/status | Report `Removed` in spec/status | Empty is effectively `Removed`; no re-enable | DEC-034; existing v2 `optionalOldSelf` CEL guard | No marker; retirement is lossy by design |
| Llama Stack Operator | Deprecated `llamastackoperator` spec/status retained for v2 compatibility | No v3 `llamastackoperator` spec/status entry; `ogx` remains separate | Drop spec/status | Report `Removed` in spec/status | Empty is effectively `Removed`; no re-enable | DEC-033; v2 `optionalOldSelf` CEL guard | No marker; retirement is lossy by design |
| Changed status/conditions | current names | final names | Required | Required | Required | n/a | Required |

Accepted removal decisions and follow-up requirements:

- DEC-033 / Task 015 / RHOAIENG-96548 removes the deprecated
  `llamastackoperator` spec/status from v3; OGX remains separate.
- DEC-034 / Task 016 / RHOAIENG-96549 removes the deprecated
  `trainingoperator` spec/status from v3 shipped in 3.6 GA.
- V2 retirement CEL, v3 field removal, and direct/round-trip conversion are
  implemented. Tasks 018/019/021/024 add migration, API-server, and E2E
  evidence; Tasks 015/016 retain their separate Jira-link follow-up.

Definition of done:

- G1-G7 have accepted decisions recorded in `decisions.md`; Training Operator
  and Llama Stack v2 guards, v3 removals, and direct conversion/tests are
  implemented. Tasks 018/019/021/024 provide additional migration evidence.
- The completed matrix has no “TBD”, implied mapping, unnamed condition, or
  unspecified collision.
- Each round-trip has a stated fidelity mechanism and test case.
- Every intentional v2/v3 structural difference has an inventory entry and
  named forward, backward, and round-trip test cases.
- Version-specific type ownership prevents accidental changes to v2/internal
  CR schemas.

### `RHOAIENG-94814`: qualify v1 removal for release

- Local status: `Future; blocked by odh-cli implementation, DEC-018, and DEC-020`
- Detailed task and outcome record: [tasks/010.md](tasks/010.md)
- Decision owners: Platform release/OLM and operator upgrade owners
- Release dependency: the external odh-cli gate must be available and pass
  qualification before shipping the v1-removing generated CRD as an upgrade
- Promotion dependency: DEC-018 must name an enforceable release/CI control;
  documentation alone must not permit publication
- Execution dependency: DEC-020 must enumerate exact supported sources,
  artifacts, clusters, permissions, odh-cli invocation, and CI jobs locally

Non-negotiable Kubernetes constraint:

- An API version must not be removed from CRD `spec.versions` while it appears
  in `status.storedVersions`. Marking v3 as storage does not rewrite existing
  objects.

Current repository inventory:

- `api/datasciencecluster/v1` contains the API, v1-to-v2 conversion, and tests.
- `cmd/main.go` registers v1 and v2 schemes.
- `PROJECT` declares v1/v2 API, defaulting, and validation metadata.
- `internal/webhook/webhook.go` and
  `internal/webhook/datasciencecluster/v1` register and implement v1
  admissions; shared envtest helpers also register v1.
- ODH/RHOAI generated CRDs and conversion patches serve v1/v2 and store v2.
- The current v1 conversion owns compatibility behavior for AI Gateway MaaS,
  deprecated KServe MaaS, portal status, and the
  `conversion.opendatahub.io/aigateway-state` annotation.
- V1/v2 references also exist in tests, samples, compare utilities, upgrade
  checks, and bundle/CSV generated content.

Accepted odh-cli gate contract:

1. Run before OLM applies the new CRD, against the installed v1/v2 CRD while
   the old operator and its conversion webhook are healthy.
2. Read the CRD and `status.storedVersions`. Mutation-free success is allowed
   only when v1 is already absent.
3. If v1 is recorded and no DSC exists, skip object migration but patch the CRD
   status to remove v1 and verify it is absent.
4. If v1 is recorded and the singleton exists, read and update the DSC through
   v2 without changing its spec, status, or metadata, then verify it no longer
   depends on v1 storage.
5. Only after verification, patch the CRD status subresource to remove v1 from
   `status.storedVersions`; read it back and require v1 to be absent.
6. Block the upgrade on an unsupported source, conversion/webhook failure,
   concurrent-conflict exhaustion, failed verification, or failed status
   update. Every partial state must be safe to retry.
7. Allow OLM to apply the v1-free CRD only after the gate succeeds. The new
   CRD serves deprecated v2 and v3 and stores only v3.
8. After `/convert` through v2 is healthy, update the DSC through v3, verify v3
   storage, and remove v2 from `status.storedVersions` only when no v2-stored
   object remains. Continue serving v2.
8. Report the last safe rollback point. Before the v3 rewrite, rollback to the
   v1/v2 release is supported. Afterwards, require a tested v3 -> v2
   down-migration or declare/block rollback as unsupported.

Qualification work:

- Verify the gate is positioned before OLM CRD replacement for every supported
  ODH and RHOAI source-release path.
- Test no DSC, v1-stored, already migrated, interrupted/retried, webhook down,
  object conflict, status-patch failure, and verification-failure cases.
- Verify the new operator never needs v1 code and registers `/convert` through
  v2 before the post-upgrade v3 rewrite.
- Exercise both supported and deliberately blocked rollback paths.

Definition of done:

- The odh-cli gate implementation matches accepted DEC-009 and is available in
  the supported upgrade workflow before the v1-free CRD can be applied.
- Fresh install, supported upgrade, interrupted retry, already-migrated, and
  final `status.storedVersions` tests are specified.
- The design never depends on removing conversion support before stored data
  no longer needs it.

## Component delivery work packages

### `RHOAIENG-95342`: implement the Dashboard handler and module CRD

- Local status: `Not started; waits for DSC-V3-006`
- Detailed task and outcome record: [tasks/007.md](tasks/007.md)
- Out of scope: renaming the existing internal Dashboard CR or metadata

Implementation steps after unblocking:

1. Add/use version-specific public Dashboard types matching the accepted shape;
   do not mutate shared v2 types unintentionally.
2. Update `internal/controller/modules/dashboard` to read typed v3 fields while
   preserving its existing component maps, namespace, gateway, and module-CR
   projection behavior.
3. Preserve operator enablement when any accepted Dashboard subcomponent needs
   the shared operator; implement independent child state exactly as specified.
4. Mirror the accepted status fields and source/DSC condition types and keep
   readiness aggregation correct for removed children.
5. Update the module CRD/schema input and handler/schema-compliance tests.
6. Add v2 compatibility round-trip tests for the existing top-level Dashboard
   and `maasConsumerPortal` states.

Definition of done:

- Every accepted Dashboard state maps to the expected operator lifecycle, module
  CR, status, and conditions.
- Existing v2 Dashboard behavior is unchanged through conversion.
- The mandatory future-v3-change checklist is complete for every Dashboard
  spec/status/condition difference.
- Focused unit/schema tests and mandatory generated gates pass; evidence is
  recorded using the template.

### `RHOAIENG-95344`: implement the AI Hub handler and module CRD

- Local status: `Completed; implementation commit ecc43e106`
- Detailed task and outcome record: [tasks/008.md](tasks/008.md)
- Out of scope: internal CR/GVK/module metadata rename (`95349`, deferred 3.7)

Completed implementation:

1. Add/use the accepted public AI Hub type and v3 `applicationNamespace` field while keeping the
   existing internal GVK `AIHub`, CR `default-aihub`, and module name
   `modelregistry`.
2. Update `internal/controller/modules/modelregistry` to read v3 public state,
   project `applicationNamespace` to internal `instancesNamespace`, and apply
   only the accepted fallback/default rules.
3. Update ready-condition and DSC status mapping to the accepted public names;
   retain legacy status mirroring only where the matrix requires it.
4. Preserve platform-module enablement, application namespace, gateway domain,
   releases, and management-state annotation behavior.
5. Add bidirectional namespace/status/condition conversion cases and handler,
   schema-compliance, and webhook E2E coverage for build defaults and custom
   namespaces.

Definition of done:

- Public v3 naming does not leak into the unchanged internal resource identity.
- V2 Model Registry and v3 AI Hub objects produce equivalent internal behavior.
- V2 `registriesNamespace` maps to v3 `applicationNamespace` in spec and
  status; `ModelRegistryReady` maps to `AIHubReady` and back.
- Status/conditions/defaults match the accepted matrix; focused tests,
  generation, formatting, lint, and webhook E2E evidence are recorded in
  [tasks/008.md](tasks/008.md).
- The mandatory future-v3-change checklist is complete for every AI Hub
  spec/status/condition difference.

### `RHOAIENG-95346`: implement the Data model and conversion

- Local status: `Completed for the agreed model/conversion scope`
- Detailed task and outcome record: [tasks/009.md](tasks/009.md)
- Runtime Feast/Data Registry projection remains separate handler work. DCH
  and internal Data/Feast CR or metadata rename
  (`95350`) remain out of scope.

Completed implementation:

1. Add/use the accepted `data`/Feature Store/Data Registry public types and the
   approved v2 compatibility field without changing unrelated schemas.
2. Add explicit v2 <-> v3 conversion and focused conversion/admission tests.

Deferred follow-up:

- Update `internal/controller/modules/feastoperator` only after the independent
  child lifecycle and status contract is accepted.

## `RHOAIENG-94812`: implement and deliver DSC v3

- Local status: `Task 006 completed for current scope; final qualification remains`
- Detailed task records: [machinery task 001](tasks/001.md),
  [KServe/MaaS task 012](tasks/012.md),
  [MaaS e2e scenarios](../../tests/e2e/webhooks/),
  [API/conversion task 006](tasks/006.md), and
  [final qualification task 011](tasks/011.md)
- Milestone 1 dependencies: none
- Release dependency: accepted DEC-009's odh-cli gate and qualification
- Deferred follow-up: Data Registry runtime projection is out of current scope.
  Training Operator and Llama Stack Operator were interim carry-overs under
  DEC-032; current v3 omits their spec/status under DEC-033/DEC-034 while v2
  remains retirement-only. Final qualification depends on the additional
  Tasks 018/019/021/024 evidence; release
  qualification also depends on Task 010.

Follow the dependencies in PLAN. The detailed `94812/M1` package above is
completed; Task 012 also completed independently of the component handler
tasks.

### Milestone 1: versioning machinery

- Introduce v3 with exactly the v2 shape and make it hub/storage/runtime.
- Add semantic identity v2 <-> v3 conversion.
- Move production controllers, registries, modules, handlers, status, helpers,
  generators, and tests from typed v2 to typed v3.
- Remove v1 API/conversion/admission/serving. Do not implement odh-cli here;
  future Task 010 separately qualifies v1 storage migration before upgrade
  release.
- Port v2 admission behavior to identical v3 fields, retain v2 compatibility,
  regenerate all artifacts, and prove lossless whole-object round trips.

### Milestone 1A: accepted KServe/MaaS migration

- After Task 001, execute Task 012 without waiting for other stanza decisions.
- Remove v3 `kserve.modelsAsService`, retain DEC-022's forward selection, and
  apply DEC-024's literal legacy-Managed marker and accepted normalization.
  Remove custom MaaS field warnings from both admissions; keep v2 CEL intact.
- Cover all four valid C/L combinations, v3 OldObject authority,
  permanent deletion after canonical MaaS edits and reverts,
  marker reset from current v2 C/L, shared component type reuse, and unknown values
  through conversion, handler, schema, CEL, and envtest/integration tests.
  Task 011 originally owned consolidated E2E under DEC-023; Tasks 021-024 now
  own targeted additions, while Task 012's original compile-fixture-only
  boundary remains historical. No installed intermediate-v3 compatibility is
  assumed.

### Milestone 2 and 3: remaining contract decisions and changes

- Task 014 historically verified the interim Training Operator and Llama Stack
  stanzas under DEC-032. Tasks 015 and 016 record accepted removals under
  DEC-033/DEC-034. Their v2 retirement-only validation, v3 spec/status
  removal, and direct/round-trip conversion are implemented; Tasks
  018/019/021/024 provide further evidence. Preserve Task 012's MaaS result.
- Integrate `95342`, `95344`, and `95346`, then adapt v3 admissions and all
  handler/status/schema tests.

### Milestone 4: final release qualification

- Run fresh-install, supported-upgrade, interrupted-migration/retry, rollback,
  v2-client, and ODH/RHOAI platform-variant tests.

Final definition of done:

- V3 is the sole storage and production-internal DSC version.
- V2 remains served and preserves all v3 state and existing effective behavior.
- V1 is absent from code and generated APIs; release tests prove existing
  storage is migrated before an installed CRD drops v1.
- Dashboard, AI Hub, Feature Store, and Data Registry accepted contracts are
  fully projected with correct status and conditions.
- Every intended v2/v3 difference has focused direct, round-trip, and
  API-server conversion coverage as required by DEC-003 and DEC-035.
- ODH and RHOAI generated deliverables are consistent.
- `make generate manifests api-docs`, `make fmt`, `make lint`,
  `make unit-test`, `make build`, focused tests, and `git diff --check` pass;
  intermediate tasks record their Task 011 E2E handoff, and Task 011 records
  the required cluster E2E results or explicit CI execution handoff.

## Out-of-scope and downstream records

- `RHOAIENG-94813` (`🚫`): downstream upgrade, compatibility, and rollback
  verification after the operator implementation is available. Operator-owned
  upgrade tests remain part of `94812`; downstream product execution does not.
- `RHOAIENG-95349` (`🚫`, RHOAI 3.7): rename/converge internal AI Hub CR/CRD,
  GVK, and module metadata. Public v3 mapping in 3.6 must use the existing
  internal identity.
- `RHOAIENG-95350` (`🚫`, RHOAI 3.7): rename/converge internal Data/Feast
  CR/CRD, GVK, and metadata. Public v3 mapping in 3.6 must use the existing
  internal identity.
- Data Connection Hub (`🚫`): not part of the 3.6 `data` stanza.
- The odh-cli executable and packaging (`🚫` in this repo): external owners
  implement the accepted gate. Its contract, release dependency, and
  operator-side upgrade qualification remain part of this plan. Release notes
  and product documentation are also externally owned.

## Optional provenance

- [RHOAIENG-94804 epic](https://redhat.atlassian.net/browse/RHOAIENG-94804)
- [RHOAIENG-94812 implementation](https://redhat.atlassian.net/browse/RHOAIENG-94812)
- [RHOAIENG-95857 dedicated MaaS migration](https://redhat.atlassian.net/browse/RHOAIENG-95857)
- [RHOAIENG-85262 spike](https://redhat.atlassian.net/browse/RHOAIENG-85262)
- [Spike findings document](https://docs.google.com/document/d/1IvAHo3xRd4fHBmzU0Vnpgiq7M1K2w0OWdt7ZRhUIuk4/edit)

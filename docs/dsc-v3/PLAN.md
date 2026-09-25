# RHOAIENG-94812: DataScienceCluster v3 operator implementation plan

This is the repository-local implementation guide for DataScienceCluster
(DSC) v3. The authoritative decisions are in `decisions.md`. This guide
contains the relevant conclusions from the DSC v3 epic,
implementation task, spike, follow-up tasks, comments, and wiki review as of
**2026-09-22**. An implementation agent must be able to use this plan and
[TASKS.md](TASKS.md) without access to Jira, Google Docs, or the wiki. External
links at the end are provenance only.

[decisions.md](decisions.md) is the authoritative decision record, and
[development.md](development.md) defines mandatory implementation rules. If a
summary in this plan conflicts with an accepted decision, the decision wins.

The plan intentionally does not invent public API decisions that remain open.
Such decisions are hard gates in the decision record below. Work that does not
depend on an open gate may proceed independently.

## Agent operating protocol

1. Read `AGENTS.md`, `CONTRIBUTING.md`, `docs/DESIGN.md`,
   `docs/COMPONENT_INTEGRATION.md`, [development.md](development.md),
   [decisions.md](decisions.md), this file, and [TASKS.md](TASKS.md).
2. Select the first unblocked item in the TASKS dependency order. Do not use a
   provisional example as an approved API contract.
3. Use the repository map and task entry as the starting point. Search for all
   remaining versioned imports and generated consumers before editing.
4. If work reaches a `Proposed` decision, stop only that workstream. Continue
   any independent task; never choose a public JSON name, conversion
   precedence, default, or release-rollout strategy on behalf of API owners.
5. When an external decision is supplied, first record it as `Accepted` in
   `decisions.md`, then update the corresponding summary/gate in this plan and
   TASKS before implementing it.
6. Update the numbered task file's front matter and Outcomes, then mirror its
   status in TASKS. Jira workflow status is informational and need not be
   updated by an agent.
7. Keep changes scoped. Preserve unrelated working-tree changes. When an
   action chain is changed, garbage collection must remain the final action.
8. Treat conversion and conversion tests as part of every v3 data-model
   change. A v3 field/status/condition change is incomplete without explicit
   v2 <-> v3 behavior and tests in the same change.
9. After every code change, run the mandatory generation, formatting, and lint
   gates listed under Verification and include generated diffs.

## Outcome and boundaries

The completed operator must:

- serve `datasciencecluster.opendatahub.io/v3` as the sole storage version and
  use typed v3 DSC objects throughout production reconciliation;
- continue serving v2 through an explicit v2-to-v3 conversion webhook and
  preserve v2 admission compatibility while marking v2 deprecated;
- remove v1 from the new operator and generated artifacts in Task 001; keep
  those artifacts ineligible for upgrade promotion until separate Task 010
  qualifies the odh-cli gate that migrates v1 storage to v2;
- preserve the effective behavior of existing v2 objects and preserve all v3
  state through a v2 read-modify-write;
- migrate deprecated KServe MaaS configuration to the canonical v3 AI Gateway
  stanza without changing the served v2 contract;
- implement the accepted Dashboard, AI Hub, Feature Store, and Data Registry
  public shapes and project them into the existing module APIs;
- regenerate both ODH and RHOAI CRDs, bundles, webhooks, samples, API docs, and
  Go-generated artifacts; and
- prove fresh-install and supported-upgrade behavior with automated tests.

The following are not part of RHOAI 3.6 DSC v3:

- renaming the internal AI Hub/Model Registry CR, GVK, or module metadata;
- renaming the internal Data/Feast CR, GVK, or module metadata;
- Data Connection Hub integration;
- removal of v2 serving or v2 admissions; and
- implementation of the odh-cli gate, product documentation, or release notes
  outside this repository. The gate contract and operator-side integration and
  qualification tests remain in scope.

## Decision summary

This is a convenience summary. [decisions.md](decisions.md) is authoritative.
`Accepted` means implementation may rely on the decision. `Proposed` is a hard
gate. Issue keys identify provenance and ownership; they are not required
reading.

| ID | State | Normative decision | Unblocks |
| --- | --- | --- | --- |
| `DEC-001` | Accepted | v3 is the hub/storage/internal type and v2 remains served. | API and internal migration |
| `DEC-002` | Accepted | Initial v3 is wire-equivalent to v2 and conversion is semantic identity. | Machinery milestone |
| `DEC-003`, `DEC-035` | Accepted | Every future v3 data change includes bidirectional conversion and focused tests; the former OpenAPI comparison guard is removed. | All future v3 changes |
| `DEC-004` | Accepted | V2/v3 use distinct GVK constants and admissions. | Admissions |
| `DEC-005`, `DEC-006` | Accepted | Register conversion through v2 and use ConversionReview protocol `[v1]`. | Conversion webhook |
| `DEC-008` | Accepted | Final v3 code/generated API removes v1. | V1-free end state |
| `DEC-009` | Accepted (`94814`) | Run the idempotent odh-cli migration gate before OLM applies the v1-free CRD, then migrate storage from v2 to v3 after upgrade. | Task 010 and upgrade release |
| `DEC-010` | Accepted | Empty `managementState` has effective value `Removed`. | Lifecycle behavior |
| `DEC-011`, `DEC-012` | Accepted | Keep internal module identities and exclude DCH in 3.6. | Component handlers |
| `DEC-013`, `DEC-014`, `DEC-016` | Accepted | Task 001 removes v1 without odh-cli work; the separate gate blocks upgrade release, and evolving v3 public types remain isolated. | Safe implementation sequence |
| `DEC-015`, `DEC-019` | Accepted | Keep v2 served but deprecated and use the exact warning literal recorded in DEC-019. | V2 migration path |
| `DEC-016` | Accepted | Task 001 excludes odh-cli work and may complete; future Task 010 blocks upgrade release until qualification passes. | Independent machinery development |
| `DEC-017` | Accepted | Even with no DSC object, remove and verify v1 in `status.storedVersions`; only v1 already absent is mutation-free success. | Correct Task 010 gate behavior |
| `DEC-018`, `DEC-020` | Proposed | Name the enforceable promotion control and complete supported execution matrix. | Task 010 start/completion and upgrade release |
| `DEC-007`, `DEC-021` | Superseded by `DEC-035` | The independent v2 baseline and structural difference registry were removed. | Test-package cleanup |
| `DEC-022` | Accepted | V3 removes deprecated KServe MaaS; forward precedence, warning removal, and v2 CEL remain. DEC-024 supersedes the reverse policy. | Independent Task 012 |
| `DEC-023` | Accepted | Intermediate tasks require unit/integration evidence; Task 011 owns consolidated E2E implementation and execution. | Test staging and task completion |
| `DEC-024` | Accepted | Literal `legacy-managed` marker preserves only legacy Managed; C/A stay migrated and v3 admission deletes the marker on canonical MaaS state changes while parent edits preserve it. | Task 012 marker/normalization contract |
| `DEC-031` | Superseded | The initial decision to retain Training Operator and Llama Stack Operator in v3 is historical; DEC-032 supersedes it. | Historical record |
| `DEC-032` | Accepted | Current Training Operator and Llama Stack Operator v3 stanzas were retained only as an interim baseline; DEC-033/DEC-034 later accept removal from final v3. | Task 014 interim verification and Tasks 015/016 |
| `DEC-033` | Accepted | Remove LlamaStackOperator spec/status from final v3; v2 remains retirement-only, with no new or re-enabled `Managed` state. | Task 015; follow-up validation/conversion implementation |
| `DEC-034` | Accepted | Remove TrainingOperator spec/status from v3 shipped in 3.6 GA; preserve the v2 no-reenable CEL rule and retirement-only behavior. | Task 016; follow-up conversion implementation |
| `DEC-027` | Accepted (`94805`) | Dashboard v3 uses independent `standard` and `maasPortal` children; existing lifecycle, status, condition, conversion, and internal Dashboard CR behavior is preserved. | Dashboard schema, conversion, handler |
| G1 | Accepted (`DEC-027`) | `dashboard` is structural with independent `standard` and `maasPortal` children; existing Dashboard lifecycle, status, conditions, and v2 conversion behavior are retained. | Dashboard schema, conversion, handler |
| G2 | Accepted (`DEC-025`) | Public v3 JSON name is `aiHub`; public status and condition use `aiHub` and `AIHubReady`. | AI Hub schema, conversion, handler |
| G2P | Accepted (`DEC-026`) | Platform v1alpha2 uses `aiHub` and `data`; v1alpha1 remains a conversion spoke with `modelregistry` and `feastoperator`. Internal module identities remain unchanged. | Platform API and projection |
| G3 | Accepted (`DEC-029`, `95339`) | `data` is structural with independent `featureStore` and `dataRegistry` children and no parent `managementState`; v2 has an explicit `feastoperator.dataRegistry` compatibility field. | Data schema, conversion, handler |
| G4 | Accepted (`DEC-028`, superseding DEC-025 field naming) | V3 uses `applicationNamespace`; v2 retains `registriesNamespace` with existing optionality, build-specific managed default, Removed behavior, immutability, and zero-value conversion semantics. | AI Hub admissions and conversion |
| G5 | Accepted (`DEC-022`, `DEC-024`) | Canonical v2 Managed is preserved; legacy v2 Managed selects v3 Managed only when the KServe parent is Managed. The marker is emitted whenever legacy Managed is paired with canonical Removed, including a disabled KServe parent. Reverse keeps C/A migrated and writes legacy Managed iff marked, else Removed. | Task 012 MaaS conversion |
| G6 | Accepted removal outcomes (`DEC-033`, `DEC-034`) | Final v3 omits both deprecated spec/status entries. V2 retains compatibility fields but may not newly enable or re-enable either component; an existing `Managed` value may remain unchanged or transition only to `Removed`/empty. | Tasks 015/016; separate validation/conversion implementation Jira remains outstanding |
| G7 | Accepted and implemented for deprecated operator retirement | V2 retirement validation, v3 spec/status removal, and direct/round-trip conversion are present; Tasks 018/019/021/024 extend migration and cluster evidence. | Tasks 014-019, 021, 024 |

To close a gate, add an accepted decision with exact normative behavior to
`decisions.md`, then replace `Proposed` here and update TASKS. A statement that
the overall v3 direction is approved does not close a field-level gate.

## Embedded contract matrix

This matrix captures accepted and proposed input. A row is implementation-ready
only when its corresponding decision is `Accepted` in `decisions.md`.

### Unchanged surface

The following v2 spec entries currently exist and are expected to retain their
JSON names and semantics in v3 unless the final matrix explicitly says
otherwise: `workbenches`, `aipipelines`, KServe except for DEC-022's removed
legacy MaaS child, `kueue`, `ray`, `trustyai`, `ogx`, `mlflowoperator`,
`trainer`, `sparkoperator`, `aigateway`, and `mcplifecycleoperator`. The
deprecated `trainingoperator` and `llamastackoperator` stanzas were present in
the interim v3 code under DEC-032; current v3 omits both spec/status entries
under DEC-033 and DEC-034. Their v2
compatibility fields remain retirement-only: new/re-enabled `Managed` values
are rejected, while an existing `Managed` value may remain unchanged or
transition only to `Removed`/empty. Conversion drops the retired v2 spec and
status fields when writing v3 and reports `Removed` for both on v2 reads.
The common DSC status fields, release information, conditions, related
objects, and error message must also survive conversion.

### Changed and proposed surface

| Concern | Current v2 shape | Target v3 shape | Mapping state |
| --- | --- | --- | --- |
| Dashboard | `dashboard.managementState`; `dashboard.maasConsumerPortal.managementState`; status fields `dashboard` and `maasConsumerPortal` | `dashboard.standard.managementState`; `dashboard.maasPortal.managementState`; existing status fields and `MaaSConsumerPortalAvailable` condition | Direct spec mapping in both directions; empty is effectively Removed; all four independent states are valid; preserve existing status, condition, lifecycle, and internal Dashboard CR behavior. |
| AI Hub | `modelregistry.managementState`; `modelregistry.registriesNamespace`; status `modelregistry`; condition `ModelRegistryReady` | `aiHub.managementState`; `aiHub.applicationNamespace`; status `aiHub`; condition `AIHubReady` | Explicit bidirectional mapping: v2 `registriesNamespace` ↔ v3 `applicationNamespace`; preserve effective namespace/default/immutability behavior and rename only public status/condition names. Internal AIHub CR stays unchanged. |
| Feature Store | `feastoperator.managementState` | `data.featureStore.managementState` | One-to-one lifecycle mapping. |
| Data Registry | `feastoperator.dataRegistry.managementState` compatibility field | `data.dataRegistry.managementState` | One-to-one lifecycle mapping in both directions; empty/absent is effectively Removed and no annotation is required. |
| Platform module projection | `config/v1alpha1` fields `modules.modelregistry` and `modules.feastoperator` | `config/v1alpha2` fields `modules.aiHub` and `modules.data` | One-to-one ManagementSpec mapping in both directions; internal identifiers stay `modelregistry` and `feastoperator`. Defined by DEC-026 and implemented by Task 013. |
| MaaS | Canonical `aigateway.modelsAsAService`; deprecated `kserve.modelsAsService` also exists | Only `aigateway.modelsAsAService` | Canonical v2 Managed is preserved; legacy Managed selects v3 Managed only when `kserve.managementState` is Managed. DEC-024 emits the marker whenever C=Removed/L=Managed, including a disabled KServe parent. Reverse keeps C/A migrated; absent marker means legacy Removed. Custom field warnings removed; v2 CEL retained. |
| Training Operator | Deprecated `trainingoperator` spec/status retained for v2 compatibility | No `trainingoperator` spec or status path | Existing v2 CEL disallows new/re-enabled `Managed`; conversion drops v2 spec/status and reverse reports `Removed` for both. Empty is effectively `Removed`. |
| Llama Stack Operator | Deprecated `llamastackoperator` spec/status retained for v2 compatibility | No `llamastackoperator` spec or status path; `ogx` remains separate | V2 CEL disallows new/re-enabled `Managed`; conversion drops v2 spec/status and reverse reports `Removed` for both. Empty is effectively `Removed`. |

Accepted Data shape:

```yaml
# v2 compatibility shape
components:
  feastoperator:
    managementState: Managed
    dataRegistry:
      managementState: Managed
```

```yaml
# v3 proposal; data is a structural group with no parent state
components:
  data:
    featureStore:
      managementState: Managed
    dataRegistry:
      managementState: Managed
```

Accepted AI Hub shape:

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

The accepted Dashboard shape from RHOAIENG-94805 is:

```yaml
components:
  dashboard:
    standard:
      managementState: Managed
    maasPortal:
      managementState: Managed
```

`standard` and `maasPortal` are independent children under a structural
`dashboard` group. The old v2 `maasConsumerPortal` name remains only in the v2
compatibility mapping and the internal Dashboard CR/status contract.

### Conversion invariants

- In the machinery milestone, v2 and v3 have identical Go/JSON schemas. The
  v2 <-> v3 converter is therefore a semantic no-op: it copies the complete
  object without renames, drops, defaults, or normalization.
- Transforming conversion begins only after the relevant field-level decision
  is accepted. DEC-022/DEC-024 authorize Task 012 independently; Dashboard,
  AI Hub, and Data transformations are authorized by DEC-025, DEC-028, and
  DEC-029. DEC-033/DEC-034 accept removing Training Operator and Llama Stack
  Operator spec/status from final v3. The prior direct identity mapping was
  interim only. Current conversion drops both fields in v2-to-v3 and reports
  `Removed` for both spec and status in v3-to-v2; Task 014 remains historical
  verification of the interim baseline.
- Conversion is deterministic, idempotent, and performs no cluster lookups.
- `ObjectMeta` (except reserved conversion metadata) and every unchanged spec
  and status field are copied in both directions. A field may be deliberately
  dropped only when the accepted matrix states how compatibility and effective
  behavior are preserved.
- `v2 -> v3 -> v2` keeps canonical MaaS and AI Gateway parent migrated under
  DEC-024; original C/A are not restored. Legacy `Managed` returns iff marked;
  legacy `Removed` remains `Removed`. Canonical MaaS edits permanently delete
  the marker, including after reverts, while parent edits preserve it.
- `v3 -> v2 -> v3` preserves effective v3 configuration, including independent
  child states, during a v2 read-modify-write; conversion-owned legacy
  provenance may be retired when visible v2 canonical state is `Managed`.
- Prefer an explicit compatibility field for future undecided mappings.
  DEC-024 requires `conversion.opendatahub.io/maas-v2-state: legacy-managed`
  only for source v2 `C=Removed, L=Managed`, ignoring incoming marker values. V3 UPDATE
  compares old/new canonical MaaS management state; a change deletes the
  marker, otherwise `OldObject` is authoritative. Parent edits preserve it.
  Native v3 CREATE strips supplied metadata; unknown interpreted marker values
  are rejected. There is no JSON payload or projection snapshot.
- Reuse existing `componentApi` AI Gateway/canonical value types in both
  versions, without canonical pointers or duplicated wrappers. Preserve the
  v2 wire contract when reusing these types.
- Defaulting and validation stay outside conversion.
- Public MaaS management states are defaulted to `Removed`; do not model absent
  or empty MaaS object shapes in the migration contract. Do not normalize
  unrelated absent API fields.

### Required rule for every future v3 data-model change

Every change to a v3 DSC spec/status type, nested component type, JSON name,
optionality, default, validation, or condition is also a conversion change,
even when the correct converter code remains an identity copy. The same pull
request or commit must:

1. identify the old v2 representation and the new v3 representation;
2. define `v2 -> v3` behavior for populated, absent, empty, and invalid legacy
   inputs;
3. define `v3 -> v2` behavior, including how a v2 read-modify-write preserves
   any value v2 cannot represent directly;
4. update both conversion directions, or document why the existing explicit
   copy is still correct;
5. add focused forward, backward, and both round-trip tests for the changed
   data, including status/condition mappings when applicable;
6. update defaulting, validation, CRD schema, samples, and API docs when the
   wire contract changes; and
7. update a structural difference/coverage guard so a new v2/v3 field
   difference cannot be added silently without an explicit mapping and test.

A v3 data-model change must not merge with only handler or schema tests. The
conversion tests are part of its definition of done.

## Current repository baseline

This map records the pre-Task001 baseline and the component behavior used to
plan the migration. Task 001 is now completed at `75c2018d4`: v3 is
hub/storage/runtime, v2 is the served deprecated spoke, and v1 is removed.
Its Outcomes are the current machinery evidence; the historical v1/v2 paths
below are not instructions to restore them. Task 012 is completed at
`25c80b63a`, following the initial `dd8472bbc` migration commit.

| Area | Current behavior and primary touchpoints |
| --- | --- |
| Public DSC API | `api/datasciencecluster/v2/datasciencecluster_types.go` is the storage API and implements `conversion.Hub`. `api/datasciencecluster/v1` is a spoke. Shared component types are in `api/components/v1alpha1`; do not change a shared type if that would unintentionally alter v1/v2 or an internal module schema. |
| Registration/codegen | `PROJECT` registers DSC v1 and v2 with defaulting/validation. `cmd/main.go` registers both schemes. `cmd/component-codegen/cmd/generator/generator.go` hard-codes the v2 DSC types path. |
| Runtime DSC type | `internal/controller/datasciencecluster`, `internal/controller/components/registry/registry.go`, `internal/controller/modules/types.go`, `internal/controller/modules/base.go`, status helpers, predicates, initial-install helpers, and tests use typed v2 objects. |
| Conversion compatibility | After Milestone 1, `api/datasciencecluster/v2/datasciencecluster_conversion.go` explicitly copies the still-identical v2/v3 shapes. The independent comparison package added then was removed under DEC-035; current conversion tests live beside the API and webhooks. |
| Admissions | `internal/webhook/webhook.go` registers `dsc-v1` and `dsc-v2`. Each version has defaulting, validation, and envtest coverage under `internal/webhook/datasciencecluster`. |
| Generated CRD | ODH and RHOAI generated DSC CRDs currently serve v1 and v2, with v2 storage. Conversion patches under `config/crd/patches` and `config/rhoai/crd/patches` use `/convert` and list review versions v1/v2. |
| Samples | Primary samples are `config/samples/datasciencecluster_v3_datasciencecluster.yaml` and the RHOAI equivalent. |
| Dashboard | V3 uses structural `dashboard` with independent `standard` and `maasPortal` children. V2 retains top-level `dashboard` and `maasConsumerPortal` fields for compatibility. `internal/controller/modules/dashboard/handler.go` deploys the operator when either child is managed, projects the core and portal states into the existing Dashboard CR, and mirrors condition `MaaSConsumerPortalAvailable` to status field `MaaSConsumerPortal`. |
| AI Hub | Public v3 DSC uses `aiHub.applicationNamespace` and `AIHubReady`; v2 retains `modelregistry.registriesNamespace`; Platform v1alpha2 also uses `aiHub`, while v1alpha1 remains the compatibility `modelregistry` field. `internal/controller/modules/modelregistry/handler.go` manages internal AIHub GVK `AIHub`, CR name `default-aihub`, module/manifest name `modelregistry`, and maps module readiness to the public `AIHubReady` condition. It maps v3 `applicationNamespace` to internal `instancesNamespace`, falling back to the applications namespace, and mirrors it into v3 AI Hub status. |
| Data | `api/components/v1alpha1/feastoperator_types.go` currently contains only Feast lifecycle state. `internal/controller/modules/feastoperator/handler.go` enables the existing Feast module and builds a FeastOperator CR whose spec currently contains only derived external-OIDC configuration; it does not project child lifecycle state. |
| MaaS | AI Gateway runtime logic uses canonical `aigateway.modelsAsAService`; v2 conversion preserves canonical `Managed` and selects it from legacy MaaS only when the KServe parent is `Managed`. `internal/controller/modules/kserve/handler.go` removes `modelsAsService` before projecting the KServe module CR. |
| Defaults/validation | V2 defaults Model Registry `registriesNamespace` when Model Registry is managed: `odh-model-registries` for ODH and `rhoai-model-registries` for RHOAI; v3 exposes the resulting value as `applicationNamespace`. It defaults KServe NIM to managed when KServe is managed. Validation enforces DSC singleton creation, denies Kueue managed, warns for deprecated KServe MaaS, prevents re-enabling that deprecated field after removal, and keeps the AI Hub application namespace immutable while managed. |
| Tests | Unit/envtest coverage exists beside APIs, handlers, and webhooks. `tests/e2e/v2tov3upgrade_test.go` is legacy-named and does not yet prove DSC API v2-to-v3 storage conversion; do not count it as the new upgrade acceptance test without rewriting it. |

### High-risk implementation map

These are the concrete starting points most likely to be missed by a package-only
migration. The numbered task files assign ownership and expected tests. Paths
under `api/components/v1alpha1` are audit inputs for v3 divergence, not an
invitation to mutate shared v2/v3 types.

| Concern | Files to inspect |
| --- | --- |
| API, registration, and generators | `api/datasciencecluster/v1`, `api/datasciencecluster/v2`, the new `api/datasciencecluster/v3`, `PROJECT`, `cmd/main.go`, `cmd/component-codegen/cmd/generator/generator.go` |
| Conversion, admission, and schemes | `internal/webhook/webhook.go`, `internal/webhook/datasciencecluster/v2`, the v3 admission package, `internal/webhook/envtestutil/envtestutil.go`, `pkg/cluster/gvk/gvk.go`, `pkg/utils/test/scheme/scheme.go` |
| Runtime helpers outside the DSC controller | `pkg/cluster/resources.go`, `pkg/initialinstall/creation.go`, `pkg/upgrade/uninstallation.go`, `pkg/controller/predicates/resources/resources.go`, `pkg/controller/actions/deploy/action_deploy_support.go`, `pkg/utils/test/mocks/types.go` and their adjacent tests |
| Runtime controllers and modules | `internal/controller/datasciencecluster`, `internal/controller/components/registry`, every handler under `internal/controller/components` and `internal/controller/modules`, especially `internal/controller/modules/types.go`, `base.go`, and `modules_controller_actions.go` |
| Delivery artifacts and init-resource contract | `config/samples/datasciencecluster_v3_datasciencecluster.yaml`, `config/rhoai/samples/datasciencecluster_v3_datasciencecluster.yaml`, both platform DSC conversion patches, both platform CSV bases, `api/datasciencecluster/v2/init_resource_annotation_test.go`, `cmd/manifest-tools/pkg/config/init_resource_sync_test.go` |
| Shared public-type regression risks | `api/components/v1alpha1/dashboard_types.go`, `modelregistry_types.go`, `modelregistry_types.odh.go`, `modelregistry_types.rhoai.go`, `feastoperator_types.go`, `aigateway_types.go`, `modelsasservice_types.go`, `kserve_types.go`, `trainer_types.go`, `trainingoperator_types.go`, `llamastackoperator_types.go`, `ogx_types.go` |
| E2E infrastructure and upgrade coverage | `tests/e2e/helper_test.go`, `test_context_test.go`, `controller_test.go`, `components_test.go`, `resilience_test.go`, `creation_test.go`, `v2tov3upgrade_test.go`, plus the component-specific E2E files named in Tasks 007-009 |

This map is an intentionally focused starting set, not a request to scan the
entire repository before work begins and not a frozen exhaustive manifest.
Start with the named files and follow their imports, generated outputs, and
test failures. Use the searches below as scoped discovery when an unexpected
dependency appears and as repository-wide acceptance checks after the change.
Record any newly discovered central file in the active task's Outcomes and
handoff record.

Before considering the internal migration complete, use acceptance searches
such as:

```bash
rg -n 'datasciencecluster/v2|dscv2' api cmd internal pkg tests -g '*.go'
rg -n 'datasciencecluster/v1|dscv1' api cmd internal pkg tests -g '*.go'
rg -n 'modelregistry|ModelRegistry|feastoperator|FeastOperator|MaaSConsumerPortal|modelsAsService' \
  api internal pkg config tests -g '*.go' -g '*.yaml'
```

The first search should end with a small, documented allowlist containing only
v2 conversion/admission compatibility and version-specific tests. The second
must have no production hits after the machinery milestone.

## Executable implementation milestones

### Milestone 1: establish v3 machinery without contract changes

This first implementation step is completed under DEC-023. It used the v2
schema unchanged so it was independent of G1-G7.
The executable task and living outcome record is
[tasks/001.md](tasks/001.md).

Task 001 is a roll-up. Execute its five records independently where their
dependencies allow: [v3 API/baseline](tasks/001-01.md),
[atomic conversion/v1 removal](tasks/001-02.md),
[typed-v3 runtime migration](tasks/001-03.md),
[v2 deprecation/generated delivery](tasks/001-04.md), and
[milestone integration](tasks/001-05.md). Tests land with the subtask that
changes behavior; the integration subtask does not defer them.

#### 1. Introduce v3

- Add `api/datasciencecluster/v3` by reproducing the current v2 DSC spec,
  status, components, markers, validation annotations, group registration, and
  list types exactly. Do not introduce Dashboard, AI Hub, Data, MaaS, or legacy
  field changes in this milestone.
- Generate v3 deepcopy code and register v3 in `PROJECT`, `cmd/main.go`, envtest
  schemes, and all version-aware helpers.
- Leave v2 as the temporary hub/storage version while 001-01 adds v3. In
  001-02, move `+kubebuilder:storageversion` and `conversion.Hub` to v3
  atomically with v2 spoke conversion activation and v1 deletion. Never leave a
  compiling boundary where the existing v1 converter requires v2 as hub after
  that marker has been removed.
- Point component-codegen and new DSC fixtures/samples at v3.
- Keep top-level v3 API types version-owned. Do not change shared component
  types to introduce v3 behavior; create v3-owned nested types when a stanza
  later diverges and review v2 schema effects explicitly.

#### 2. Add identity v2 <-> v3 conversion

- Implement `ConvertTo` and `ConvertFrom` on v2 with v3 as the hub.
- Because the schemas are identical in this milestone, copy the complete
  object semantically unchanged. Do not apply defaults, normalize management
  states, rename fields, drop legacy fields, or invoke any cluster lookup.
- Add whole-object tests showing `v2 -> v3 -> v2` and `v3 -> v2 -> v3`
  semantic equality for populated spec, status, metadata, conditions, releases,
  related objects, annotations, empty values, and deprecated fields. Ignore
  only the expected target-version `TypeMeta`/GVK representation.
- Register controller-runtime conversion through the v2 spoke, introduce
  distinct v2/v3 GVK constants, and keep each admission handler bound to its
  own GVK.
- Generate `conversionReviewVersions: [v1]`; this is the Kubernetes protocol
  version, not a list of DSC versions.
- Exercise real `/convert` `ConversionReview` requests in both directions with
  one and multiple objects.

#### 3. Move production code from v2 to v3

- Change the DSC reconciler, component registry, module `DSCContext`, handler
  interfaces, base-handler reflection/status writers, all component/module
  handlers, predicates, initial-install helpers, comparison utilities,
  generators, and production utilities to typed v3.
- Update tests and fixtures alongside each package. After this step, v2 imports
  are allowed only in v2 conversion, v2 admissions, and explicit compatibility
  tests.
- Re-check every touched action chain and keep garbage collection last.

#### 4. Remove v1 and record the separate release gate

- Remove v1 in Task 001 without implementing or qualifying odh-cli. Task 010 is
  the separate future release-gate task.
- Remove the v1 API, conversion, admissions, scheme and `PROJECT` registration,
  generated serving, fixtures, and obsolete version-specific tests. Preserve
  behavioral coverage by moving relevant cases to v2/v3 tests.
- Move shared `/convert` registration to the v2 spoke before deleting its v1
  registration path. No v1 <-> v3 converter is required in the new operator.
- Delete the AI Gateway conversion annotation only after confirming that no
  v2/v3 or other consumer requires it.
- Mark the v1-free artifacts ineligible for upgrade promotion until Task 010
  proves DEC-009's odh-cli migration, `status.storedVersions`, OLM ordering,
  retry, and rollback contract.

#### 5. Amend tests and generated artifacts

- Add identity conversion unit and webhook/envtest round-trip coverage.
- Cover v2/v3 differences with focused conversion tests. The structural
  OpenAPI comparison added in Milestone 1 was later removed under DEC-035.
- Port controller, handler, admission, initial-install, comparison, and E2E
  helpers to v3 while retaining explicit v2 client compatibility tests.
- Assert that v3 is the only storage version, v2/v3 are served, v1 is absent,
  `/convert` is configured through v2, `conversionReviewVersions` is `[v1]`,
  and v2/v3 admissions remain version-correct. Assert v2 has
  `deprecated: true` and DEC-019's exact `deprecationWarning` literal.
- Regenerate every ODH and RHOAI artifact and run all mandatory gates.

Milestone 1 exit criteria:

- V2 and v3 expose the same schema and identity conversion is lossless in both
  directions.
- Production reconciliation exclusively uses typed v3 objects.
- V1 code and serving are absent. Upgrade release remains blocked until Task
  010 proves v1 storage is removed before the v1-free CRD is installed.
- Generated CRDs serve deprecated v2 and v3 with v3 as the sole storage version.
- G1-G6 have accepted decisions. Training Operator/Llama Stack G7 retirement
  validation, v3 field removal, and conversion are now implemented under
  DEC-033/DEC-034; the additional migration and E2E coverage is tracked in
  Tasks 018/019/021/024. Accepted G5 was implemented separately by Task 012.

### Milestone 1A: migrate KServe MaaS independently

Entry: Milestone 1 is complete. No component Jira or additional decision is
required for the independent MaaS work.

Execution record: [KServe/MaaS migration](tasks/012.md); executable MaaS
scenarios: [tests/e2e/webhooks/](../../tests/e2e/webhooks/).
Status: completed 2026-09-18 by Codex on `RHOAIENG-94812-DSC-v3`;
implementation commit `25c80b63a` (following `dd8472bbc`). Verification and
behavior examples are in Task 012 Outcomes; Task 011 owns cluster E2E execution.

- Remove deprecated `kserve.modelsAsService` only from the v3 schema; keep the
  served v2 schema and update guard unchanged while removing the custom MaaS
  field warning from both admissions.
- Move MaaS selection into explicit v2 <-> v3 conversion under DEC-022 and
  DEC-024: canonical v2 MaaS `Managed` is preserved; legacy `Managed` selects
  canonical v3 `Managed` only when the KServe parent is `Managed`. A legacy
  `Managed` value remains recoverable through the marker when the parent gates
  effective MaaS off. Preserve canonical readiness/status and the independent
  AI Gateway parent behavior.
- Under DEC-024, set the reserved annotation to literal `legacy-managed` for
  incoming v2 `C=Removed, L=Managed`, including when the KServe parent gates
  effective MaaS off. Ignore incoming markers and remove them for all other
  C/L pairs. Reverse copies the current canonical v3 value and sets legacy
  `Managed` iff marked, else `Removed`.
- Enforce v3 CREATE stripping and UPDATE `OldObject` authority. Unrelated and
  parent edits preserve the old marker; canonical MaaS management-state
  changes permanently delete it, even after a revert. Reject unknown marker
  values.
  Reuse component types without canonical pointers, duplicated AI Gateway
  wrappers, or a JSON payload/snapshot. Original C/A/absence are not restored.
- Remove the custom MaaS field deprecation-warning hooks from v2 and v3 while
  retaining the v2 legacy field's existing CEL transition validation and
  DEC-019's unrelated warning for use of the v2 API version.
- Remove obsolete v3 handler/admission fallback behavior and add direct,
  both-round-trip, `/convert`, handler,
  schema, CEL, and envtest integration coverage. Record the required MaaS E2E
  scenario for Task 011; only E2E compile fixture fixes belong in Task 012.
- Assume no supported installed intermediate-v3 release with the legacy field;
  supported migration starts through v2, subject to Task 010's upgrade gates.

Exit: v3 has one canonical MaaS path, v2 remains compatible, and Task 012's
Outcomes contain all conversion and verification evidence.

### Milestone 2: freeze and encode component contracts

Entry: Milestone 1 is complete and component-owner decisions are available for
G1-G7.

Execution records: [Dashboard contract](tasks/002.md),
[Data contract](tasks/003.md), [AI Hub contract](tasks/004.md),
[consolidated matrix](tasks/005.md), and
[API/conversion implementation](tasks/006.md).

- Update the local decision record before code.
- Replace provisional schemas with exact Go/JSON shapes and complete the
  field-by-field spec/status/condition/conversion matrix.
- Implement the accepted Dashboard, AI Hub, and Data changes in v3. DEC-033
  and DEC-034 now accept Training Operator and Llama Stack removal; implement
  those changes only through the separate follow-up contract/task covering v2
  retirement validation, v3 spec/status removal, and conversion/tests. MaaS is
  already owned by Task 012 and must not be reimplemented here.
- Change the formerly identity converter only for accepted schema differences;
  unchanged fields must retain the Milestone 1 copy behavior.
- Apply DEC-003 to every changed spec/status/condition field: conversion code
  and focused bidirectional/round-trip tests land together. DEC-035 removed the
  former structural-difference inventory.
- Update v2 compatibility types only where the accepted contract explicitly
  requires a representable compatibility field.

Exit: G1-G5 are accepted, G6 and the related G7 cells have accepted owner
decisions, every changed field has deterministic bidirectional conversion,
and generated v2/v3 schemas match the approved contract.

### Milestone 3: integrate component projections and admissions

Execution records: [Dashboard](tasks/007.md), [AI Hub](tasks/008.md), and
[Data](tasks/009.md).

- Dashboard: project the accepted core/portal shape into the existing Dashboard
  module CR and mirror the approved status/conditions.
- AI Hub: project the public AI Hub shape into the existing `default-aihub`
  internal resource without performing the deferred rename.
- Data: project Feature Store and Data Registry independently into the existing
  Data/Feast module while retaining OIDC behavior and excluding DCH.
- Adapt v3 defaulting/validation to changed fields. Keep v2 admission behavior
  compatible with v3 storage.
- Add handler, lifecycle, readiness, status, admission, conversion, and schema
  tests for all accepted state combinations.

Exit: each public stanza produces the approved internal resource behavior and
DSC status while v2 clients remain compatible.

### Milestone 4: final release and upgrade qualification

Execution records: [odh-cli gate qualification](tasks/010.md) and
[final integration/release qualification](tasks/011.md).

- Qualify the odh-cli pre-upgrade gate from every supported source release and
  prove OLM never applies the v1-free CRD while v1 remains in
  `status.storedVersions`.
- Add upgrade coverage from the gate's v2 storage state to RHOAI 3.6 v3 storage
  for ODH and RHOAI, including v2/v3 read-update cycles and all changed
  component behaviors.
- Verify final served/storage versions and `status.storedVersions`.

Exit: fresh install, supported upgrade, interrupted retry, and rollback tests
pass with reviewed generated artifacts.

## Verification and acceptance

Minimum automated scenarios:

- Milestone 1 whole-object identity conversion in both directions while v2 and
  v3 schemas are identical;
- a v2/v3 structural-difference guard whose explicit inventory has focused
  conversion coverage for every listed path;
- table-driven spec/status/condition conversion for every changed field;
- `v2 -> v3 -> v2` accepted normalization: canonical v2 MaaS Managed is
  preserved, legacy MaaS Managed selects v3 Managed only when the KServe parent
  is Managed, canonical/parent values stay projected, legacy Managed survives
  iff marked, and legacy Removed remains Removed; permanent marker deletion
  after canonical MaaS edits, including reverts;
- `v3 -> v2 -> v3` preservation of effective v3 values, with conversion-owned
  legacy provenance retired when visible v2 canonical state is `Managed`;
- all four valid `Managed`/`Removed` canonical-versus-legacy combinations;
- v2 and v3 admission defaulting, validation, applicable warnings, and update
  rules, including absence of the removed custom MaaS field warning;
- Dashboard, AI Hub, Feature Store, and Data Registry handler projections,
  lifecycle, readiness, releases, legacy status, and removal;
- generated Milestone 1 schema: deprecated v2 and v3 served, exactly v3
  storage, v1 absent, `/convert` registered through v2, and ConversionReview
  protocol `[v1]`;
- odh-cli pre-upgrade gating before the v1-free CRD, followed by verified v3
  storage migration and safe `status.storedVersions` cleanup;
- ODH and RHOAI fresh install and supported upgrade; and
- idempotent storage migration, interrupted retry, and approved rollback.

Mandatory repository gates after code changes:

```bash
make generate manifests api-docs
make fmt
make lint
make unit-test
make build
git diff --check
```

Tasks before final qualification may complete with the required unit and
integration suites. They must record commands, results, generated diffs, and
E2E handoffs. Tasks 021-024 own their targeted E2E additions; Task 011
integrates and qualifies the full suite on supported clusters. Execution may
be handed to a named CI job when no local cluster is available. Task 010
retains its separate odh-cli/upgrade qualification responsibilities.

## Provenance (optional reading)

- [RHOAIENG-94804: DSC v3 epic](https://redhat.atlassian.net/browse/RHOAIENG-94804)
- [RHOAIENG-94812: operator implementation](https://redhat.atlassian.net/browse/RHOAIENG-94812)
- [RHOAIENG-95857: dedicated MaaS migration](https://redhat.atlassian.net/browse/RHOAIENG-95857)
- [RHOAIENG-85262: original spike](https://redhat.atlassian.net/browse/RHOAIENG-85262)
- [Spike findings document](https://docs.google.com/document/d/1IvAHo3xRd4fHBmzU0Vnpgiq7M1K2w0OWdt7ZRhUIuk4/edit)
- [AI Core Platform wiki summary](https://redhat.atlassian.net/wiki/spaces/~712020ae5659d45615471ca83c37c75652ea3e/pages/460554960/AI+Core+Platform+Features+and+Epics+In-Progress+Summary)
- [Kubernetes CRD versioning](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definition-versioning/)

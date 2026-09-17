# DSC v3 authoritative decisions

This file is the authoritative decision record for DSC v3 development. Agents
must read it before starting a task and must honor every `Accepted` decision.
If another DSC v3 document conflicts with this file, this file wins. Repository
and system-level safety instructions still take precedence.

## How to maintain this record

- Use immutable IDs in the form `DEC-NNN`.
- Allowed states are `Proposed`, `Accepted`, `Superseded`, and `Rejected`.
- Only `Accepted` decisions authorize implementation. A `Proposed` decision is
  a blocker where code behavior depends on it.
- Do not rewrite an accepted decision's history. Add a new decision that names
  and supersedes the old one.
- Record decisions as soon as they are made during development, before relying
  on them in code. Update the index and add the full entry.
- Every task must list the decision IDs it used in its Outcomes section.

## Decision index

| ID | State | Date | Decision |
| --- | --- | --- | --- |
| `DEC-001` | Accepted | 2026-09-18 | V3 is the hub, storage, and production-internal DSC version; v2 remains served. |
| `DEC-002` | Accepted | 2026-09-18 | The first v3 schema is wire-equivalent to v2 and conversion is identity-only. |
| `DEC-003` | Accepted | 2026-09-18 | Every future v3 data change includes bidirectional conversion and tests. |
| `DEC-004` | Accepted | 2026-09-18 | V2 and v3 use distinct GVK constants and admissions. |
| `DEC-005` | Accepted | 2026-09-18 | Conversion webhook registration moves to the v2 spoke. |
| `DEC-006` | Accepted | 2026-09-18 | CRD `conversionReviewVersions` contains Kubernetes protocol version `v1`, not DSC versions. |
| `DEC-007` | Superseded | 2026-09-18 | The OpenAPI schema-difference guard was removed under DEC-035. |
| `DEC-008` | Accepted | 2026-09-18 | V1 is absent from the final DSC v3 code and generated API surface. |
| `DEC-009` | Accepted | 2026-09-18 | An idempotent odh-cli pre-upgrade gate migrates v1 storage to v2 and removes v1 from `status.storedVersions` before the v1-free CRD is installed. |
| `DEC-010` | Accepted | 2026-09-18 | Empty DSC management state has effective value `Removed`. |
| `DEC-011` | Accepted | 2026-09-18 | Internal AI Hub/Model Registry and Data/Feast resource identities are not renamed in 3.6. |
| `DEC-012` | Accepted | 2026-09-18 | Data Connection Hub is excluded from the 3.6 DSC v3 contract. |
| `DEC-013` | Accepted | 2026-09-18 | Task 001 removes v1; the odh-cli pre-upgrade gate is the required migration boundary. |
| `DEC-014` | Accepted | 2026-09-18 | V3-evolving public types are version-owned and v2 has an independent OpenAPI baseline. |
| `DEC-015` | Accepted | 2026-09-18 | V2 remains served but is marked deprecated with a migration warning directing clients to v3. |
| `DEC-016` | Accepted | 2026-09-18 | Task 001 excludes odh-cli work and may complete independently; future Task 010 gates upgrade release. |
| `DEC-017` | Accepted | 2026-09-18 | A no-DSC gate run must still remove and verify v1 in `status.storedVersions`; only v1 already absent is mutation-free success. |
| `DEC-018` | Proposed | 2026-09-18 | Name the concrete CI/release control that prevents promotion or installation of the v1-free upgrade before Task 010 passes. |
| `DEC-019` | Accepted | 2026-09-18 | Use the exact v2 CRD deprecation warning recorded below. |
| `DEC-020` | Proposed | 2026-09-18 | Accept the complete supported-source, artifact, cluster, permission, odh-cli, and CI execution matrix for Task 010. |
| `DEC-021` | Superseded | 2026-09-18 | The independent v2 OpenAPI baseline was removed under DEC-035. |
| `DEC-022` | Accepted | 2026-09-18 | Independent MaaS migration and forward precedence remain; partially superseded by DEC-024 for reverse conversion and no-annotation normalization. |
| `DEC-023` | Accepted | 2026-09-18 | Intermediate implementation tasks complete with unit/integration evidence; Task 011 owns consolidated E2E. |
| `DEC-024` | Accepted | 2026-09-18 | Preserve only legacy Managed with a literal marker; keep canonical/parent migration, retire the marker on canonical MaaS changes, and preserve it across parent edits. |
| `DEC-025` | Accepted | 2026-09-22 | Public v3 Model Registry is exposed as `aiHub`; v2 `modelregistry` maps bidirectionally, including `ModelRegistryReady` <-> `AIHubReady`, while internal AI Hub identities remain unchanged. |
| `DEC-026` | Accepted | 2026-09-22 | Platform v1alpha2 is the hub/storage version; v1alpha1 `modelregistry` and `feastoperator` map to v1alpha2 `aiHub` and `data`, while internal module identities remain unchanged. |
| `DEC-027` | Accepted | 2026-09-22 | Dashboard v3 uses independent `standard` and `maasPortal` children; existing lifecycle, status, condition, and internal Dashboard CR behavior is preserved. |
| `DEC-028` | Accepted | 2026-09-22 | V3 AI Hub uses `applicationNamespace`; v2 Model Registry keeps `registriesNamespace`, with explicit bidirectional conversion and unchanged defaults/validation. |
| `DEC-029` | Accepted | 2026-09-22 | Data v3 is structural with independent Feature Store and Data Registry children; v2 gains an explicit Data Registry compatibility field and both lifecycle states convert directly. |
| `DEC-030` | Accepted | 2026-09-23 | All internal Platform APIs and runtime handlers use v1alpha2; v1alpha1 remains only the served compatibility spoke. |
| `DEC-031` | Superseded | 2026-09-24 | Initial decision to retain Training Operator and Llama Stack Operator in v3; superseded by the interim-only carry-over in DEC-032. |
| `DEC-032` | Accepted | 2026-09-24 | The current Training Operator and Llama Stack Operator v3 stanzas were retained only as an interim verification baseline; DEC-033/DEC-034 later accept their removal from final v3. |
| `DEC-033` | Accepted | 2026-09-24 | Remove LlamaStackOperator spec/status from final v3; retain the v2 compatibility field as retirement-only, disallowing new or re-enabled Managed state. |
| `DEC-034` | Accepted | 2026-09-24 | Remove TrainingOperator spec/status from v3 shipped in 3.6 GA; retain its v2 field as retirement-only and preserve its existing no-reenable CEL rule. |
| `DEC-035` | Accepted | 2026-09-25 | Remove the `pkg/dsc/compare` test package, including its schema baseline and conversion registry; retain adjacent direct and integration conversion tests. |

## Accepted decisions

### DEC-001: v3 is storage and internal; v2 remains served

V3 implements `conversion.Hub`, is the only CRD storage version, and is the
typed DSC version used by production controllers, registries, modules, status
writers, utilities, and generators. V2 remains served as the compatibility
spoke with version-specific admission behavior.

### DEC-002: initial v3 and v2 schemas are equivalent

The machinery task introduces v3 with the current v2 spec, status, JSON names,
markers, defaults, validations, and deprecated fields unchanged. Initial v2
<-> v3 conversion copies object metadata, spec, and status without renaming,
dropping, defaulting, validation, normalization, or cluster access. Only the
expected target API version/GVK may differ.

Unresolved component stanzas must not alter v3 until their contract decisions
are accepted and recorded here.

### DEC-003: every v3 data change owns conversion and tests

Any v3 spec, status, nested type, JSON tag, optionality, default, validation,
condition, addition, move, rename, or removal is a conversion change. The same
change must:

- define and implement or explicitly confirm `v2 -> v3` and `v3 -> v2`;
- preserve v3-only data through a v2 read-modify-write;
- add focused direct tests in both directions;
- add `v2 -> v3 -> v2` and `v3 -> v2 -> v3` tests; and
- cover status and condition mappings when applicable.

If explicit identity-copy code remains correct, the new data still requires a
conversion test.

### DEC-004: keep version-specific GVK and admission behavior

Use distinct `DataScienceClusterV2` and `DataScienceClusterV3` GVK constants.
Each admission handler validates against its own request GVK. Changing a global
DSC GVK to v3 must not cause retained v2 admission requests to fail.

### DEC-005: register conversion from the v2 spoke

After v3 becomes the hub, v2 is the surviving `conversion.Convertible` spoke.
Register the controller-runtime conversion webhook with the v2 DSC type before
deleting v1 webhook registration. Test the real `/convert` endpoint with
Kubernetes `ConversionReview` requests containing one and multiple objects in
both desired API-version directions.

### DEC-006: use the Kubernetes ConversionReview protocol version

`spec.conversion.webhook.conversionReviewVersions` describes the Kubernetes
`ConversionReview` protocol, not served DSC API versions. Generate and assert:

```yaml
conversionReviewVersions:
  - v1
```

Do not add `v2` or `v3` to this list.

### DEC-007: guard the complete v2/v3 schema contract

> Superseded by DEC-035. The guard described below was removed; this records
> the original decision rather than an active test requirement.

The conversion coverage guard compares normalized, generated v2 and v3 OpenAPI
schemas. It must detect differences in paths, types, requiredness, nullability,
defaults, enums, CEL validation, and list/map semantics. Keep an independent v2
schema baseline so changing a shared Go type cannot silently mutate both APIs.

The expected difference set is empty for the initial machinery task. Every
future difference must be explicit and paired with direct and round-trip
conversion cases. A field-name-only reflection comparison is insufficient.

### DEC-008: final v3 delivery removes v1

The final DSC v3 code and generated API surface do not contain the v1 DSC API,
v1 conversion, v1 admissions, v1 scheme registration, or a served v1 CRD
version. Remove v1-specific compatibility annotations only after proving they
have no remaining consumers.

### DEC-010: empty management state means Removed

The effective lifecycle value of an empty DSC component or subcomponent
`managementState` is `Removed`. Normalize only when behavior requires a concrete
state; conversion does not rewrite an absent wire value merely to normalize it.

### DEC-011: retain internal module resource identities in 3.6

Public DSC naming may change after component contracts are accepted, but the
internal AI Hub/Model Registry and Data/Feast CR names, GVKs, and module metadata
remain unchanged in RHOAI 3.6. Their internal renames are deferred work.

### DEC-012: exclude Data Connection Hub

Data Connection Hub is not part of the RHOAI 3.6 DSC v3 `data` stanza. Do not
add its types, conversion, handler projection, status, conditions, or generated
schema as part of these tasks.

### DEC-013: remove v1 in Task 001 behind the odh-cli gate

Task 001 introduces v3, makes it storage/internal, adds v2 <-> v3 identity
conversion, migrates production code, and removes v1 code, conversion,
admissions, scheme registration, tests, and generated serving. The new
operator does not retain or adapt v1.

The required migration boundary is the accepted odh-cli pre-upgrade gate in
DEC-009. It runs while the old v1/v2 operator and its conversion support are
still available, converts any v1-stored DSC to v2, and prevents installation of
the v1-free CRD until v1 is absent from `status.storedVersions`.

### DEC-014: isolate evolving v3 public types

V3 owns its top-level DSC API types. Any nested public stanza that diverges
from v2 must become v3-owned before it changes. Shared
`api/components/v1alpha1` or internal module types must not be edited as a
shortcut to change v3 because doing so can silently change v2 or internal CRDs.

The independent baseline and generated-schema comparison formerly required by
DEC-007 were removed under DEC-035. Version-owned types and explicit v2
compatibility remain required.

### DEC-015: deprecate v2 while keeping it served

The generated v2 CRD version remains `served: true` and `storage: false`, with
`deprecated: true` and a stable `deprecationWarning` that directs clients to
`datasciencecluster.opendatahub.io/v3`; DEC-019 supplies its exact value. V2
admission and bidirectional v2/v3 conversion remain supported for the
deprecation window. Deprecation does not authorize removal; a later v2
retirement requires its own compatibility period, odh-cli storage gate,
accepted decision, and upgrade/rollback qualification.

### DEC-016: odh-cli qualification is separate future work

Task 001 creates and verifies the v1-free operator and generated artifacts but
does not implement, integrate, or qualify odh-cli. It may be marked complete
when its repository subtasks and tests pass. Fresh installation of those
artifacts does not require migration from v1.

DSC-V3-010 is a separate future task that starts from Task 001's completed
artifacts and the externally implemented odh-cli gate. It owns storage
migration, OLM ordering, failure/retry, `status.storedVersions`, and rollback
qualification. Until Task 010 completes, the v1-free artifacts must not be
promoted or installed as an upgrade over releases that may have v1 storage.

### DEC-017: no object does not imply no stored-version cleanup

This decision clarifies and supersedes the no-object shortcut in DEC-009. If no
DSC exists and v1 is present in CRD `status.storedVersions`, the gate skips the
object rewrite but must patch the CRD status subresource, read it back, and
verify v1 is absent before permitting CRD replacement. Mutation-free success is
allowed only when v1 is already absent from `status.storedVersions`.

### DEC-019: exact v2 deprecation warning

The v2 CRD version uses this exact `deprecationWarning` value:

```text
datasciencecluster.opendatahub.io/v2 DataScienceCluster is deprecated; use datasciencecluster.opendatahub.io/v3 DataScienceCluster
```

Tests compare the complete string, including capitalization and punctuation.

### DEC-021: protect the v2 OpenAPI baseline

> Superseded by DEC-035. The baseline described below no longer exists.

A v3-only public API change must not update the independent normalized v2
OpenAPI baseline. The baseline may change only when an accepted decision
explicitly changes the v2 wire contract. Such a change must list every modified
v2 path separately, update v2 admission/conversion compatibility, and include
focused tests proving the v2 change. Regenerating both sides to make a v2/v3
difference disappear is prohibited.

### DEC-035: remove the DSC schema-comparison test package

The `pkg/dsc/compare` test package and its fixtures are removed without a
replacement structural OpenAPI guard or baseline hash. Conversion correctness
continues to be tested beside the v2/v3 API and webhook implementations,
including direct, round-trip, and API-server conversion cases. This removal
does not authorize unreviewed changes to the served v2 wire contract.

### DEC-022: migrate KServe MaaS independently

> Supersession note (2026-09-18): DEC-024 supersedes this decision's reverse
> conversion and no-annotation policy, with the normalization contract now
> specified in DEC-024. The original accepted text below is retained as history. Forward precedence,
> independent task ownership, warning removal, and retained v2 CEL still apply.

- State: `Accepted`
- Applies to: DSC-V3-012
- Supersedes: G5 as a proposed blocker

The KServe/MaaS v3 change is fully determined by existing runtime behavior and
may be implemented after DSC-V3-001 without waiting for Dashboard, AI Hub,
Data, removed-legacy-field, consolidated-contract, or Jira feedback.

V2 keeps both canonical `spec.components.aigateway.modelsAsAService` and
deprecated `spec.components.kserve.modelsAsService`, including the existing
update validation. Remove the custom MaaS field deprecation warnings from both
v2 and v3 admission; DEC-019's warning for use of the entire v2 API version is
unrelated and remains. V3 removes only
`spec.components.kserve.modelsAsService`; canonical
`spec.components.aigateway.modelsAsAService`, MaaS status, and
`ModelsAsAServiceReady` remain unchanged.

Conversion follows the agreed v2 MaaS state selection:

1. V2 -> v3 uses the API-visible canonical
   `aigateway.modelsAsAService.managementState`. The canonical MaaS field
   defaults to `Removed`; canonical `Managed` is preserved, while legacy
   `kserve.modelsAsService.managementState: Managed` selects canonical
   `Managed` only when the KServe parent is `Managed`. An empty or `Removed`
   KServe parent gates legacy `Managed` off, so all other combinations select
   canonical `Removed`.
2. Conversion copies `aigateway.managementState`, except that an empty value is
   set to `Managed` when both v2 `kserve.managementState` and deprecated
   `kserve.modelsAsService.managementState` are `Managed`. This preserves the
   existing legacy fallback that deploys AI Gateway for KServe-hosted MaaS.
3. V3 -> v2 copies canonical MaaS to the canonical v2 AI Gateway path and
   sets deprecated `kserve.modelsAsService.managementState` to `Managed` only
   when the supported provenance marker is present; otherwise it sets it to
   `Removed`. It copies `aigateway.managementState` unchanged and does not
   infer an independent v2 KServe parent state.

"Current runtime precedence" means preserving all three observable decisions
in `internal/controller/modules/aigateway/handler.go`:

1. The projected AI Gateway CR uses the canonical v3 MaaS state selected during
   conversion.
2. MaaS readiness/status is enabled only when canonical v3 MaaS is `Managed`.
3. The AI Gateway platform module uses its non-empty parent state unchanged.
When that state is empty, both KServe parent and legacy MaaS must be
   `Managed` to synthesize `Managed`; an empty KServe parent is therefore
   equivalent to `Removed` for this decision.

V2 retains the existing CEL transition rule on deprecated
`kserve.modelsAsService`. It permits creation of legacy `Managed` objects,
permits `Managed -> Managed`, `Managed -> Removed`, and `Removed -> Removed`,
and rejects `Removed -> Managed`. Removing the custom admission warning does
not remove or weaken this CEL rule. V3 removes the deprecated field and
therefore does not carry that field-specific CEL rule. Conversion must not call
admission or attempt to enforce CEL itself; it must still convert previously
accepted legacy `Managed` objects. Because v3 -> v2 always writes legacy
`Removed`, a subsequent v2 update cannot re-enable the deprecated path.

The `conversion.opendatahub.io/maas-v2-state: legacy-managed` marker preserves
legacy `Managed` intent. A v2 -> v3 -> v2 round trip writes the selected state
to canonical v2 MaaS and restores legacy `Managed` only when the marker is
present. Without provenance, legacy v2 MaaS becomes `Removed`. Both round
trips preserve effective MaaS/AI Gateway behavior, metadata, and unrelated
fields. These are accepted normalization rules for the removed deprecated
field.

Task 012 owns the v3-owned KServe type, explicit conversion and migration,
OpenAPI difference entry, removal of both versioned custom warning hooks,
obsolete runtime-fallback removal, generated artifacts, and direct, semantic
round-trip, webhook, handler, CEL, schema, and envtest integration tests. It
records the final cluster scenario, while Task 011 owns its E2E implementation
and execution. G1-G6 are closed; Dashboard, AI Hub, Data, Training Operator,
and Llama Stack Operator portions of G7 are accepted under their respective
decisions. Task 011 still owns final qualification and E2E execution.

### DEC-023: defer consolidated E2E to final qualification

- State: `Accepted`
- Applies to: DSC-V3-001, DSC-V3-006 through DSC-V3-009, DSC-V3-011, and
  DSC-V3-012

Intermediate implementation tasks complete when their required unit,
conversion, webhook, schema, CEL, handler/controller, and envtest integration
coverage and mandatory repository gates pass. They record the real-cluster
scenario and fixture prerequisites in Outcomes, but E2E implementation or
execution does not block their completion.

DSC-V3-011 consumes those handoffs, implements the consolidated ODH/RHOAI E2E
coverage, and runs it on supported clusters. When no local cluster is
available, Task 011 may hand execution to a named CI job, but it may not omit
the E2E implementation. DSC-V3-010 remains a separate exception because its
purpose is to qualify the external odh-cli gate, storage migration, OLM
ordering, retry, and rollback boundary.

### DEC-024: preserve legacy Managed with a literal marker

- State: `Accepted`
- Date: 2026-09-18
- Applies to: DSC-V3-012; consumed by DSC-V3-005, DSC-V3-006, and DSC-V3-011
- Supersedes: DEC-022's reverse conversion, no-annotation policy, and
  canonical-first MaaS precedence
- Approval: user-approved provenance-only design. The literal marker records
  legacy `Managed` intent. Reverse conversion restores the v2 canonical source
  state while the AI Gateway parent remains migrated.

Reserve `conversion.opendatahub.io/maas-v2-state` with the sole supported value
`legacy-managed`. There is no JSON payload, versioned encoding, or snapshot.
`L` denotes v2 `kserve.modelsAsService.managementState` and `C` denotes
canonical `aigateway.modelsAsAService.managementState`. The canonical field
defaults to `Removed`; an omitted canonical field is therefore observed as
`Removed` during admission. `K` and `A` denote the KServe and AI Gateway parent
`managementState` values.
Emit the marker whenever `L=Managed` and `C` is not `Managed`, including when
`K` is `Removed` or empty. The marker records legacy intent and does not
override the KServe parent gate.

V2 -> v3 preserves canonical `C=Managed`. Legacy `L=Managed` selects canonical
`Managed` only when `K=Managed`; `K=Removed` or empty leaves canonical at
`Removed`. All other combinations select `Removed`. Ignore any incoming marker
and derive `legacy-managed` from visible `C`/`L`.
Incoming stale or unknown values cannot override visible v2 state.

V3 -> v2 always copies the AI Gateway and KServe parents. When the marker is
present, restore the v2 source view with canonical `C=Removed` and legacy
`L=Managed`; this lets unchanged v2 writes retain provenance and lets later
KServe parent changes recalculate effective MaaS. When the marker is absent,
copy the current canonical state and set legacy `L=Removed`. A parent
synthesized as `Managed` remains projected so disabling KServe gates MaaS off
without disabling AI Gateway or Batch Gateway. Consume the reserved marker on
the v2 output; the next forward conversion derives it anew from current
`C`/`L`. Reject unknown marker values when interpreting the marker in reverse
conversion or admission; an empty annotation value is not absence. Unrelated
metadata, spec, status, and conditions remain intact.

V3 admission enforces the marker lifecycle without a projection snapshot:

- On native v3 `CREATE`, strip supplied reserved metadata.
- On v3 `UPDATE`, compare old/new canonical MaaS state `C`. Any change deletes
  the marker permanently. KServe and AI Gateway parent changes do not retire
  legacy provenance.
- If those states do not change, `OldObject` supplies the authoritative marker,
  regardless of a new object's attempt to add, replace, omit, or delete it.
  Reject unknown authoritative marker values.
- For a request originating from v2, conversion output is authoritative: a
  generated legacy-only marker is retained, while a v2 object whose canonical
  MaaS is already `Managed` has no marker and cleans legacy `Managed` to
  `Removed`.
- Deletion is permanent for that legacy intent, including after a later
  revert. If the old object has no marker, a v3 update cannot introduce one.
  A later v2 conversion derives a new marker solely from its current `C`/`L`
  pair.
- Unrelated edits preserve the old marker and unrelated annotations.

Reuse existing `componentApi` AI Gateway/canonical value types in both API
versions. Do not introduce a canonical pointer or duplicated v2/v3 AI Gateway
wrappers. Keep existing JSON names, defaults, optionality, validations, shared
module schemas, and the independent v2 OpenAPI baseline unchanged. Absence
versus `{}` is not restored by conversion; API-server defaulting remains
separate from typed conversion.

Tests must assert this deliberate normalization: marker-bearing reads restore
canonical `C=Removed` and legacy `L=Managed`, while parent `A` stays projected.
Without the marker, canonical `C` stays projected and legacy `L=Removed`.
Cover all four valid `C`/`L` combinations and prove that a KServe parent disable
removes effective MaaS without removing the migrated AI Gateway parent.

Runtime reads only canonical v3 MaaS. Remove both custom MaaS field-warning
hooks; retain DEC-019's API-version warning and the existing v2 CEL transition
matrix, including rejection of `Removed -> Managed`. A retained marker
exposes legacy `Managed` to v2; after retirement, reverse
conversion exposes `Removed` and the retained CEL guard applies. Status and
condition names do not change.

There is no supported installed intermediate-v3 release with the pre-Task012
legacy KServe field. Do not add a migration or runtime fallback for that
development-only shape. Supported legacy input enters through v2; Task 010's
separate upgrade gates remain in force.

### DEC-025: expose Model Registry as public AI Hub in v3

- State: `Accepted`
- Applies to: v3 API, v2 conversion, defaulting, module projection, and status
- Namespace-field note: DEC-028 supersedes this decision's v3
  `registriesNamespace` name with `applicationNamespace`; all other DEC-025
  behavior remains authoritative.

The public v3 DSC uses `spec.components.aiHub` and
`status.components.aiHub`, with the existing Model Registry component wire
shape (`managementState` and `registriesNamespace`). V2 continues to expose
`modelregistry` in both spec and status. V2 and v3 use distinct Go types, and
conversion copies the fields explicitly between them.

The v2 and v3 component fields map directly in both conversion directions.
Top-level status condition `ModelRegistryReady` maps to `AIHubReady` in v3 and
back again in v2. All other condition fields and unrelated conditions are
preserved.

An empty management state remains effectively `Removed`. A managed AI Hub with
an empty `registriesNamespace` receives the existing build-specific default;
removed or empty-state AI Hub does not receive a namespace default. Existing
namespace validation and immutability rules remain unchanged.

The internal GVK `AIHub`, CR name `default-aihub`, module name `modelregistry`,
internal `instancesNamespace`, and module CR readiness contract are unchanged.

Task 012 covers direct/round-trip conversion, reserved metadata, admission
CREATE/UPDATE including edit-then-revert, schema/CEL, handler behavior, and
envtest/integration through the real webhook. Under DEC-023, new E2E code and
execution belong to Task 011; Task 012 may only fix E2E fixtures as needed to
compile and must record the full cluster scenario handoff.

### DEC-026: align Platform module names with DSC v3

- State: `Accepted`
- Applies to: Platform API versioning, conversion, module projection, and
  generated Platform CRDs

Platform v1alpha2 is the conversion hub and sole storage version. Platform
v1alpha1 remains served as a compatibility spoke. The served v1alpha1 fields
`spec.modules.modelregistry` and `spec.modules.feastoperator` map directly to
the v1alpha2 fields `spec.modules.aiHub` and `spec.modules.data`, respectively.
Both versions keep one `ManagementSpec` for each top-level field and preserve
Managed, Removed, and empty values in both conversion directions. All other
Platform module fields, metadata, status, and unrelated module state are
copied unchanged.

This is a public Platform API rename only. Internal module identifiers
`modelregistry` and `feastoperator`, their handlers, GVKs, singleton CR names,
manifest paths, and release metadata remain unchanged. No nested Data
FeatureStore or DataRegistry fields are added to Platform by this decision.
The module reconciler may use an explicit adapter between the public v1alpha2
shape and the existing internal handler state structure.

### DEC-027: finalize the Dashboard v3 public shape

- State: `Accepted`
- Applies to: v3 Dashboard spec, v2 conversion, lifecycle behavior, status,
  conditions, and Dashboard module projection
- Evidence: RHOAIENG-94805 comment 18567363, dated 2026-09-22, recording
  agreement among Dashboard, Workbenches, and Platform participants; related
  behavior is documented by RHOAIENG-88605.

The v3 Dashboard component is a structural group with two independently
managed children:

```yaml
spec:
  components:
    dashboard:
      standard:
        managementState: Managed
      maasPortal:
        managementState: Managed
```

`standard` is the core Dashboard and `maasPortal` is the MaaS Consumer Portal.
The parent `dashboard` has no management state. Both children accept `Managed`
and `Removed`; empty values are effectively `Removed` under DEC-010. The portal
retains its existing `Removed` default. All four independent child-state
combinations are valid. The shared Dashboard operator is enabled while either
child is `Managed` and removed only after both are `Removed`.

The v2-to-v3 spec mapping is direct and deterministic:

| v2 path | v3 path |
| --- | --- |
| `spec.components.dashboard.managementState` | `spec.components.dashboard.standard.managementState` |
| `spec.components.dashboard.maasConsumerPortal.managementState` | `spec.components.dashboard.maasPortal.managementState` |

V3-to-v2 conversion performs the inverse mapping. Existing Dashboard status
fields and the `MaaSConsumerPortalAvailable` condition retain their current
wire names and semantics unless a later accepted decision supersedes them.
Unrelated metadata, status, and conditions are preserved through conversion.

The existing internal Dashboard CR and module identity are unchanged.
`standard.managementState` projects to the Dashboard CR's existing core
`managementState`; `maasPortal.managementState` projects directly to
`spec.maasConsumerPortal.managementState`. No new CEL rule or parent-state
conflict rule is introduced.

### DEC-028: use applicationNamespace for the public v3 AI Hub field

- State: `Accepted`
- Applies to: v3 AI Hub spec/status naming, v2 conversion, defaulting,
  validation, and module projection
- Supersedes: DEC-025 only for the v3 namespace field name
- Evidence: updated RHOAIENG-95340 description, including the explicit
  `registriesNamespace` to `applicationNamespace` mapping.

The public v3 AI Hub component uses `applicationNamespace`; `registriesNamespace`
is not exposed in the v3 API. V2 retains its existing public names:

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

The conversion mapping is explicit in both directions:

| V2 | V3 |
| --- | --- |
| `components.modelregistry.managementState` | `components.aiHub.managementState` |
| `components.modelregistry.registriesNamespace` | `components.aiHub.applicationNamespace` |
| `status.components.modelregistry.managementState` | `status.components.aiHub.managementState` |
| `status.components.modelregistry.registriesNamespace` | `status.components.aiHub.applicationNamespace` |
| `ModelRegistryReady` | `AIHubReady` |

Absent, empty, defaulted, and custom namespace values retain the existing
build-specific behavior. A managed empty v2 namespace uses the existing ODH
or RHOAI default; conversion does not invent a second default. Existing
validation, managed-state immutability, Removed behavior, v2 read-modify-write
preservation, and round-trip guarantees remain unchanged.

The handler projects public v3 `applicationNamespace` to the unchanged
internal `instancesNamespace`. Internal GVK `AIHub`, singleton name
`default-aihub`, module/manifest identity `modelregistry`, and related metadata
remain unchanged for RHOAI 3.6.

### DEC-029: finalize the Data, Feature Store, and Data Registry public shape

- State: `Accepted`
- Applies to: v3 Data API shape, v2 compatibility, lifecycle conversion, and
  Data/Feast contract scope
- Evidence: resolved `RHOAIENG-95339` on 2026-09-22, with the agreed behavior
  recorded in the Jira description and final comment

The v3 `data` component is a structural group with no parent
`managementState`. Feature Store and Data Registry are independently managed:

```yaml
components:
  data:
    featureStore:
      managementState: Managed
    dataRegistry:
      managementState: Managed
```

V2 carries an explicit compatibility field under the existing FeastOperator
component:

```yaml
components:
  feastoperator:
    managementState: Managed
    dataRegistry:
      managementState: Managed
```

The conversion is direct and bidirectional:

| V2 | V3 |
| --- | --- |
| `components.feastoperator.managementState` | `components.data.featureStore.managementState` |
| `components.feastoperator.dataRegistry.managementState` | `components.data.dataRegistry.managementState` |

Empty or absent management state is effectively `Removed`. The explicit v2
compatibility field makes the mapping lossless without an annotation stash.
Data Connection Hub and internal Data/Feast CR, GVK, module, manifest, and
metadata renames are out of scope for RHOAI 3.6 and deferred beyond this
contract. Existing Feast status and runtime behavior are preserved; runtime
child projection is separate implementation work and must not invent a parent
Data management state.

### DEC-030: use Platform v1alpha2 throughout internal code

- State: `Accepted`
- Applies to: Platform runtime, module handler interfaces, resource projection,
  and shared Platform helpers
- Evidence: user direction on 2026-09-23

Platform v1alpha2 is not only the storage/hub API; it is the sole typed
Platform API used by internal production code and tests. This includes module
handler interfaces, module registries, readiness evaluation, Platform
reconciliation, DSC/DSCI Platform resource projection, and shared GVK helpers.
The public v1alpha2 field names `aiHub` and `data` are used by handlers, while
the internal handler identifiers remain `modelregistry` and `feastoperator`.
Platform v1alpha1 remains served only as the compatibility conversion spoke
and is referenced internally only where registering/testing that conversion or
adding both versions to a scheme is necessary.

### DEC-031: retain Training Operator and Llama Stack Operator in v3 for the current scope

- State: `Superseded`
- Date: `2026-09-24`
- Applies to: DSC-V3-005, DSC-V3-006, and DSC-V3-014
- Supersedes: the proposed G6 premise that these stanzas are removed from v3
- Superseded by: DEC-032

This entry records the initial decision as made on 2026-09-24. It is retained
for history; its keep-in-v3 conclusion is no longer normative. DEC-032 limits
the current shape to an interim carry-over and leaves the final v3 presence of
each stanza to its component owner.

The v3 API explicitly retains both deprecated component stanzas with their
existing public names and types:

- `spec.components.trainingoperator` and its status counterpart remain
  `DSCTrainingOperator` and `DSCTrainingOperatorStatus`.
- `spec.components.llamastackoperator` and its status counterpart remain
  `DSCLlamaStackOperator` and `DSCLlamaStackOperatorStatus`.

The v2 and v3 JSON names, optionality, defaults, validation rules, deprecation
messages, status fields, and conditions remain unchanged. In particular, the
Training Operator re-enablement CEL validation is retained. Conversion copies
both stanzas directly in both directions, including absent, empty, and
non-empty values, with no annotation or other fidelity mechanism.

This decision makes no runtime behavior change. It does not rename either
stanza to `trainer` or `ogx`, remove either stanza, change component handlers,
or alter the existing deprecation guidance. Any future removal, rename, or
behavioral change requires a separate accepted decision and its own
bidirectional conversion and round-trip tests.

### DEC-032: keep deprecated operator stanzas as an interim baseline only

- State: `Accepted`
- Date: `2026-09-24`
- Applies to: DSC-V3-014, DSC-V3-015, DSC-V3-016, and final DSC v3 qualification
- Supersedes: DEC-031's final-retention conclusion

The current v3 `trainingoperator` and `llamastackoperator` stanzas remain in
place only as an interim verification baseline. Their current identity mapping
does not decide whether either stanza belongs in the final v3 API, and no code
or runtime change is authorized by this decision.

The component owners make the keep-or-remove decision independently in
DSC-V3-015 (`RHOAIENG-96548`) and DSC-V3-016 (`RHOAIENG-96549`). Each Jira
records the selected outcome and rationale. If either owner selects removal,
that Jira requires a separate follow-up Jira defining the validation checks
needed for the removal. Any resulting API, conversion, or runtime work requires
its own implementation task and applicable accepted decision.

Until those decisions are recorded, preserve the existing v3 fields and
identity conversion as-is. Do not treat their presence as final approval to
retain them or invent removal behavior.

### DEC-033: remove LlamaStackOperator from final DSC v3

- State: `Accepted`
- Date: `2026-09-24`
- Applies to: DSC-V3-005, DSC-V3-006, DSC-V3-015, DSC-V3-011, and the
  follow-up API/conversion implementation
- Evidence: RHOAIENG-96548 comment 18613541 (2026-09-24)
- Supersedes: DEC-032 for final v3 LlamaStackOperator spec/status presence

The final DSC v3 API omits both `spec.components.llamastackoperator` and
`status.components.llamastackoperator`. OGX remains a separate component. The
recorded rationale is that OGX GA'd in RHOAI 3.5 as the replacement, the
upstream rename is complete, and v2 already carries the old component as
`Removed`. The Jira comment points to RHOAIENG-94809 for the conversion
contract; the follow-up must confirm the locally accepted contract there before
implementation rather than infer how removed v3 fields map back to v2.

Keep the deprecated v2 spec and status paths for compatibility. V2 must be a
retirement-only path for
`spec.components.llamastackoperator.managementState`:

- On CREATE, absent/empty or `Removed` is allowed; `Managed` is rejected.
- On UPDATE, an existing `Managed` value may remain unchanged so unrelated DSC
  edits remain possible. The only allowed transition away from `Managed` is
  `Removed` or empty; DEC-010 makes empty effectively `Removed`.
- An old `Removed` or empty value cannot transition to `Managed`. No new or
  re-enabled `Managed` state is allowed.
- This is spec validation. V2 status remains controller-owned; v3 exposes
  neither the spec nor status field.

The v2 CRD must enforce the transition rule with CEL and have schema/admission
tests for create, unchanged existing Managed, Managed-to-Removed/empty, and
rejected new or re-enabled Managed values. The separate implementation task
must also define v2/v3 spec and status conversion, v3 schema removal, both
direct conversion directions, both round trips, and v2 read-modify-write
coverage under DEC-003/DEC-007. This decision does not itself implement those
changes. Update the independent v2 OpenAPI baseline only for the explicitly
accepted CEL change, if the existing schema does not already enforce it.

Implementation note (2026-09-25): the current v2 spoke drops this retired
spec/status field on conversion to v3 and reports `Removed` for both fields on
conversion from v3. The v2 CEL uses `optionalOldSelf`. Tasks 018/019/021/024
track additional storage, API-server, and E2E evidence; this note does not
close Task 015's separate Jira-link follow-up.

Use the same transition expression as the existing TrainingOperator guard:

```cel
!has(self.managementState) || self.managementState != 'Managed' || (has(oldSelf.managementState) && oldSelf.managementState == 'Managed')
```

### DEC-034: remove TrainingOperator from DSC v3 shipped in 3.6 GA

- State: `Accepted`
- Date: `2026-09-24`
- Applies to: DSC-V3-005, DSC-V3-006, DSC-V3-016, DSC-V3-011, and the
  follow-up API/conversion implementation
- Evidence: RHOAIENG-96549 comment 18606510 (2026-09-24)
- Supersedes: DEC-032 for final v3 TrainingOperator spec/status presence

The DSC v3 API shipped in 3.6 GA omits both
`spec.components.trainingoperator` and `status.components.trainingoperator`.
The Jira comment records the removal direction and requests an additional
acknowledgment from Christoph Goern. This decision records that removal
direction as accepted while keeping the acknowledgment request noted in Task
016.
Existing v2 deprecation guidance recommends Trainer v2 and instructs users to
retire the old TrainingOperator CR.

Keep the deprecated v2 spec and status paths for compatibility. V2 must be a
retirement-only path for
`spec.components.trainingoperator.managementState`:

- On CREATE, absent/empty or `Removed` is allowed; `Managed` is rejected.
- On UPDATE, an existing `Managed` value may remain unchanged so unrelated DSC
  edits remain possible. The only allowed transition away from `Managed` is
  `Removed` or empty; DEC-010 makes empty effectively `Removed`.
- An old `Removed` or empty value cannot transition to `Managed`. No new or
  re-enabled `Managed` state is allowed.
- Preserve the existing v2 CEL rule in the generated v2 schema. V2 status
  remains controller-owned; v3 exposes neither the spec nor status field.

The separate implementation task must verify the existing CEL rule against
the create/update cases above and define v2/v3 spec and status conversion, v3
schema removal, both direct conversion directions, both round trips, and v2
read-modify-write coverage under DEC-003/DEC-007. This decision does not itself
implement those changes. Preserve the independent v2 OpenAPI baseline unless
the accepted v2 validation contract requires an explicit recorded difference.
Implementation note (2026-09-25): the current v2 spoke drops this retired
spec/status field on conversion to v3 and reports `Removed` for both fields on
conversion from v3. The v2 CEL uses `optionalOldSelf`. Tasks 018/019/021/024
track additional storage, API-server, and E2E evidence; this note does not
close Task 016's separate Jira-link follow-up.

The v2 guard to preserve is:

```cel
!has(self.managementState) || self.managementState != 'Managed' || (has(oldSelf.managementState) && oldSelf.managementState == 'Managed')
```

### DEC-009: use an odh-cli gate before removing v1

- State: `Accepted`
- Applies to: future DSC-V3-010/RHOAIENG-94814 qualification and upgrade
  delivery; not Task 001 implementation

Kubernetes rejects removal of a CRD version while it remains in
`status.storedVersions`, and changing the storage marker does not rewrite stored
objects. Therefore the v1-free CRD must not be applied until the following
idempotent gate succeeds:

1. Before OLM applies the new CRD, odh-cli runs against the installed v1/v2 CRD
   while the old operator and its conversion webhook are healthy.
2. The gate reads the DSC CRD and its `status.storedVersions`. It succeeds
   without mutation only when v1 is already absent. DEC-017 defines the
   no-object case.
3. If v1 is recorded and the singleton DSC exists, the gate reads and updates
   that object through the served v2 API, preserving spec, status, and metadata,
   so the API server rewrites it in v2 storage. The gate verifies the rewrite;
   it must not infer success from the CRD storage marker alone.
4. Only after every stored DSC is verified as no longer requiring v1 does the
   gate patch the CRD status subresource to remove v1 from
   `status.storedVersions`, then read it back and verify v1 is absent.
5. Any unsupported source state, failed conversion, webhook unavailability,
   conflicting mutation, incomplete verification, or status update failure
   blocks the upgrade. Re-running the gate is safe after partial completion.
6. OLM may then install the new CRD, which serves deprecated v2 and v3, stores
   only v3, and contains no v1. The new operator registers `/convert` through
   the v2 spoke with `conversionReviewVersions: [v1]`.
7. After the new webhook is healthy, the upgrade flow reads and updates the DSC
   through v3 to force v3 storage, verifies v3 is present in
   `status.storedVersions`, and removes v2 from that status only after no object
   remains stored as v2. V2 remains served and deprecated.

The gate must explicitly report its last safe rollback point. Before the
post-upgrade v3 rewrite, rollback to a v1/v2 release is supported. After data is
stored as v3, rollback requires a tested v3 -> v2 down-migration before the old
CRD/operator is restored; otherwise rollback is unsupported and must be
blocked. Supported source releases and both ODH/RHOAI packaging paths must be
covered by qualification tests.

## Pending decisions

### DEC-018: enforce upgrade-promotion blocking

- State: `Proposed`
- Blocks: DSC-V3-010 completion and any upgrade promotion/install of the
  v1-free CRD; it does not block Task 001 implementation or fresh install

Release engineering must record the exact control that consumes Task 010's
result and prevents a failed or missing gate from publishing the upgrade bundle
or advancing OLM. Record its repository/system owner, configuration or workflow
identifier, required evidence, failure signal, retry behavior, and a test that
proves failure cannot be bypassed. A prose warning or task status is not an
enforcement mechanism. Until this is accepted, Task 001 may be developed and
marked implementation-complete on its feature branch, but it must not merge to
any branch whose automation publishes or syncs the v1-free upgrade artifact.

### DEC-020: freeze Task 010's supported execution matrix

- State: `Proposed`
- Blocks: starting DSC-V3-010

Record locally every supported ODH/RHOAI source release, source and target
bundle/catalog/image reference, supported cluster version, packaging entry
point, required permissions, odh-cli version/invocation, and CI/E2E job. Task
010 must not discover or choose this matrix from Jira while executing.

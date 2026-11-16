# Module Framework QA Testing Strategy

**Date:** 2026-11-05

## Overview

This document defines the testing strategy for the Platform controller and module handlers (`internal/controller/modules/`), the PlatformModule controller (`internal/controller/platformmodules/`), and the reconciliation utilities in `pkg/controller/`.

Platform creates and removes trackers, coordinates the unified DAG, and aggregates tracker status. PlatformModule renders and deploys module operators, watches module CR readiness, and performs inventory-based cleanup. On OpenShift, DSC/DSCI controllers retain ownership of module CRs; on xKS, users or GitOps manage their lifecycle. See the [tracker lifecycle design](platformmodule-tracker-design.md) for these boundaries.

Shared reconciliation primitives are partly implemented in this repository and partly delegated to the versioned `github.com/opendatahub-io/odh-platform-utilities/framework` dependency declared in [go.mod](../go.mod). Local tests validate the operator's integration with that framework.

The strategy is organized into three validation levels, progressing from isolated unit coverage of the shared utilities through consumer integration to module contract enforcement.

---

## Level 1: Library-Level Validation

Unit and integration tests cover the local implementations and framework adapters in `pkg/controller/`. These run in CI via the `test-unit.yaml` GitHub Actions workflow (`make unit-test`), which executes Ginkgo with envtest assets, 8 parallel procs, and `--cover --coverprofile=cover.out`. Standard Go tests and fake-client tests run alongside envtest suites. Results are uploaded to Codecov.

### Coverage Summary

Coverage tables use the successful local ODH `make unit-test` run from 2026-11-04. Percentages are statement-weighted per package from `cover.out`, rounded to one decimal place. They are a snapshot, not minimum thresholds or RHOAI measurements. Refresh them after relevant source or test changes.

| Package | Test File(s) | Coverage |
|---------|-------------|----------|
| `actions/cacher` | `cacher_test.go` | 100.0% |
| `actions/cleanup` | `cleanup_test.go` | 94.1% |
| `actions/deleteresource` | `action_delete_resources_test.go` | 100.0% |
| `actions/dependency/certmanager` | `bootstrap_test.go`, `monitor_test.go` | 83.1% |
| `actions/deploy` | `action_deploy*_test.go` | 100.0% |
| `actions/gc` | `action_gc_test.go` | 50.0% |
| `actions/render/kustomize` | `action_render_manifests_test.go` | 73.0% |
| `actions/render/template` | `action_render_templates_test.go` | 100.0% |
| `actions/status/deployments` | `action_deployments_available_test.go` | 100.0% |
| `actions/status/platformrelease` | `platform_release_test.go` | 77.8% |
| `dag` | `dag_test.go` | 69.6% |
| `gates` | `gates_test.go` | 80.0% |
| `predicates/resources` | `resources_test.go` | 82.0% |

Delegated packages such as `actions/dynamicownership`, `actions/render/helm`, `actions/resourcecacher`, `actions/sanitycheck`, `conditions`, `handlers`, and several predicate packages have no local executable statements in this profile. This is not zero coverage of their framework implementations: dependency code is excluded from the local profile. Conversely, 100% coverage of a small adapter does not imply full coverage of the external implementation.

Additional tested packages outside the core action/predicate tree:

| Package | Test File(s) | Coverage |
|---------|-------------|----------|
| `cloudmanager` | `action_*_test.go`, `run_hooks_test.go` | 89.8% |
| `monitor` | `crd_test.go`, `operator_test.go`, `result_test.go` | 96.3% |
| `precondition` | `custom_test.go`, `monitor_crd_test.go`, `monitor_operator_test.go`, `precondition_test.go`, `runlevel_gate_test.go` | 98.1% |
| `provision` | `gates_action_test.go`, `gating_test.go`, `runlevel_tracker_test.go`, `unified_test.go` | 82.5% |
| `reconciler` | `reconciler_test.go`, `reconciler_finalizer_test.go` | 66.7% |
| `types` | `types_test.go` | 0.0% |

### Gaps

- **Framework boundary**: Local coverage does not measure delegated framework code. Review the framework's own tests when upgrading it, and retain operator integration checks for the contracts used here.
- **Local behavior**: `types` has tests but its executable statements are not covered by this run. DAG, GC, and reconciler coverage is partial; prioritize missing failure and recovery behavior over testing aliases.

---

## Level 2: Consumer Integration Validation

The opendatahub-operator's own test suite validates `pkg/controller/` library usage at two tiers.

### Unit/Integration (fake client and envtest)

The default `make unit-test` includes `internal/`, `pkg/`, `tests/integration/`, `api/config/`, `api/datasciencecluster/`, `api/services/v1alpha1/`, and `cmd/cloudmanager/`. CI overrides `TEST_SRC` to select `internal/`, `pkg/`, and those three API trees. Additional make targets run clusterhealth, manifest-tools, scope rules, and module-registration completeness checks. The module suites exercise orchestration, handlers, and tracker reconciliation without requiring a live cluster.

| Package / Handler | Test File(s) | Coverage |
|---------------|-----------|----------|
| Platform orchestration | `internal/controller/modules/*_test.go` | 80.2% |
| PlatformModule controller | `internal/controller/platformmodules/*_test.go` (including SSA envtest and OTel namespace tests) | 71.2% |
| Built-in registration | `builtin/handlers_test.go` | 100.0% |
| `aigateway` | `aigateway/handler_test.go` | 96.3% |
| `aihub` | `aihub/handler_test.go`, `aihub/status_test.go` | 100.0% |
| `aipipelines` | `aipipelines/handler_test.go` | 82.4% |
| `dashboard` | `dashboard/handler_test.go` | 93.4% |
| `data` | `data/handler_test.go` | 82.3% |
| `databaseservice` | `databaseservice/handler_test.go` | 94.3% |
| `kserve` | `kserve/handler_test.go`, `kserve/handler_aihub_test.go` | 56.0% |
| `mcplifecycleoperator` | `mcplifecycleoperator/handler_test.go` | 94.1% |
| `mlflowoperator` | `mlflowoperator/handler_test.go` | 66.7% |
| `monitoring` | `monitoring/handler_test.go` | 92.2% |
| `ogx` | `ogx/handler_test.go` | 58.8% |
| `ray` | `ray/handler_test.go` | 89.5% |
| `sparkoperator` | `sparkoperator/handler_test.go` | 88.2% |
| `trainer` | `trainer/handler_test.go` | 89.5% |
| `trustyai` | `trustyai/handler_test.go` | 88.2% |
| `workbenches` | `workbenches/handler_test.go` | 85.7% |

Paths in the handler rows are relative to `internal/controller/modules/`. Fetch charts and manifests with `make get-manifests` before running compliance tests; missing assets are a setup failure, not a coverage measurement.

Tracker architecture regression checks:

| Behavior | Tests |
|----------|-------|
| Tracker creation/removal and module status aggregation | `modules/lifecycle_integration_test.go` (fake client) |
| Alphabetical component handler enumeration and suppression; unified runlevel ordering, timeouts, and reverse cleanup | `components/registry/registry_test.go`, `pkg/controller/provision/`, `pkg/controller/precondition/runlevel_gate_test.go` |
| Upgrade gates, release handshake, durable DAG progression, and a registry containing only runlevel 0 | `modules/modules_controller_actions_gates_test.go`, `modules/readiness_test.go`, `platformmodules/platformmodule_controller_test.go` |
| Module-CR readiness and atomic inventory/release commit after apply and deletion observed on a later reconciliation, retry/restart recovery, ownership checks with delete preconditions, protected/unmanaged resources, and two-phase finalization | `platformmodules/platformmodule_controller_actions_test.go` (fake client and interceptors) |
| Completion, restart, status-write recovery, and removal against a real API server | `platformmodules/platformmodule_controller_integration_test.go` (running controller in envtest) |
| Owned-tracker finalization, deletion conflicts/ownership changes, and tracker-side upgrade gates | `modules/modules_controller_actions_test.go`, `platformmodules/module_gates_test.go` |
| Shared namespace inputs, source-specific Platform projections, and owner-reference error/no-op handling | `modules/platform_context_test.go`, `modules/modules_controller_actions_owner_test.go` |
| Operator image/env injection, ConfigMap updates without Deployment changes, and OTel namespace discovery/watches | `platformmodules/platformmodule_controller_actions_inject_env_test.go`, `platformmodules/platformmodule_controller_actions_platform_config_test.go`, `platformmodules/otel_namespace_test.go` |
| Platform v1alpha1/v1alpha2 conversion, including DAG checkpoint preservation | `api/config/v1alpha1/platform_conversion_test.go` |

The `components/`, `modules/`, and `platformmodules/` paths above are relative to `internal/controller/`; `api/` and `pkg/` paths are repository-relative.

### End-to-End (cluster-based)

E2E tests in `tests/e2e/` exercise the full module lifecycle (provision, update, removal) on a live cluster. These run via `make e2e-test` on OpenShift or `make e2e-test-xks` on KinD/AKS. Key module E2E test files:

- `tests/e2e/data_module_test.go`
- `tests/e2e/aigateway_test.go`
- `tests/e2e/aihub_test.go`
- `tests/e2e/datasciencepipelines_test.go`
- `tests/e2e/dashboard_test.go`
- `tests/e2e/databaseservice_test.go`
- `tests/e2e/kserve_test.go`
- `tests/e2e/mcplifecycleoperator_test.go`
- `tests/e2e/mlflowoperator_test.go`
- `tests/e2e/monitoring_test.go`
- `tests/e2e/ogx_test.go`
- `tests/e2e/ray_test.go`
- `tests/e2e/sparkoperator_test.go`
- `tests/e2e/trainer_test.go`
- `tests/e2e/trustyai_test.go`
- `tests/e2e/workbenches_test.go`

Additional E2E tests validate library-level behavior at the system level:

- `tests/e2e/dag_ordering_test.go` -- DAG resolution and ordering
- `tests/e2e/resilience_test.go` -- error recovery and retry
- `tests/e2e/deletion_test.go` -- cleanup and GC chains
- `tests/e2e/circuit_breaker_test.go` -- circuit breaker behavior

---

## Level 3: Contract Enforcement Validation

Contract interfaces define the behavioral expectations that any consumer or handler implementation must satisfy. These are validated through compile-time assertions, structural compliance tests, and behavioral compliance tests.

### Contract Interfaces

| Interface | Location | Purpose |
|-----------|----------|---------|
| `ModuleHandler` | `internal/controller/modules/types.go` | Manifest descriptors, DSC/DSCI configuration projection, module CR construction/deletion, and status access; PlatformModule owns operator deployment and inventory cleanup |
| `ComponentHandler` | `internal/controller/components/registry/registry.go` | Component lifecycle for in-tree components |
| `PlatformObject` | `api/common/types.go` | Common interface for reconciled platform objects, including Platform and PlatformModule |
| `dag.Node` | `pkg/controller/dag/dag.go` | Named DAG vertex with a runlevel |
| `dag.ReadinessChecker` | `pkg/controller/dag/dag.go` | Readiness probing for DAG gating |

### Compliance Tests

- **`manifests_compliance_test.go`** (`internal/controller/modules/`): Validates rendered Helm charts and Kustomize manifests against allowed Kubernetes resource kinds, including module-specific exceptions and DatabaseService SCC gates. Requires assets from `make get-manifests`; missing assets fail the checks.

- **`handler_compliance_test.go`** (`internal/controller/modules/`): Exercises built-in `ModuleHandler` implementations against a common set of structural assertions: non-empty unique names, valid GVKs, manifest source presence, CR/GVK consistency, nil-safety for `IsEnabled` and `WriteDSCComponentStatus`, and optional interface conformance (`ReadyConditionTyper`, `ContainerNamer`, `DeploymentNamer`, `SubmoduleConditionProvider`). `builtin/handlers_test.go` and the command/E2E scope checks keep handler registration and test scope aligned.

- **`lifecycle_integration_test.go`** (`internal/controller/modules/`): Uses a fake client to test tracker creation on enablement, tracker deletion on disablement, status aggregation, and stale module status. Operator resource deployment and finalization are tested separately in `internal/controller/platformmodules/`; this file does not exercise the complete controller pipeline in envtest.

- **`platformmodule_controller_actions_test.go`** (`internal/controller/platformmodules/`): Focused fake-client checks cover release publication, cleanup retries and API error priority for completion and finalization, and independence from module CR runtime readiness. Shared deploy-action behavior is covered in `pkg/controller/actions/deploy/`.

- **`platformmodule_controller_integration_test.go`** (`internal/controller/platformmodules/`): Runs the production PlatformModule controller against envtest, covering watches, the action pipeline, status publication, restart recovery, and finalization. The lifecycle scenario is described below.

- **Shared module E2E checks** (`tests/e2e/components_test.go`): Existing module enable/disable cases wait for tracker Ready and the expected committed release, then fetch every inventory entry and require that it exists without a deletion timestamp. Known operator names are also required in inventory when supplied by the handler. These existence checks do not detect omitted entries; complete expected-inventory comparisons use the controlled envtest fixture, which also fetches every committed resource, including its ownerless protected Lease. The shared cases retain Platform/tracker ownership checks, recreation of a deleted owned ConfigMap, and removal checks for remaining owned Deployments without saving inventory between cases. AI Pipelines and Dashboard independently check their known operator Deployment's current tracker controller identity. Component label selectors continue to identify workloads separately from tracker-owned operators. Data checks its known FeastOperator CRD's inventory entry and retention after removal, and calls the tracker helper directly to include Tier3 alongside Smoke/Tier1. These run against the deployed operator on OpenShift or xKS wherever the module suite is supported.

### PlatformModule Controller Integration Scenario

[`TestPlatformModuleControllerLifecycle`](../internal/controller/platformmodules/platformmodule_controller_integration_test.go) starts a real API server, etcd, manager, cached client, and production PlatformModule reconciler. The test changes API objects and waits for controller reactions; it never calls actions or `Reconcile` directly. The fake client is used only to initialize cluster settings before the manager starts.

The scenario models standalone Kubernetes: the DSCInitialization CRD is absent, and the test writes Platform DAG status and a generic module CR's status in place of their controllers. Local ConfigMap and Lease manifests stand in for operator resources. No external operator, Deployment controller, or garbage collector runs, so this verifies controller/API integration rather than operator rollout, workload execution, or Kubernetes garbage collection.

The opening state is seeded directly, rather than produced by running a previous operator image: `status.release=1.0.0`, inventory `[old-config]`, tracker readiness conditions True, and module CR Ready=True. Only the new controller is started. `old-config` is initially live; its test finalizer holds deletion only after the new manifests make it stale. Removing the initial module CR after phase 2 deliberately tests release publication independently from runtime readiness; it is not an automatic upgrade action.

A closed runlevel schedules a retry without failing provisioning or publishing a new release. `Ready` can therefore stay True for an existing healthy module while `status.release` still identifies the previous deployment. The DAG checks the committed release separately. Once deployment starts, pending cleanup returns a scheduled stop and makes `ProvisioningSucceeded=False`; release and inventory remain unchanged until cleanup and publication succeed. The module CR's own Ready condition is never overwritten by the tracker.

### Compile-Time Assertions

Handler implementations use compile-time interface satisfaction checks (e.g., `var _ ModuleHandler = (*Handler)(nil)`) to guarantee that new handlers implement all required methods. This is enforced at build time and requires no runtime test execution.

---

## Test Environments

| Environment | Scope | Infrastructure | CI Workflow |
|-------------|-------|---------------|-------------|
| Unit / envtest | `pkg/controller/`, `internal/controller/`, selected API trees including `api/config/` | GitHub Actions runner, fake client and envtest (kubebuilder assets) | `test-unit.yaml` |
| Integration | Full operator build + OLM catalog + cluster deploy | OpenShift cluster (label-gated) | `test-integration.yaml` |
| E2E (OpenShift) | Full operator on OpenShift with real components | OpenShift cluster | PR-triggered via `ci-build-push-e2e-tests-on-pr.yaml` |
| E2E (xKS / KinD) | Operator on vanilla Kubernetes (KinD) | KinD cluster on GitHub Actions runner | `test-kind-odh-e2e.yaml` |
| Cloudmanager E2E | Cloudmanager-specific flows | Dedicated test environment | `test-cloudmanager-e2e.yaml` |
| Gateway integration | Gateway controller flows | Dedicated test environment | `test-gateway-integration.yaml` |

### Test Utilities

| Utility | Location | Purpose |
|---------|----------|---------|
| envtest wrapper | `pkg/utils/test/envt/` | Simplified envtest setup with CRD registration and cert-manager support |
| Fake client | `pkg/utils/test/fakeclient/` | Pre-configured fake Kubernetes client for unit tests |
| JQ matchers | `pkg/utils/test/matchers/jq/` | Gomega matchers for asserting on unstructured Kubernetes objects using JQ expressions |

---

## Gaps and Future Work

1. **Framework upgrade compatibility**: The shared framework is already a versioned Go module. Compilation and operator integration tests catch some incompatibilities, but dependency upgrades also need review of framework behavior and its upstream tests. Local coverage alone cannot establish compatibility.

2. **Cross-controller lifecycle envtest**: Reconciler tests cover deploy, dynamic ownership, and GC composition, while tracker tests cover orchestration and inventory cleanup separately. A single envtest scenario spanning Platform, PlatformModule, module CR finalizers, and controller restart recovery remains a gap. PlatformModule uses inventory cleanup rather than generic GC.

3. **Platform mode (xKS) module E2E coverage**: The `test-kind-odh-e2e.yaml` workflow runs `make e2e-test-xks`, which selects KServe and Gateway scenarios and skips DSC management/validation. This does not establish full lifecycle coverage for every registered module. Expand module scenarios where vanilla Kubernetes behavior differs from OpenShift.

4. **Cross-controller recovery**: The PlatformModule integration test covers real watch delivery, finalizer barriers, and a manager restart. Coordinated recovery across Platform, DSC/DSCI, and external module operators still needs cross-controller envtest or cluster E2E validation.

5. **Partial local coverage**: Use the package tables to target untested local branches in `types`, DAG progression, GC, reconciler behavior, and module handlers. Do not add tests solely to cover framework re-exports.

---

## Acceptance Criteria Mapping

| Acceptance Criterion (RHOAIENG-61426) | Validation |
|----------------------------------------|------------|
| Library packages have unit tests with documented coverage | Level 1: coverage table above; CI via `test-unit.yaml` with Codecov upload |
| Module handlers have per-handler unit tests | Level 2: all 16 built-in handlers have tests and measured per-package coverage |
| Contract interfaces are validated across all implementations | Level 3: `manifests_compliance_test.go`, `handler_compliance_test.go`, registration checks, compile-time assertions |
| Module lifecycle is integration-tested end-to-end | Level 2: cluster E2E suites; fake-client tracker and PlatformModule tests validate each controller boundary, with cross-controller envtest listed as a gap |
| Test gaps are identified and tracked | Gaps section above: framework upgrades, cross-controller recovery, xKS coverage, and partial local coverage |
| CI runs tests automatically on relevant changes | `test-unit.yaml` selects `api/config/...` alongside controller/library tests and triggers on source, API, config, manifest pins, Makefile, and workflow changes |

# Module Framework QA Testing Strategy

**Date:** 2026-08-04

## Overview

This document defines the testing strategy for the module framework (`internal/controller/modules/`) and its underlying shared utilities library (`pkg/controller/`) in the opendatahub-operator. The module framework orchestrates Helm-based module lifecycle (provisioning, status aggregation, cleanup) on top of reconciliation primitives (actions, renderers, status, DAG ordering, conditions, predicates) provided by `pkg/controller/`.

The strategy is organized into three validation levels, progressing from isolated unit coverage of the shared utilities through consumer integration to module contract enforcement.

---

## Level 1: Library-Level Validation

Direct unit and integration tests for each package in `pkg/controller/`. These run in CI via the `test-unit.yaml` GitHub Actions workflow (`make unit-test`), which executes ginkgo with envtest, 8 parallel procs, and `--cover --coverprofile=cover.out`. Results are uploaded to Codecov.

### Coverage Summary

| Package | Test File(s) | Coverage |
|---------|-------------|----------|
| `actions/cacher` | `cacher_test.go` | 100% |
| `actions/deleteresource` | `action_delete_resources_test.go` | 80.8% |
| `actions/dynamicownership` | `action_dynamic_ownership_test.go` | 87.9% |
| `actions/render/helm` | `action_render_manifests_test.go` | 67.4% |
| `actions/render/kustomize` | `action_render_manifests_test.go` | 73.0% |
| `actions/render/template` | `action_render_templates_test.go` | 88.7% |
| `actions/resourcecacher` | `resourcecacher_test.go` | 93.3% |
| `actions/sanitycheck` | `sanitycheck_test.go` | 90.9% |
| `actions/status/deployments` | `action_deployments_available_test.go` | 79.6% |
| `actions/status/releases` | `action_fetch_releases_status_test.go` | 84.6% |
| `conditions` | `conditions_test.go`, `conditions_support_test.go` | 86.6% |
| `dag` | `dag_test.go` | 69.6% |
| `gates` | `gates_test.go` | 80.5% |
| `handlers` | _(none)_ | 0% |
| `predicates/resources` | `resources_test.go` | tested |
| `predicates/component` | _(none)_ | 0% |
| `predicates/dependent` | _(none)_ | 0% |
| `predicates/generation` | _(none)_ | 0% |
| `predicates/hash` | _(none)_ | 0% |
| `predicates/partial` | _(none)_ | 0% |

Additional tested packages outside the core action/predicate tree:

| Package | Test File(s) |
|---------|-------------|
| `cloudmanager` | `action_cleanup_test.go`, `action_gc_test.go`, `action_monitor_dependencies_test.go`, `action_reconcile_test.go`, `action_reconcile_integration_test.go`, `run_hooks_test.go` |
| `monitor` | `operator_test.go` |
| `precondition` | `custom_test.go`, `monitor_crd_test.go`, `monitor_operator_test.go`, `monitor_subscription_test.go`, `precondition_test.go`, `runlevel_gate_test.go` |
| `provision` | `gates_action_test.go`, `gating_test.go`, `runlevel_tracker_test.go`, `unified_test.go` |
| `reconciler` | `reconciler_test.go`, `reconciler_actions_test.go`, `reconciler_finalizer_test.go` |
| `types` | `types_test.go` |

### Gaps

- **`handlers`**: No tests. Single file (`handlers.go`), likely low complexity but untested.
- **`predicates/{component,dependent,generation,hash,partial}`**: Five sub-packages with zero test coverage. Only `predicates/resources` has tests.
- **`actions/deploy`**: Has tests but coverage is not in the provided metrics; several source files (`action_deploy_cache.go`, `action_deploy_managed.go`, `action_deploy_merge_deployment.go`, `action_deploy_merge_monitoring.go`, `action_deploy_metrics.go`, `action_deploy_remove_deployment_resources.go`, `action_deploy_support.go`, `action_deploy.go`) are covered by corresponding test files.

---

## Level 2: Consumer Integration Validation

The opendatahub-operator's own test suite validates `pkg/controller/` library usage at two tiers.

### Unit/Integration (envtest)

The operator's `make unit-test` runs against the full source tree including `pkg/controller/`. The module framework tests in `internal/controller/modules/` exercise the library's actions, renderers, and status primitives through concrete module handlers.

| Module Handler | Test File | Coverage |
|---------------|-----------|----------|
| Core framework | `base_test.go`, `base_delete_test.go`, `watch_test.go`, `readiness_test.go`, `registry_test.go`, `submodule_conditions_test.go`, `dsc_release_status_test.go`, `modules_controller_actions_test.go`, `modules_controller_actions_inject_env_test.go`, `modules_controller_actions_platform_config_test.go`, `modules_controller_actions_status_test.go`, `modules_controller_watch_test.go` | 71.9% |
| `aigateway` | `aigateway/handler_test.go` | 73.7% |
| `dashboard` | `dashboard/handler_test.go` | 97.1% |
| `feastoperator` | `feastoperator/handler_test.go` | 82.5% |
| `kserve` | `kserve/handler_test.go` | 81.5% |
| `mcplifecycleoperator` | `mcplifecycleoperator/handler_test.go` | 95.5% |
| `mlflowoperator` | `mlflowoperator/handler_test.go` | 79.4% |
| `monitoring` | `monitoring/handler_test.go` | 95.5% |
| `ogx` | `ogx/handler_test.go` | 95.8% |
| `workbenches` | `workbenches/handler_test.go` | 88.2% |

Note: The core framework coverage of 71.9% is partially due to `chart_compliance_test.go` failing when Helm charts have not been fetched (`make get-manifests` required).

### End-to-End (cluster-based)

E2E tests in `tests/e2e/` exercise the full module lifecycle (provision, update, removal) on a live cluster. These run via `make e2e-test` on OpenShift or `make e2e-test-xks` on KinD/AKS. Key module E2E test files:

- `tests/e2e/feastoperator_module_test.go`
- `tests/e2e/aigateway_test.go`
- `tests/e2e/dashboard_test.go`
- `tests/e2e/kserve_test.go`
- `tests/e2e/mcplifecycleoperator_test.go`
- `tests/e2e/mlflowoperator_test.go`
- `tests/e2e/monitoring_test.go`
- `tests/e2e/ogx_test.go`
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
| `ModuleHandler` | `internal/controller/modules/types.go` | Module lifecycle operations (render, deploy, status, cleanup) |
| `ComponentHandler` | `internal/controller/components/registry/registry.go` | Component lifecycle for in-tree components |
| `PlatformObject` | `api/common/types.go` | Common interface for DSC-managed platform objects |
| `dag.Node` | `pkg/controller/dag/dag.go` | DAG vertex with dependency declarations |
| `dag.ReadinessChecker` | `pkg/controller/dag/dag.go` | Readiness probing for DAG gating |

### Compliance Tests

- **`chart_compliance_test.go`** (`internal/controller/modules/`): Validates that Helm chart output for each module handler conforms to allowed Kubernetes resource kinds. Ensures charts do not introduce unsupported resource types. Requires charts to be fetched (`make get-manifests`).

- **`handler_compliance_test.go`** (`internal/controller/modules/`, NEW): Behavioral compliance test that exercises all registered `ModuleHandler` implementations against a common set of structural assertions: non-empty unique names, valid GVKs, manifest source presence, CR/GVK consistency, nil-safety for `IsEnabled` and `WriteDSCComponentStatus`, and optional interface conformance (`ReadyConditionTyper`, `ContainerNamer`, `DeploymentNamer`, `SubmoduleConditionProvider`). Validates that new handlers satisfy the same structural contract as existing ones.

- **`lifecycle_integration_test.go`** (`internal/controller/modules/`, NEW): Integration test that exercises the full module provision/cleanup state machine -- from initial creation through Managed state, status convergence, transition to Removed state, and resource cleanup -- in a single envtest session.

### Compile-Time Assertions

Handler implementations use compile-time interface satisfaction checks (e.g., `var _ ModuleHandler = (*Handler)(nil)`) to guarantee that new handlers implement all required methods. This is enforced at build time and requires no runtime test execution.

---

## Test Environments

| Environment | Scope | Infrastructure | CI Workflow |
|-------------|-------|---------------|-------------|
| Unit / envtest | `pkg/controller/`, `internal/controller/`, `api/` | GitHub Actions runner, envtest (kubebuilder assets) | `test-unit.yaml` |
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

1. **Upgrade compatibility testing**: Deferred until the library is versioned and released as a standalone Go module. API breakage for in-tree consumers is caught at compile time, but external consumers would benefit from semver guarantees.

2. **Full pipeline envtest**: No single test currently exercises the complete render-deploy-GC pipeline in one envtest session. Individual actions are tested in isolation; a unified pipeline test would validate action composition.

3. **Platform mode (xKS) module E2E coverage**: The `test-kind-odh-e2e.yaml` workflow covers a subset of modules (kserve, cloudmanager). Expanding xKS E2E coverage to all modules would validate library behavior on vanilla Kubernetes without OpenShift dependencies.

4. **Predicates sub-packages**: Five predicate packages (`component`, `dependent`, `generation`, `hash`, `partial`) have zero test coverage. These implement controller watch filters and should be unit tested.

5. **Handlers package**: `pkg/controller/handlers/handlers.go` has no test coverage.

---

## Acceptance Criteria Mapping

| Acceptance Criterion (RHOAIENG-61426) | Validation |
|----------------------------------------|------------|
| Library packages have unit tests with documented coverage | Level 1: coverage table above; CI via `test-unit.yaml` with Codecov upload |
| Module handlers have per-handler unit tests | Level 2: handler test files with coverage per handler (73.7% -- 97.1%) |
| Contract interfaces are validated across all implementations | Level 3: `chart_compliance_test.go`, `handler_compliance_test.go` (NEW), compile-time assertions |
| Module lifecycle is integration-tested end-to-end | Level 2 E2E: `tests/e2e/*_test.go` on OpenShift; Level 3: `lifecycle_integration_test.go` (NEW) |
| Test gaps are identified and tracked | Gaps section above: predicates, handlers, upgrade compat, pipeline envtest, xKS coverage |
| CI runs tests automatically on relevant changes | `test-unit.yaml` triggers on `internal/**`, `pkg/**`, `cmd/main.go`, `api/**`, `config/**` changes |

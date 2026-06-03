# MCP Lifecycle Operator - Remaining Work

This file tracks what is still needed before this PR is complete.

## Reconciler actions (OCPMCP-318)

The controller currently has no reconciliation actions. It needs:

- [ ] `initialize()` action to register manifest paths (see other components for reference)
- [ ] Manifest rendering action (kustomize-based, from upstream `config/` directory)
- [ ] Manifest deployment action
- [ ] Garbage collection action (must be last in the builder chain)
- [ ] Wire up `params.env` / image parameter map if needed

Reference: `internal/controller/components/ogx/` or `internal/controller/components/sparkoperator/`

## opendatahub-io fork (RHOAIENG-65514)

`get_all_manifests.sh` only pulls from `opendatahub-io` or `red-hat-data-services` orgs.
A fork at `opendatahub-io/mcp-lifecycle-operator` needs to be created (requires org admin).

- [ ] Create `opendatahub-io/mcp-lifecycle-operator` fork
- [ ] Add entry to `get_all_manifests.sh` pointing to the fork

## Tests (OCPMCP-319)

- [ ] Unit tests for component-specific functions
- [ ] e2e test suite in `tests/e2e/`
- [ ] Update `CreateDSC()` in `tests/e2e/helper_test.go`
- [ ] Update `componentsTestSuites` in `tests/e2e/controller_test.go`

## Prometheus / monitoring (OCPMCP-320)

- [ ] Add prometheus rules template at `internal/controller/components/mcplifecycleoperator/monitoring/`
- [ ] Add prometheus unit tests in `tests/prometheus_unit_tests/`

## Final wiring (OCPMCP-321)

- [ ] Update `get_all_manifests.sh` with branch pointer (depends on opendatahub-io fork)

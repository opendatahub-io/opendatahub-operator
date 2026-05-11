# opendatahub-operator

Kubernetes operator deploying AI/ML platform on OpenShift. Manages 16 components via DataScienceCluster (DSC) and DSCInitialization (DSCI) CRs with dedicated per-component controllers.

## Required Reading

Read before starting work:
- `CONTRIBUTING.md` — PR workflow, quality gates, code style
- `docs/DESIGN.md` — Architecture, CRDs, reconciliation design
- `docs/COMPONENT_INTEGRATION.md` — New component integration guide

## Build & Test

```bash
make build                         # Build binary
make unit-test                     # Unit tests
make e2e-test                      # E2E tests (requires cluster)
make lint                          # golangci-lint
make run-nowebhook                 # Run locally (dev)
make new-component COMPONENT=name  # Scaffold new component
```

## Quality Gates (MANDATORY)

Run after every code change. Fix all failures before finishing:
```bash
make generate manifests api-docs   # Codegen
make fmt                           # Format
make lint                          # Lint
```
Include any generated diff in your changes.

## Conventions

- Wrap errors: `fmt.Errorf("context: %w", err)`. Use `k8serr.IsNotFound()` etc.
- Use `go-multierror` for collecting multiple errors.
- Commits: conventional format `type(scope): description`.
- Platform builds: `-tags=odh` or `-tags=rhoai`. `ODH_PLATFORM_TYPE=OpenDataHub|rhoai`.
- OpenShift resources (Routes, OAuth, ConsoleLinks) may not exist on vanilla K8s — always handle with three-way branch (found / IsNotFound|IsNoMatchError / other error).

## Critical Rules

1. GC action MUST be last in action chain
2. Management states: `Managed` (deployed), `Removed` (cleaned up), empty = Removed

## Before Writing Code

Read existing files in same area. File locations follow pattern `<component>` = dashboard, kserve, ray, etc:
- API types: `api/components/v1alpha1/<component>_types.go`
- Controller: `internal/controller/components/<component>/<component>_controller.go`
- Actions: `internal/controller/components/<component>/<component>_controller_actions.go`
- Tests: `internal/controller/components/<component>/*_test.go`, `tests/e2e/`
- Reconciler builder: `pkg/controller/reconciler/`
- Component handler interface: `internal/controller/components/registry/registry.go`

## Documentation Index

Read as needed: `docs/api-overview.md`, `docs/troubleshooting.md`, `docs/integration-testing.md`, `docs/release-workflow-guide.md`, `docs/OLMDeployment.md`, `docs/upgrade-testing.md`. Run `make help` for all commands.

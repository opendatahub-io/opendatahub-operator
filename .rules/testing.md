---
paths: ["**/*_test.go", "tests/**/*.go"]
---

# Testing Patterns

Unit tests: use `fakeclient.New()` from `pkg/utils/test/fakeclient` for tests that only need a fake k8s client. Use `envt.New()` from `pkg/utils/test/envt` when testing against unstructured CRDs or when the test needs a real API server (CRD registration, status subresource updates).

E2E tests: use `TestContext` (`tc.Client()`, `tc.Context()`).

Prefer table-driven tests when multiple cases share the same setup/assertion structure. Use separate test functions only when setup differs significantly (e.g. fakeclient vs envtest).

E2E test oracles MUST be structurally independent from production code. Never call same production function or read same API resource as code-under-test — derive expectations from independent signals.

Follow patterns in:

- Unit (fakeclient): `pkg/controller/cloudmanager/action_monitor_dependencies_test.go`
- Unit (envtest): `pkg/controller/actions/dependency/action_operator_test.go`
- Unit (dashboard): `internal/controller/components/dashboard/dashboard_controller_actions_test.go`
- E2E: `tests/e2e/`

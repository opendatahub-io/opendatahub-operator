---
paths: ["**/*_test.go", "tests/**/*.go"]
---

# Testing Patterns

Unit tests: use `fake.NewClientBuilder()` with explicit scheme registration via `pkg/utils/test/scheme`.
E2E tests: use `TestContext` (`tc.Client()`, `tc.Context()`).

E2E test oracles MUST be structurally independent from production code. Never call same production function or read same API resource as code-under-test — derive expectations from independent signals.

Follow patterns in:
- Unit: `internal/controller/components/dashboard/dashboard_controller_actions_test.go`
- E2E: `tests/e2e/`

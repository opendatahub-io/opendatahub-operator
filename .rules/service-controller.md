---
paths: ["internal/controller/services/**/*.go"]
---

# Service Controller Patterns

Use reconciler builder pattern (same as components). Services implement `ServiceHandler` interface from `internal/controller/services/registry/registry.go`.

Action execution order matters: sequential, stops on first error. GC action MUST be last.

RBAC: service controllers use codegen (no `kubebuilder_rbac.go`) — except `gateway` which has hand-maintained RBAC markers.

File locations for service `<svc>` (auth, monitoring, setup, etc.):
- Handler: `internal/controller/services/<svc>/<svc>.go`
- Controller: `internal/controller/services/<svc>/<svc>_controller.go`
- Actions: `internal/controller/services/<svc>/<svc>_controller_actions.go`
- Tests: `internal/controller/services/<svc>/*_test.go`

Follow patterns in `internal/controller/services/auth/auth_controller.go`.

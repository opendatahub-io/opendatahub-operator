---
paths: ["**/cloudmanager/**/*.go"]
---

# Cloud Manager Controller Patterns

Use reconciler builder pattern with `WithDynamicOwnership()`. Each cloud provider has its own controller under `internal/controller/cloudmanager/<provider>/`.

Action execution order matters: sequential, stops on first error. GC action MUST be last.

RBAC: cloudmanager controllers have hand-maintained `kubebuilder_rbac.go` per provider + `common/kubebuilder_rbac.go`. After RBAC changes run `make manifests`.

Key differences from component/service controllers:
- Config passed via `*operatorconfig.CloudManagerConfig`

File locations for provider `<provider>` (azure, coreweave):
- Controller: `internal/controller/cloudmanager/<provider>/*_controller.go`
- Actions: `internal/controller/cloudmanager/<provider>/*_actions.go`
- RBAC: `internal/controller/cloudmanager/<provider>/kubebuilder_rbac.go`
- Shared: `internal/controller/cloudmanager/common/`

Follow patterns in `internal/controller/cloudmanager/azure/azurekubernetesengine_controller.go`.

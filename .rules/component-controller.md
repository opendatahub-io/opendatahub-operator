---
paths: ["internal/controller/components/**/*.go"]
---

# Component Controller Patterns

Use reconciler builder pattern:
```go
reconciler.ReconcilerFor(mgr, &componentApi.Xxx{}).
    Owns(&corev1.ConfigMap{}).
    WithAction(renderAction).
    WithAction(deployAction).
    WithAction(gcAction).  // MUST be last
    Build(ctx)
```

Action signature: `func(ctx context.Context, rr *types.ReconciliationRequest) error`

Component handler interface in `internal/controller/components/registry/registry.go`.

RBAC: component controllers use codegen. Do NOT add `kubebuilder_rbac.go` here — only top-level controllers (`dscinitialization`, `datasciencecluster`, `gateway`, `cloudmanager/*`) have hand-maintained RBAC markers.

Action execution order matters: sequential, stops on first error. Place actions deliberately — earlier actions set up state for later ones.

Use `StopError` to halt reconciliation without failure. Propagate errors via `WithError()` to update status conditions.

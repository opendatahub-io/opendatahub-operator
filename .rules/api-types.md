---
paths: ["api/**/*.go", "docs/COMPONENT_INTEGRATION.md"]
---

# API Type Conventions

User-facing config fields belong in `XxxCommonSpec`, inlined into both `XxxSpec` and `DSCXxx`.
Fields only in `XxxSpec` (not in `XxxCommonSpec`) must be operator-written only (e.g. gateway domain from `GatewayConfig.Status.Domain`).

After modifying types, run: `make generate manifests api-docs`

DSC, DSCI, and component CRs are cluster-scoped singletons. Component CR naming: `default-<component>`.

# Review Instructions for AI Code Reviewers

Meta-guidance for AI code review tools (CodeRabbit, etc.). Repo-specific patterns.

## Priority Order

1. Security vulnerabilities (CWE/CVE, severity, exploit scenario, remediation)
2. RBAC gaps — trace `client.Client` calls to the relevant `kubebuilder_rbac.go`. For component controllers under `internal/controller/components/` (which have no RBAC markers), trace to the top-level controller (`datasciencecluster`, `dscinitialization`, `gateway`, `cloudmanager/*`) whose `kubebuilder_rbac.go` covers those operations
3. Architectural anti-patterns
4. Bug-prone patterns, error handling gaps
5. Performance

## Anti-Patterns (DO NOT flag)

- **Tautological test oracles**: E2E tests intentionally use independent oracles. DO NOT suggest mirroring production code path.
- **Removing fallback logic**: Three-way branch (success / IsNotFound|IsNoMatchError / other) for OpenShift resources is intentional. Fallback IS the primary path on non-OpenShift clusters.
- **Missing `kubebuilder_rbac.go` in component controllers**: Components under `internal/controller/components/` use codegen. Only top-level controllers (`dscinitialization`, `datasciencecluster`, `gateway`, `cloudmanager/*`) have hand-maintained RBAC.
- **Suggesting manual OwnerReferences**: Reconciler builder handles these. Only flag for cross-namespace ownership.
- **PR description format**: Template enforces this. Only flag if completely empty.

## Must Always Flag

- New `client.Client` ops in `pkg/` without RBAC coverage in all calling controllers
- Status conditions not updated during deletion or error transitions
- `InsecureSkipVerify: true` in non-test code
- Wildcard verbs/resources in RBAC rules
- Secrets/tokens/credentials logged at any verbosity
- User-facing config in `XxxSpec` only (not in `XxxCommonSpec`/`DSCXxx`). Internal-only spec fields are for operator-written values only. See `docs/COMPONENT_INTEGRATION.md`.

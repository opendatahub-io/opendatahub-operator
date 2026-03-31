# Review Instructions for AI Code Reviewers

These instructions are meta-guidance for AI code review tools (CodeRabbit, etc.).
They define review priorities and anti-patterns specific to this repository.

## Review Priority Order

When multiple findings exist, prioritize in this order:

1. Security vulnerabilities (provide CWE/CVE IDs, severity, exploit scenario, and remediation code)
2. RBAC permission gaps — trace `client.Client` calls to the calling controller's `kubebuilder_rbac.go`
3. Architectural issues and anti-patterns
4. Bug-prone patterns and error handling gaps
5. Performance problems

## Anti-Patterns to Avoid

DO NOT suggest the following — these are common false positives in this codebase:

- **Tautological test oracles**: DO NOT suggest that e2e tests call the same production helper or API resource they are validating. Independent oracles are intentional. See `AGENTS.md` "Test Oracle Independence" section.
- **Removing fallback logic**: DO NOT flag OpenShift-to-vanilla-K8s fallback paths as dead code or suggest simplifying the three-way error branch (`success / IsNotFound|IsNoMatchError / other error`). The fallback is the primary path on non-OpenShift clusters.
- **Missing `kubebuilder_rbac.go` in component controllers**: Component controllers under `internal/controller/components/` use codegen for RBAC. Only top-level controllers (`dscinitialization`, `datasciencecluster`, `gateway`, `cloudmanager/*`) have hand-maintained `kubebuilder_rbac.go` files.
- **Suggesting `OwnerReferences` be set manually**: The reconciler builder pattern handles OwnerReferences. DO NOT suggest adding them in action code unless there is a cross-namespace ownership scenario.
- **PR description format comments**: The PR template already enforces description requirements. DO NOT comment on PR description format unless it is completely empty.

## Patterns That Must Always Be Flagged

Always flag these regardless of context:

- Any `+kubebuilder:rbac` change without a corresponding `make manifests` regeneration
- New `client.Client` operations in `pkg/` without RBAC coverage in all calling controllers
- Status conditions that don't update during deletion or error transitions
- `InsecureSkipVerify: true` in non-test code
- Wildcard verbs or resources in RBAC rules
- Secrets, tokens, or credentials logged at any verbosity level

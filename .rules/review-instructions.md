# Review Instructions for AI Code Reviewers

These instructions are meta-guidance for AI code review tools (CodeRabbit, etc.).
They define reviewer behavior controls specific to this repository.
For detailed architectural rules, see `AGENTS.md` "Architecture Enforcement Rules" section.

## Review Noise Reduction

- Restrict feedback to errors, security risks, and functionality-breaking problems.
- DO NOT post comments on code style, formatting, or non-critical improvements.
- Limit review comments to 5 items maximum per file, unless additional blockers exist.
- Group similar issues into a single comment instead of posting multiple notes.
- If a pattern repeats across files, mention it once at summary level only.
- DO NOT suggest refactoring unless it fixes a bug or security issue.
- If there are no critical problems, respond with minimal approval. DO NOT add filler commentary.
- Avoid line-by-line commentary unless it highlights a critical bug, security hole, or RBAC gap.

## Review Priority Order

When multiple findings exist, prioritize in this order:

1. Security vulnerabilities (provide CWE ID and severity; include CVE ID if referencing a known vulnerability; describe exploit scenario and remediation code)
2. RBAC permission gaps — see AGENTS.md "RBAC and Controller Tracing" for the tracing procedure
3. Architectural issues and anti-patterns
4. Bug-prone patterns and error handling gaps
5. Performance problems

## Anti-Patterns to Avoid

DO NOT suggest the following — these are common false positives in this codebase:

- **Tautological test oracles**: DO NOT suggest that e2e tests call the same production helper or API resource they are validating. See AGENTS.md "Test Oracle Independence" for rationale.
- **Removing fallback logic**: DO NOT flag OpenShift-to-vanilla-K8s fallback paths as dead code or suggest simplifying the three-way error branch. See AGENTS.md "Multi-Platform Fallback Patterns" for rationale.
- **Missing `kubebuilder_rbac.go` in component controllers**: Component controllers use codegen for RBAC — see AGENTS.md "RBAC and Controller Tracing" for which controllers have hand-maintained files.
- **Suggesting `OwnerReferences` be set manually**: The reconciler builder pattern handles OwnerReferences. DO NOT suggest adding them in action code unless there is a cross-namespace ownership scenario.
- **PR description format comments**: The PR template already enforces description requirements. DO NOT comment on PR description format unless it is completely empty.

## Patterns That Must Always Be Flagged

Always flag these regardless of context:

- New or modified `+kubebuilder:rbac` markers inconsistent with `client.Client` operations in the PR (note: `config/rbac/role.yaml` is gitignored; regeneration is validated by CI)
- New `client.Client` operations in `pkg/` without RBAC coverage in all calling controllers (see AGENTS.md "RBAC and Controller Tracing")
- Status conditions that don't update during deletion or error transitions (see AGENTS.md "Status Conditions and Lifecycle Transitions")
- `InsecureSkipVerify: true` in non-test code
- Newly introduced wildcard verbs or resources in RBAC rules
- Secrets, tokens, or credentials logged at any verbosity level

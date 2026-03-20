# ADR-0001: Per-Component Controller Architecture

## Status

Accepted (December 2024)

## Context

The original ODH operator used a single controller/reconcile loop for all components. This led to:
- Tight coupling between unrelated component reconciliation logic
- A single failure could block reconciliation for all components
- Poor scalability as new components were added
- Difficulty reasoning about individual component lifecycle

## Decision

Refactor the operator to use dedicated controllers for each component, with each component represented by its own internal Custom Resource. The two user-facing CRs (DSCInitialization and DataScienceCluster) remain as the entry points, but component reconciliation is delegated to per-component controllers.

## Consequences

- Each component has its own controller, CR, and reconciliation loop
- Failures in one component do not block others
- New components can be integrated by implementing a standard controller interface
- Component state is tracked via individual status conditions
- The operator can reconcile components in parallel
- Trade-off: more CRDs and controllers to maintain, but better separation of concerns

## References

- [DESIGN.md](../DESIGN.md) for full architectural details

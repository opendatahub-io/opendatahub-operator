Summary

The ODH operator currently monitors external operator dependencies (e.g., cert-manager) via two mechanisms, both with known limitations:

OLM Subscription watches (checkSubscriptionDependencies / dependency.CheckSubscriptionGroup): Used on OpenShift to detect whether operators like NIM are installed (see https://redhat.atlassian.net/browse/RHOAIENG-62464 ). This approach will break with OLM v1, which removes the Subscription API entirely, and is unavailable on xKS clusters where OLM is not present.

CRD existence checks (MonitorCRDs / cluster.HasCRD()): Used by CCM to check for cert-manager. This is insufficient: CRDs persist after an operator is uninstalled, causing false-positive DependenciesAvailable: True and downstream failures like cert-manager webhook connection refused (see https://redhat.atlassian.net/browse/RHOAIENG-62676 ).

This spike should investigate and recommend a future-proof monitoring strategy that works across all three scenarios: current OLM (v0), OLM v1, and non-OLM (xKS) clusters.

Areas to investigate:

Operational readiness vs. CRD existence: How to verify that an operator is actually running and functional, not just that its CRDs are registered.

OLM v1 compatibility: What replaces Subscription watches in OLM v1 (e.g., ClusterExtension API) and how to detect operator presence through it.

Webhook readiness probing: Whether checking ValidatingWebhookConfiguration or MutatingWebhookConfiguration endpoint readiness is a reliable cross-platform signal.

Deployment/Pod health checks: Whether watching operator Deployment status (available replicas, conditions) is feasible across namespaces.

Unified vs. platform-specific approach: Whether a single monitoring strategy can work across all platforms, or if platform-specific branches are needed (and how to minimize divergence).

Reconciliation triggers: What Kubernetes resources to watch (Deployments, Webhooks, CRDs, ClusterExtensions) to reactively trigger reconciliation when dependency state changes.

Failure modes: How to handle transient unavailability (operator restarting) vs. permanent absence (operator uninstalled).

Acceptance Criteria

Document the recommended monitoring approach that works for current OLM, OLM v1, and non-OLM (xKS) clusters

Identify which Kubernetes resources provide reliable "operator is operational" signals in each scenario

Propose whether the solution should be platform-specific or unified, with justification

Assess impact on existing checkSubscriptionDependencies pattern and migration path

Identify risks and limitations of each approach considered

Provide a rough implementation sketch (watches, conditions, controller changes)

Reference

RHOAIENG-62676 — Bug: CRD-only check gives false-positive DependenciesAvailable for cert-manager

RHOAIENG-62464 — Story: OLM Subscription watch pattern for NIM operator (breaks with OLM v1)

RHOAIENG-62288 — Related: webhook connection refused during bootstrap

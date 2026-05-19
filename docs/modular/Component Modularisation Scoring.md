# Component Modularisation Scoring

This document analyses each in-tree component in the ODH Operator, assesses its
migration complexity to the modular architecture, and recommends an order for
modularisation from easiest to most complex.

## Scoring criteria

Each component is scored across five dimensions:

| Dimension | Low (1) | Medium (2) | High (3) |
|---|---|---|---|
| **Business logic** | Standard pipeline only (`initialize` / `releases` / `kustomize` / `deploy` / `deployments` / `gc`) | 1-2 custom actions (params injection, config projection) | 3+ custom actions, programmatic resource creation, config surgery |
| **Platform integration** | No gateway, auth, or monitoring coupling | Reads gateway domain or auth config for field projection | Deep gateway/OIDC/Istio/Kuadrant/Perses integration |
| **Webhooks** | None in `internal/webhook/` | Owns webhook configs deployed via manifests | Platform-managed mutating/validating webhooks that must transfer |
| **Upgrade / migration** | No entries in `pkg/upgrade/` | Minor cleanup (deprecated resource deletion) | Multi-resource migration (HWP, annotations, CRD conversions) |
| **Inter-module deps** | Self-contained | Reads platform service config (gateway, auth) via PlatformContext | Reads other ODH component states or imports component packages |

Total score range: 5 (trivial) to 15 (very complex).

**Important:** Checking for external operators or CRDs (e.g., JobSet, Istio,
cert-manager, InferenceServices) is a standard pattern that any module operator
handles trivially in its reconcile loop. External operator dependencies do
**not** contribute to migration complexity. The complexity lies in what the
platform operator does *for* the component -- business logic, programmatic
resource creation, webhook management, and upgrade hooks.

---

## Component analysis

### Tier 1 -- Deploy-only components (score 5-6)

These components follow the standard action pipeline with no custom business
logic, no webhooks, no upgrade hooks, and no inter-module dependencies. They
are pure "deploy the operator manifests and check the deployment is healthy"
controllers.

#### Training Operator -- Score: 5

| Dimension | Score | Detail |
|---|---|---|
| Business logic | 1 | `initialize` → `releases` → `kustomize` → `deploy` → `deployments` → `gc`. No custom actions. |
| Platform integration | 1 | None. Reads no gateway/auth config. |
| Webhooks | 1 | None. |
| Upgrade / migration | 1 | No entries in `pkg/upgrade/`. |
| Inter-module deps | 1 | Self-contained. |

**Migration effort:** Minimal. A module handler sets a manifest path and a GVK.
No `BuildModuleCR` field projection beyond platform defaults. 6 files.

---

#### LlamaStack Operator -- Score: 5

| Dimension | Score | Detail |
|---|---|---|
| Business logic | 1 | Standard pipeline only. No custom actions. |
| Platform integration | 1 | None. |
| Webhooks | 1 | None. |
| Upgrade / migration | 1 | No entries in `pkg/upgrade/`. |
| Inter-module deps | 1 | Self-contained. |

**Migration effort:** Minimal. Identical pattern to Training Operator. Owns
`PodDisruptionBudget` as a manifest artifact. 6 files.

---

#### Spark Operator -- Score: 6

| Dimension | Score | Detail |
|---|---|---|
| Business logic | 1 | Standard pipeline only. No custom actions. |
| Platform integration | 1 | None. |
| Webhooks | 1.5 | **Owns** `MutatingWebhookConfiguration` and `ValidatingWebhookConfiguration` deployed as manifest resources. These are the Spark operator's own in-cluster admission webhooks, not platform-managed. |
| Upgrade / migration | 1 | No entries in `pkg/upgrade/`. |
| Inter-module deps | 1 | Self-contained. |

**Migration effort:** Minimal. The webhook configs are operand-level manifest
resources that move naturally with the module. Owns `PodMonitor`. 6 files.

---

#### Ray -- Score: 6

| Dimension | Score | Detail |
|---|---|---|
| Business logic | 1.5 | `initialize` injects application namespace into params. `sanitycheck.NewAction` blocks if legacy CodeFlare CRs exist -- a migration-era guard likely removable before modularisation. |
| Platform integration | 1 | Only reads application namespace (available via `PlatformContext`). |
| Webhooks | 1 | None. |
| Upgrade / migration | 1 | No entries in `pkg/upgrade/`. |
| Inter-module deps | 1 | The CodeFlare check is an anti-dependency against a removed component, not an active inter-module concern. |

**Migration effort:** Low. The CodeFlare sanity check is a one-off guard. Once
CodeFlare is fully removed, Ray becomes a pure Tier 1 deploy-only component.
Owns `SecurityContextConstraints`. 6 files.

---

### Tier 2 -- Light business logic (score 7)

These components have a small amount of custom business logic (params
injection, CRD ownership semantics) but remain self-contained with no
platform coupling.

#### Data Science Pipelines -- Score: 7

| Dimension | Score | Detail |
|---|---|---|
| Business logic | 2 | `checkPreConditions` validates Argo Workflows CRD ownership (ODH-managed vs external). `argoWorkflowsControllersOptions` serialises DSC spec fields into `params.env`. Both are small, self-contained functions. |
| Platform integration | 1 | None. |
| Webhooks | 1 | None. |
| Upgrade / migration | 1 | No entries in `pkg/upgrade/`. |
| Inter-module deps | 1 | Reads Argo Workflows CRD labels -- external CRD ownership, not an inter-module dependency. |

**Migration effort:** Low. The Argo CRD ownership check and params injection
transfer cleanly to the module operator. Owns `SecurityContextConstraints`.
9 files.

---

#### Trainer -- Score: 7

| Dimension | Score | Detail |
|---|---|---|
| Business logic | 2 | `checkPreConditions` validates JobSet operator presence. `dependency.NewAction` monitors JobSet operator conditions. Both are standard module-side patterns -- checking for external operators is trivial in a module reconcile loop. |
| Platform integration | 1 | None. |
| Webhooks | 1 | None (Kueue validator references `trainer.kubeflow.org` but is disabled). |
| Upgrade / migration | 1 | No entries in `pkg/upgrade/`. |
| Inter-module deps | 1 | JobSet is an external operator, not an ODH component. `ClusterTrainingRuntime` is the module's own CRD. |

**Migration effort:** Low. The JobSet dependency check and condition monitoring
are standard patterns any module operator implements. Dynamic
`ClusterTrainingRuntime` ownership is the module's own CRD -- natural for a
module operator. 8 files.

---

#### TrustyAI -- Score: 7

| Dimension | Score | Detail |
|---|---|---|
| Business logic | 2 | `checkPreConditions` gates on KServe InferenceServices CRD presence (trivial for a module). `createConfigMap` builds a ConfigMap imperatively from DSCI config. Optional MCP Guardrails overlay path selection. |
| Platform integration | 1 | Reads DSCI config for ConfigMap contents -- available via `PlatformContext`. |
| Webhooks | 1 | None. |
| Upgrade / migration | 1 | No entries in `pkg/upgrade/`. |
| Inter-module deps | 1 | The KServe CRD check is a standard external CRD gate, not a read of KServe's management state. |

**Migration effort:** Low-moderate. The KServe CRD precondition is trivial for
a module. The imperative ConfigMap creation is the main piece -- small but must
be replicated. The MCP Guardrails overlay adds a minor config concern. 6 files.

---

### Tier 3 -- Platform field projection (score 8-9)

Components with meaningful platform coupling (gateway domain, auth, OIDC)
or inter-module awareness, but no webhooks or heavy upgrade logic.

#### Feast Operator -- Score: 8

| Dimension | Score | Detail |
|---|---|---|
| Business logic | 2 | `setKustomizedParams` merges OIDC config into `params.env`. |
| Platform integration | 2 | `NewCRObject` reads `GatewayConfig` and cluster auth mode to populate `spec.oidc` on the Feast CR. |
| Webhooks | 1 | None. |
| Upgrade / migration | 1 | No entries in `pkg/upgrade/`. |
| Inter-module deps | 2 | Depends on gateway/auth configuration (platform services via `PlatformContext`). |

**Migration effort:** Moderate. The OIDC projection in `NewCRObject` maps to
`BuildModuleCR` + `PlatformContext`. The gateway domain and auth mode must be
correctly projected into the module CR spec. 6 files.

---

#### Model Controller -- Score: 8

| Dimension | Score | Detail |
|---|---|---|
| Business logic | 2 | `initialize` conditionally includes WVA manifests based on KServe spec. Params encode NIM, KServe, and ModelRegistry management states. |
| Platform integration | 1 | None beyond reading DSC spec. |
| Webhooks | 1 | Owns `ValidatingWebhookConfiguration` as a manifest resource. |
| Upgrade / migration | 2 | `cleanupModelControllerLegacyDeployment` in `pkg/upgrade/` removes pre-2.17 deployment (one-time cleanup). |
| Inter-module deps | 2 | Reads KServe and ModelRegistry management states from the DSC to decide manifest composition. The subscription dependency check is a trivial external operator pattern. |

**Migration effort:** Moderate. The inter-module awareness (KServe/ModelRegistry
enablement) means `BuildModuleCR` must project these states. The legacy
deployment cleanup is one-time. 7 files.

---

### Tier 4 -- Webhook transfer + platform coupling (score 9-10)

Components requiring webhook ownership transfer or significant platform
integration beyond simple field projection.

#### Workbenches -- Score: 9

| Dimension | Score | Detail |
|---|---|---|
| Business logic | 2 | `configureDependencies` creates a namespace. `setKustomizedParams` injects params. `imagestreams.NewAction()` handles ImageStream status. `updateStatus` copies namespace to status. |
| Platform integration | 1 | Reads application namespace for the workbench namespace default. |
| Webhooks | 3 | Platform-managed notebook mutating webhook (`internal/webhook/notebook/`) and HardwareProfile mutating webhook (`internal/webhook/hardwareprofile/`) target `kubeflow.org/v1 Notebooks`. Both must transfer to the module. |
| Upgrade / migration | 2 | Notebook HardwareProfile annotation migration in `pkg/upgrade/`. |
| Inter-module deps | 1 | Watches MLflowOperator CR (cross-component trigger) but does not read its state. |

**Migration effort:** Moderate-high. The webhooks are the main concern -- the
module team must take ownership of notebook connection mutation and HWP
injection. The namespace creation, ImageStream handling, and upgrade hooks are
smaller concerns. 9 files.

---

#### Model Registry -- Score: 10

| Dimension | Score | Detail |
|---|---|---|
| Business logic | 2 | `customizeManifests` sets overlay paths. `configureDependencies` creates a namespace. `template.NewAction()` adds an extra template render stage. `updateStatus` copies namespace to status. |
| Platform integration | 2 | Requires `spec.gateway.domain` populated from GatewayConfig. `computeKustomizeVariable` derives URLs from gateway domain. |
| Webhooks | 2 | DSC defaulting webhook (`internal/webhook/datasciencecluster/`) imports `modelregistry` package for `DefaultModelRegistriesNamespace`. This import must be removed during migration. |
| Upgrade / migration | 1 | No dedicated entries in `pkg/upgrade/`. |
| Inter-module deps | 3 | Watches DSCInitialization. Build-tag split (`_support.odh.go` / `_support.rhoai.go`) for platform-specific defaults -- dual platform configuration. |

**Migration effort:** High. The gateway domain coupling, template render stage,
build-tag platform split, and webhook import into DSC defaulting all need
careful handling. 10 files.

---

### Tier 5 -- Heavy integration (score 12-15)

Components with deep platform coupling, extensive business logic, webhooks,
programmatic resource creation, and/or significant upgrade/migration surface.

#### Dashboard -- Score: 13

| Dimension | Score | Detail |
|---|---|---|
| Business logic | 3 | `deployObservabilityManifests` (Perses side-deploy to monitoring namespace), `setKustomizedParams`, `configureDependencies` (Anaconda secret on RHOAI), `reconcileHardwareProfiles` (AP → infra HWP migration). Custom `updateStatus` (dashboard URL from Routes). |
| Platform integration | 2 | Gateway domain for CR construction. OpenShift Route/ConsoleLink. Perses integration for COO dashboards. |
| Webhooks | 3 | Deprecation validators for `dashboard.opendatahub.io` AcceleratorProfile and HardwareProfile in `internal/webhook/dashboard/`. |
| Upgrade / migration | 3 | Heavy `pkg/upgrade/` involvement: `MigrateAcceleratorProfilesToHardwareProfiles`, `MigrateContainerSizesToHardwareProfiles`, OdhDashboardConfig reading, feature visibility annotations. |
| Inter-module deps | 2 | Owns `OdhDashboardConfig` (unremovable GC resource). Watches Perses CRDs, HTTPRoutes. |

**Migration effort:** Very high. The HWP migration logic in `pkg/upgrade/`
and `reconcileHardwareProfiles` spans multiple API types. The deprecation
webhooks must transfer. The Perses side-deploy to the monitoring namespace is
cross-namespace. The Anaconda secret is RHOAI-specific. 9 files.

---

#### KServe -- Score: 13

| Dimension | Score | Detail |
|---|---|---|
| Business logic | 3 | `removeOwnershipFromUnmanagedResources`, `cleanUpTemplatedResources`, `customizeKserveConfigMap` (config surgery + hash annotation for deployment rollout), `versionedWellKnownLLMInferenceServiceConfigs`. Dual overlays for OpenShift (Serverless/ServiceMesh) vs XKS. |
| Platform integration | 3 | Dual-platform manifest selection. ConfigMap mutation with deployment rollout hashing. |
| Webhooks | 3 | ISVC and LLMISVC mutating webhooks (`internal/webhook/serving/`). HardwareProfile mutating webhook targets KServe GVKs. |
| Upgrade / migration | 3 | ISVC HWP annotation migration, `custom-serving` HWP creation, serverless deployment mode annotations, Kueue namespace skip logic. |
| Inter-module deps | 1 | External operator dependencies (Istio, cert-manager, LWS) are trivial for a module. No reads of other ODH component states. |

**Migration effort:** Very high. The real complexity is the business logic:
dual-platform overlays, ConfigMap surgery, ownership cleanup, and
platform-managed webhooks. The external operator dependency checks are trivial
and not a factor. 10 files.

---

#### Kueue -- Score: 13

| Dimension | Score | Detail |
|---|---|---|
| Business logic | 3 | `manageDefaultKueueResourcesAction` programmatically creates ResourceFlavor, ClusterQueue, and LocalQueue from live cluster capacity. `manageKueueAdminRoleBinding` reads Auth CR to build ClusterRoleBinding. `configureClusterQueueViewerRoleAction` patches ClusterRole labels. |
| Platform integration | 3 | Reads Auth service CR for admin groups. Creates scheduling topology from cluster state. Namespace label mutation across the cluster. |
| Webhooks | 2 | Validating webhook implementation exists in `internal/webhook/kueue/` but is currently disabled. |
| Upgrade / migration | 2 | `cleanupDeprecatedKueueVAPB` removes deprecated ValidatingAdmissionPolicyBinding. Cross-cutting: Kueue namespace detection affects notebook/ISVC HWP migration. |
| Inter-module deps | 3 | Auth service integration. Kueue-managed namespace semantics affect Workbenches and KServe upgrade paths. |

**Migration effort:** Very high. The programmatic resource creation from
cluster state is the hardest piece -- this business logic must move to the
module operator. The Auth integration and namespace labeling are platform-level
concerns that need clear API boundaries. 11 files.

---

#### Models as a Service (MaaS) -- Score: 14

| Dimension | Score | Detail |
|---|---|---|
| Business logic | 3 | 16 actions in the pipeline: `checkDependencies`, `validatePrerequisites`, `validateGateway`, `customizeManifests`, `configureGatewayNamespaceResources`, `configureExternalOIDC`, `configureTelemetryPolicy`, `configureIstioTelemetry`, `validatePersesResources`, `configurePersesResources`, `configureConfigHashAnnotation`. Programmatically builds AuthPolicy, Telemetry, and TelemetryPolicy resources. |
| Platform integration | 3 | Deep Gateway API, Kuadrant AuthPolicy, Istio Telemetry, Perses dashboards, external OIDC, UWM (User Workload Monitoring). |
| Webhooks | 1 | None. |
| Upgrade / migration | 1 | No entries in `pkg/upgrade/` (component is newer). |
| Inter-module deps | 3 | Requires functioning gateway (Gateway API Gateway), Kuadrant stack, Authorino, Istio, COO/Perses. Reads OpenShift Authentication CR for OIDC issuer. |

**Migration effort:** Highest. Almost every action depends on a different
platform service. The module operator would need to handle gateway validation,
OIDC configuration, Istio/Kuadrant integration, and observability setup. This
is essentially a rewrite of the reconciliation logic within the module.
11 files.

---

## Recommended modularisation order

| Priority | Component | Score | Rationale |
|---|---|---|---|
| **1** | Training Operator | 5 | Pure deploy-only. Zero custom logic. Ideal first migration to validate the pipeline. |
| **2** | LlamaStack Operator | 5 | Identical pattern. Second validation of the migration path. |
| **3** | Spark Operator | 6 | Deploy-only with operand-level webhooks in manifests. |
| **4** | Ray | 6 | CodeFlare anti-dependency is a migration-era guard, likely removable. |
| **5** | Data Science Pipelines | 7 | Light CRD ownership check and params injection. First test of moving business logic to a module. |
| **6** | Trainer | 7 | External operator dependency (JobSet) is trivial in a module. Dynamic CRD ownership is natural for a module operator. |
| **7** | TrustyAI | 7 | KServe CRD gate is trivial. Imperative ConfigMap creation is the only real business logic. |
| **8** | Feast Operator | 8 | First component with gateway/OIDC coupling. Tests `PlatformContext` → `BuildModuleCR` projection. |
| **9** | Model Controller | 8 | Inter-module awareness (KServe/ModelRegistry states). Tests spec field projection for cross-component awareness. |
| **10** | Workbenches | 9 | Platform-managed webhooks (notebook mutation, HWP injection). First migration requiring webhook ownership transfer. |
| **11** | Model Registry | 10 | Gateway domain prerequisite, template render, build-tag platform split, DSC webhook import. |
| **12** | Dashboard | 13 | HWP migration logic, deprecation webhooks, Perses side-deploy, heavy upgrade hooks. |
| **13** | KServe | 13 | Dual-platform overlays, ConfigMap surgery, webhooks, heavy upgrade hooks. External deps are trivial. |
| **14** | Kueue | 13 | Programmatic scheduling resource creation from cluster state, Auth integration, namespace labeling. |
| **15** | Models as a Service | 14 | Longest action chain, broadest platform integration, deep gateway/OIDC/Istio/Kuadrant/Perses coupling. |

## Migration phases

Based on the analysis, we recommend grouping the migration into four phases:

### Phase 1 -- Validation (components 1-4)

Migrate Training Operator, LlamaStack Operator, Spark Operator, and Ray.
These are deploy-only components that validate the entire modular pipeline end
to end: manifest gathering, module handler registration, module CR creation,
module operator deployment, status aggregation, and GC.

**Success criteria:** All four modules deploy and reconcile correctly. The DSC
status reflects module health. Disabling a module via the DSC correctly tears
down the module operator.

### Phase 2 -- Light business logic + platform fields (components 5-9)

Migrate Data Science Pipelines, Trainer, TrustyAI, Feast, and Model Controller.
These have small amounts of business logic (params injection, imperative
resource creation) and test `PlatformContext` field projection (gateway domain,
OIDC config, inter-module awareness).

**Success criteria:** Module operators handle their own business logic
(precondition checks, params, ConfigMap creation). `BuildModuleCR` correctly
projects platform fields and cross-component management states.

### Phase 3 -- Webhook ownership transfer (components 10-11)

Migrate Workbenches and Model Registry. These are the first components
requiring webhook ownership transfer. The notebook mutation webhook and HWP
injection webhook must be re-implemented by the module team.

**Success criteria:** Module operators own their webhooks. DSC defaulting
webhook no longer imports modelregistry. Platform HWP CRD and defaults still
deployed by the platform.

### Phase 4 -- Heavy integration (components 12-15)

Migrate Dashboard, KServe, Kueue, and Models as a Service. These require the
most significant effort due to deep platform coupling, heavy upgrade logic,
programmatic resource creation, and broad RBAC footprints.

**Order within phase:**
1. Dashboard -- HWP migration logic and deprecation webhooks are bounded, even
   if complex.
2. KServe -- dual-platform overlays, ConfigMap surgery, webhooks, upgrade hooks.
3. Kueue -- programmatic scheduling topology and Auth integration.
4. MaaS -- broadest platform integration, essentially a rewrite.

**Success criteria:** All business logic lives in module operators. The platform
operator's only role is deploying module operators, creating module CRs, and
aggregating status. The `pkg/upgrade/` hooks for these components are either
removed or moved to the module operators.

---

## Notes

- **Services** (Auth, Gateway, Monitoring, CertConfigMapGenerator) are platform
  infrastructure and are **not** candidates for modularisation. They remain
  in-tree as part of the platform orchestrator.

- Scores are relative and based on the current codebase. Components may become
  simpler over time (e.g., if CodeFlare is fully removed, Ray drops to Tier 1;
  if the Kueue webhook is permanently disabled, it is one less concern).

- The `pkg/upgrade/` migration functions that reference multiple components
  (e.g., HWP migration spans Dashboard, Workbenches, and KServe) should be
  addressed as part of the last component in that group to migrate.

- Each phase should include comprehensive E2E testing before proceeding to the
  next phase. See the
  [Module Handler Developer Guide](Module%20Handler%20Developer%20Guide.md)
  testing section for guidance.

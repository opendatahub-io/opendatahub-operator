# Cloud Controller Manager Chart Dependency Onboarding

Use this checklist when a new xKS dependency will be installed and reconciled
by Cloud Controller Manager (CCM) through a KubernetesEngine custom resource.
The CCM implementation and chart packaging are in this repository. The source
Helm charts currently registered with CCM live in
[odh-gitops](https://github.com/opendatahub-io/odh-gitops/tree/main/charts/dependencies).

Complete the relevant steps in this repository and `odh-gitops`. Choose the
policy, resource scope, readiness criteria, and ownership behavior for each
dependency before applying the implementation steps.

This is an engineering checklist, not an installation guide. For the xKS Helm
installation chart, see the
[rhai-on-xks-chart README](https://github.com/opendatahub-io/odh-gitops/blob/main/charts/rhai-on-xks-chart/README.md).

## Ownership and data flow


| Concern                               | Repository and location                                                                                           | What changes there                                                                                                                            |
| ------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| Dependency's rendered resources       | `odh-gitops/charts/dependencies/<chart>/`                                                                         | Chart templates, values, version, CRDs, RBAC, and chart tests.                                                                                |
| CCM image's copy of the chart         | `opendatahub-operator/manifests-config.yaml`, `opt/charts/`, and Dockerfiles                                      | Pin each platform's chart source and package it at `/opt/charts/<chart>`.                                                                     |
| KubernetesEngine API and CCM behavior | `opendatahub-operator/api/cloudmanager/`, `internal/controller/cloudmanager/`, and `pkg/controller/cloudmanager/` | Policy, configuration, rendering, cleanup, health, watches, RBAC, and generated CRDs.                                                         |
| xKS Helm installation interface       | `odh-gitops/charts/rhai-on-xks-chart/`                                                                            | Pass dependency settings into each provider's KubernetesEngine CR; update generated CCM resources, defaults, docs, and snapshots when needed. |


The xKS installation chart deploys the platform and creates a provider-specific
KubernetesEngine CR. CCM reads that CR, renders charts packaged in its image,
and applies their resources. Only charts registered under `ccmCharts` are
fetched for CCM. Other charts in `charts/dependencies/` can be consumed directly
by the xKS Helm installation chart.

The existing [odh-gitops contributing guide](https://github.com/opendatahub-io/odh-gitops/blob/main/CONTRIBUTING.md#adding-a-new-dependency-to-the-helm-chart)
describes adding an OpenShift/OLM Helm-chart dependency. That is a separate
path from adding an xKS CCM dependency.

The integration varies with the chart: it may render no operator CR, a
cluster-scoped CR, or a namespace-scoped CR. The policy default, configuration,
health checks, and removal path must match the chosen resource model.

## 1. Define the dependency contract

- [ ] Identify the chart owner, chart name, release name, supported chart and
  operand versions, and any prerequisites or transitive dependencies.
- [ ] Decide whether the dependency is required for every engine or is an
  optional capability.
- [ ] Decide the default `managementPolicy` deliberately. `Managed` means CCM
  installs and reconciles the dependency. `Unmanaged` requests that CCM stop
  managing it; the administrator must supply it after cleanup completes. See
  the existing-CR caveat in step 7. Do not copy another dependency's default.
- [ ] Identify every configurable value CCM must pass to the chart, especially
  operator and operand namespaces.
- [ ] Identify the operator or operand CR, including its exact GVK, name, and
  scope. A namespace-scoped CR must use the configured namespace on every
  lookup and cleanup path.
- [ ] Define what constitutes readiness: deployments, an operator or operand CR
  condition, or both.
- [ ] If the chart creates the CR used for health monitoring, require that CR:
  its absence must report the dependency unhealthy.
- [ ] Define removal behavior, including whether the operand CR must be
  removed before CCM cleans up chart-owned resources.

## 2. Prepare the source chart

In `odh-gitops`:

- [ ] Add or update `charts/dependencies/<chart>/` with its `Chart.yaml`,
  `values.yaml`, templates, and any applicable CRDs, RBAC, or update script.
- [ ] Render the chart with default and CCM-supplied values. Check stable
  resource names, namespace-scoped and cluster-scoped CR identity, pull-secret
  needs, and compatibility with non-OpenShift Kubernetes.
- [ ] Add dependency-chart snapshots to `scripts/snapshot-config.yaml`, run
  `helm lint charts/dependencies/<chart>`, and run
  `make chart-test CHART_NAME=dependencies/<chart>`. Regenerate intentional
  snapshot changes with `make chart-snapshots CHART_NAME=dependencies/<chart>`.
- [ ] Update `charts/README.md` when adding a source chart. Run
  `make helm-docs` after changing chart values and include the generated
  `charts/dependencies/<chart>/api-docs.md`.
- [ ] Make the chart available at a revision that the operator can pin for
  every platform that will ship it. The upstream and downstream repositories
  and revisions may differ.

## 3. Package the chart for supported platforms in this repository

- [ ] Add the chart to `ccmCharts` in `manifests-config.yaml`.
- [ ] Provide a repository, revision-pinned ref, and `sourcePath` for each
  supported platform. The downloader skips an entry without a source for the
  selected platform.
- [ ] For each configured platform, run `make get-manifests` (ODH default) or
  `make get-manifests ODH_PLATFORM_TYPE=rhoai` and verify the fetched chart in
  `opt/charts/<chart-name>/`. The Dockerfiles copy fetched charts to
  `/opt/charts/<chart-name>/` in the CCM image.
- [ ] Render the chart with its defaults and with every CCM-supplied value.
  Confirm that namespace, CRD, and RBAC resources match the contract from step
  1.

`ccmCharts` is intentionally separate from `components` and `componentCharts`:
it supplies the Cloud Controller Manager image rather than a DSC component.

## 4. Expose configuration through the xKS chart in `odh-gitops`

When users configure the new dependency through `rhai-on-xks-chart`:

- [ ] Add `kubernetesEngine.spec.dependencies.<name>` values with the agreed default policy and
  configuration. The xKS chart's CR-creation hooks pass this provider spec
  to the KubernetesEngine CR.
- [ ] Update `charts/rhai-on-xks-chart/values.schema.json` for the new
  dependency and confirm its documented policy default agrees with the xKS
  values and KubernetesEngine API. Do not assume a shared schema default
  expresses the correct policy for every dependency.
- [ ] Update namespace and pull-secret helpers, hook permissions, or cleanup
  jobs when the dependency introduces namespaces or resources they handle.
  In particular, `templates/_helpers.tpl` currently reads
  `configuration.namespace`; dependencies with other namespace fields need
  explicit handling wherever those namespaces matter.
- [ ] Update the xKS chart's generated CCM CRDs, RBAC, and templates from a
  matching operator revision using
  `charts/rhai-on-xks-chart/scripts/update-bundle.sh`. Review the generated
  diff rather than editing generated templates by hand.
- [ ] Update the xKS chart README, run `make helm-docs` for generated API
  docs, and test managed, unmanaged, and supported custom-namespace values with
  `make chart-test CHART_NAME=rhai-on-xks-chart`. Regenerate intentional
  snapshot changes with `make chart-snapshots CHART_NAME=rhai-on-xks-chart`.

## 5. Extend the CCM API

- [ ] Add a dependency type and its configuration type in
  `api/cloudmanager/common/types.go`.
- [ ] Add the dependency to `Dependencies` with validation, generated-object
  markers, defaults, and documentation for every user-visible field.
- [ ] Add helpers that resolve empty configuration values to defaults. Keep
  namespace fields immutable when changing them would orphan existing
  resources.
- [ ] Add the required readiness condition in `internal/controller/status`.
- [ ] Update the sample CR under `config/cloudmanager/<provider>/samples/` for
  each supported provider. Keep its policy and configuration aligned with the
  API default.
- [ ] Regenerate API code and documentation with `make generate api-docs`.

## 6. Register chart lifecycle and reconciliation behavior

`internal/controller/cloudmanager/common/charts.go` is the single source of
truth for CCM chart lifecycle. Add one `chartDef` to `allChartDefs` that
includes:

- [ ] A `stateFn` driven by the new management policy.
- [ ] The Helm chart path, stable release name, and all chart values derived
  from the effective API configuration.
- [ ] Required pre- or post-apply hooks, only when they are specific to the
  chart's behavior.
- [ ] The operator or operand CR identity when one exists.
- [ ] The new readiness condition and deployment namespace. Set `RequireCR`
  when the chart creates the monitored operator or operand CR.

Do not bypass `BuildHelmCharts`. It derives the render list, the first cleanup
phase, the final cleanup list, and the monitoring configuration from the same
definition.

## 7. Preserve the two-phase removal contract

For a dependency with an operator or operand CR, an `Unmanaged` transition is
two phases:

1. If the CR still exists, keep rendering the chart but exclude that CR from
  CCM deployment. Garbage collection can then remove the CR before the
   operator.
2. Once the CR is absent, stop rendering the chart and delete only the
  chart-rendered resources that are owned by the CCM instance.

- [ ] Verify both phases for the new dependency in unit tests.
- [ ] Do not delete unowned resources or the unremovable resources (including
  CRDs and namespaces). CCM's garbage-collection action remains last in its
  action chain.

A chart without an operator or operand CR goes directly to the excluded state
when set to `Unmanaged`.

**Existing-CR caveat:** `makeStateFn` checks only whether a CR with the
configured GVK, name, and namespace exists. It does not check ownership before
entering phase 1. A preexisting administrator-managed CR with that identity can
therefore cause CCM to render and apply the chart's other resources even when
policy is `Unmanaged`. Before claiming safe coexistence with a user-managed
installation, add ownership-aware handling and a regression test, or document
that limitation for the dependency.

## 8. Monitor health and watch state changes

- [ ] Add deployment monitoring when the chart installs an operator deployment.
- [ ] Add operator/operand CR health monitoring when the chart creates one.
  The default degraded filter treats `Degraded=True`, `Available=False`, and
  `Ready=False` as unhealthy.
- [ ] Ensure the dependency condition reports reason `Unmanaged` when CCM
  does not own the dependency. This does not assert the health of an
  administrator-managed installation.
- [ ] Register a resource-version predicate for the operator/operand GVK in
  `OperatorCRGVKPredicates()` when status-only changes must trigger
  reconciliation. The default generation predicate ignores those updates.
- [ ] Add a controller-level test showing that the watched CR's status update
  re-reconciles the engine.

## 9. Generate RBAC and manifests

- [ ] Run `make update-cloudmanager-rbac`. The generator reads every
  `ccmCharts` entry and captures chart-rendered Role and ClusterRole names in
  CCM's RBAC annotations. Inspect names that change with custom chart values;
  the generator renders defaults.
- [ ] Declare the API permissions CCM needs to read, watch, and manage the
  dependency's operator or operand CR in
  `internal/controller/cloudmanager/common/kubebuilder_rbac.go`.
- [ ] Run `make manifests-ccm` to regenerate the provider CRDs and RBAC.
- [ ] Run the required quality gates `make generate manifests api-docs`,
  `make fmt`, and `make lint` after code changes.
- [ ] Inspect generated diffs. The rendered chart must not require RBAC that
  CCM has not declared.

## 10. Run the updated CCM for cluster tests

- [ ] Follow [CCM Deployment](../README.md#ccm-deployment) to run the modified
  CCM locally or deploy an image built with the modified code and chart. For a
  local run, install the selected provider CRD and use the updated `opt/charts/`.
- [ ] Confirm that CCM is running with the new chart and provider CRD before
  CCM E2E tests. `make e2e-test-ccm` installs cert-manager if needed and runs
  the tests; it does not deploy CCM. Unit tests do not require this step.

## 11. Test the integration

- [ ] Add or update `internal/controller/cloudmanager/common/charts_test.go`
  for managed, unmanaged, cleaning, and excluded states.
- [ ] Add API default and custom-value coverage, including custom namespaces
  for namespace-scoped operator CRs.
- [ ] Add monitoring coverage in
  `pkg/controller/cloudmanager/action_monitor_dependencies_test.go` for
  healthy, degraded, and unmanaged dependency states as applicable.
- [ ] If the chart creates the monitored CR, test that `RequireCR` is set and
  that a missing CR marks the dependency readiness condition `False` even when
  other monitored resources are healthy.
- [ ] Add cleanup coverage in
  `pkg/controller/cloudmanager/action_cleanup_test.go` for both removal
  phases.
- [ ] Add dynamic-watch coverage for status-only changes when the dependency
  has an operator or operand CR.
- [ ] Add the new `ccmCharts` key to
  `tests/e2e/scripts/e2e-scope-rules.yaml` (or an alias or ignored-name entry).
  The manifest scope completeness test requires every chart key to be known.
- [ ] Add or update CCM E2E coverage when the dependency is part of the
  supported xKS deployment path.
- [ ] Run `make unit-test` and, when the dependency is exercised by a supported
  provider, `make e2e-test-ccm CLOUD_MANAGER_PROVIDER=<provider>` before
  merging.
- [ ] For an xKS Helm installation, extend the relevant assertions in
  `charts/rhai-on-xks-chart/scripts/verify.sh` and run
  `make helm-install-verify-xks` in `odh-gitops`. If an existing installation
  can be upgraded to the new dependency, extend its upgrade assertions and
  run `make helm-upgrade-verify-xks`. The current generic scripts do not by
  themselves verify a new dependency's health or ownership transition.

## Completion checklist

The onboarding is complete only when all of the following are true:

- [ ] The chart is reproducibly packaged for each platform that supports it.
- [ ] The CCM API, rendering, cleanup, status, and watch behavior agree on the
  dependency's policy and identity.
- [ ] Generated API, RBAC, and CCM manifest artifacts are included.
- [ ] The xKS installation chart creates a KubernetesEngine CR accepted by
  the matching operator image's CRD and passes the intended dependency values.
- [ ] Managed, Unmanaged, cleanup, readiness, and status-update behavior are
  covered by tests appropriate to the dependency.
- [ ] The pull request explains the default policy, the ownership boundary
  between CCM and the source chart, and the validation performed.

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

The opendatahub-operator is a Kubernetes operator built with controller-runtime that manages the deployment and lifecycle of Open Data Hub components. It uses a modular, component-based architecture where two main CRDs (DataScienceCluster and DSCInitialization) orchestrate multiple component and service controllers.

## Common Development Commands

### Build and Run

```bash
# Build operator binary
make build

# Run operator locally (with webhooks)
make run

# Run operator locally (without webhooks - useful for debugging)
make run-nowebhook

# Build operator image
make image-build IMG=quay.io/<username>/opendatahub-operator:<tag>

# Build for RHOAI mode instead of ODH
make image-build ODH_PLATFORM_TYPE=rhoai
```

### Code Generation

```bash
# Generate deepcopy methods, manifests, CRDs, RBAC, webhooks
make manifests generate

# Generate for both ODH and RHOAI
make manifests-all

# Update API documentation
make api-docs

# After changing API types, run:
make generate manifests api-docs
```

### Linting and Formatting

```bash
# Format code
make fmt

# Run linter
make lint

# Fix linting issues automatically
make lint-fix

# Run go vet
make vet
```

### Testing

```bash
# Run unit tests
make unit-test

# Run e2e tests (requires deployed operator)
make e2e-test

# Run e2e tests for specific component
make e2e-test -e E2E_TEST_COMPONENT=dashboard

# Run e2e tests for specific service
make e2e-test -e E2E_TEST_SERVICE=monitoring

# Run single e2e test
make e2e-test-single TEST="TestOdhOperator/Component_Tests/dashboard/Validate component enabled"

# Setup cluster for e2e testing (creates DSCI/DSC)
make e2e-setup-cluster

# Run Prometheus alert unit tests
make test-alerts
```

### Deployment

```bash
# Install CRDs
make install

# Deploy operator to cluster
make deploy IMG=quay.io/<username>/opendatahub-operator:<tag>

# Undeploy operator
make undeploy

# Deploy via OLM bundle
make bundle
make bundle-build BUNDLE_IMG=quay.io/<username>/opendatahub-operator-bundle:<version>
operator-sdk run bundle <bundle-image>
```

### Component Manifests

```bash
# Fetch component manifests from upstream repositories
make get-manifests

# Build image with local manifests (for testing manifest changes)
make image-build USE_LOCAL=true
```

### Adding a New Component

```bash
# Use component-codegen to scaffold a new component
make new-component COMPONENT=<component-name>

# This will generate:
# - api/components/v1alpha1/<component>_types.go
# - internal/controller/components/<component>/
# - Manifests and tests
```

## Architecture

### Two-Tier CRD Model

1. **DataScienceCluster (DSC)** - Singleton, cluster-scoped
   - Owns all component CRs (Dashboard, Workbenches, KServe, Ray, etc.)
   - User-facing configuration for enabling/disabling components
   - Aggregates component statuses

2. **DSCInitialization (DSCI)** - Singleton, cluster-scoped
   - Platform-level initialization (namespaces, monitoring, trusted CA)
   - Must be created before DSC
   - Watches cluster configuration

### Component & Service Architecture

**Component Controllers** (`internal/controller/components/`):
- Each component (Dashboard, Workbenches, KServe, etc.) has a dedicated controller
- Components are owned by DSC and reconciled when DSC changes
- Component handlers registered in `registry/registry.go`
- 16+ components supported (see `api/components/v1alpha1/`)

**Service Controllers** (`internal/controller/services/`):
- Platform services: Auth, Monitoring, Gateway
- Not owned by DSC but watched by it
- Reconciled independently

### Handler Registry Pattern

All components implement the `ComponentHandler` interface:
- `Init(platform)` - Initialize with platform-specific config
- `GetName()` - Component identifier
- `NewCRObject()` - Create component CR from DSC spec
- `NewComponentReconciler()` - Create dedicated reconciler
- `UpdateDSCStatus()` - Update parent DSC status
- `IsEnabled()` - Check if enabled in DSC

Handlers are registered in `cmd/main.go` and can be suppressed via flags.

### Generic Reconciler Pattern

The operator uses a generic reconciler (`pkg/controller/reconciler/`) with an action-based pipeline:
- Actions execute in sequence: Initialize → Validate → Deploy → GarbageCollect → UpdateStatus
- Automatic resource ownership tracking
- Finalizer-based cleanup
- Condition-based status reporting

Example action sequence for components:
1. Initialize - Setup reconciliation context
2. Deploy - Render and apply manifests from `opt/manifests/<component>/`
3. GarbageCollect - Remove resources when component disabled
4. UpdateStatus - Report conditions and phase

### Manifest Deployment

Component manifests are fetched from upstream repositories:
- `get_all_manifests.sh` downloads manifests to `opt/manifests/`
- `COMPONENT_MANIFESTS` map defines source repositories
- Manifests are embedded in operator image at build time
- `pkg/deploy/` handles manifest rendering and application
- Supports Kustomize overlays and templating

### Webhook System

Webhooks registered in `internal/webhook/webhook.go`:
- Validation/mutation for DSC, DSCI, components, services
- Component-specific webhooks (Dashboard, Notebook, KServe, Kueue)
- Webhooks suppressed if components are disabled
- Can be disabled entirely with `-tags nowebhook` build tag

### Platform Detection

The operator detects platform capabilities (`pkg/cluster/`):
- OpenShift vs vanilla Kubernetes
- Managed vs self-managed
- Available APIs (Route, OAuth, etc.)
- Component availability adjusted based on platform

## Key Directories

- `api/` - CRD type definitions (datasciencecluster, dscinitialization, components, services)
- `internal/controller/` - Reconciliation logic for DSC, DSCI, components, services
- `pkg/controller/` - Generic reconciler framework and utilities
- `pkg/deploy/` - Manifest deployment and resource management
- `pkg/cluster/` - Platform detection and configuration
- `pkg/upgrade/` - Version upgrade handling
- `internal/webhook/` - Webhook validation/mutation logic
- `opt/manifests/` - Component manifests (fetched from upstream)
- `config/` - Kustomize configurations for deployment (ODH mode)
- `config/rhoai/` - Kustomize configurations for RHOAI mode
- `tests/e2e/` - End-to-end tests
- `tests/integration/` - Integration tests with envtest

## Working with Components

### Enabling/Disabling Components

Components are enabled in DataScienceCluster spec:

```yaml
apiVersion: datasciencecluster.opendatahub.io/v2
kind: DataScienceCluster
metadata:
  name: default-dsc
spec:
  components:
    dashboard:
      managementState: Managed  # or Removed
    workbenches:
      managementState: Managed
```

### Component Lifecycle

1. User creates/updates DSC with component enabled
2. DSC controller creates component CR (e.g., Dashboard CR)
3. Component controller reconciles, deploying manifests from `opt/manifests/`
4. Component controller updates component status conditions
5. DSC controller aggregates component status into DSC status

### Testing Component Changes

When changing component code in `internal/controller/components/<component>/`:

```bash
# Run component-specific e2e test
make e2e-test -e E2E_TEST_COMPONENT=<component>
```

When changing component manifests:

```bash
# Test with local manifests
make image-build USE_LOCAL=true
make deploy IMG=<your-image>
```

### Component Dependencies

Some components have dependencies:
- **kueue** requires workbenches
- **modelcontroller** requires kserve, modelregistry
- **modelsasservice** requires kserve
- **trustyai** requires kserve

When testing, enable dependencies: `E2E_TEST_COMPONENT=kserve,trustyai`

## Working with Services

Services (Auth, Monitoring, Gateway) are reconciled independently:

```bash
# Test monitoring service
make e2e-test -e E2E_TEST_SERVICE=monitoring

# Disable component tests
make e2e-test -e E2E_TEST_COMPONENTS=false -e E2E_TEST_SERVICES=true
```

## Configuration

### Build-time Configuration

- `ODH_PLATFORM_TYPE` - Set to `OpenDataHub` (default) or `rhoai`
- `USE_LOCAL` - Use local manifests instead of fetching (`true`/`false`)
- `VERSION` - Operator version (default: 3.3.0)
- `IMG` - Operator image to build/deploy
- `PLATFORM` - Multi-arch build platforms (default: `linux/amd64`)

### Runtime Configuration

Environment variables (or flags):
- `OPERATOR_NAMESPACE` - Namespace where operator runs
- `DEFAULT_MANIFESTS_PATH` - Path to component manifests
- `ODH_MANAGER_LOG_MODE` - Logging mode (`prod`, `devel`, or empty)
- `ZAP_LOG_LEVEL` - Log level (`info`, `debug`, `error`)

### E2E Test Configuration

Key environment variables:
- `E2E_TEST_OPERATOR_NAMESPACE` - Operator namespace (default: `opendatahub-operator-system`)
- `E2E_TEST_APPLICATIONS_NAMESPACE` - Applications namespace (default: `opendatahub`)
- `E2E_TEST_COMPONENT` - Space-separated list of components to test
- `E2E_TEST_SERVICE` - Space-separated list of services to test
- `E2E_TEST_DELETION_POLICY` - When to delete resources (`always`, `on-failure`, `never`)

## Change-to-Test Mapping

| What Changed | Pre-Test Steps | E2E Command |
|--------------|----------------|-------------|
| `internal/controller/components/<comp>/` | None | `make e2e-test -e E2E_TEST_COMPONENT=<comp>` |
| `internal/controller/services/<svc>/` | None | `make e2e-test -e E2E_TEST_SERVICE=<svc>` |
| `api/components/v1alpha1/<comp>_types.go` | `make generate manifests api-docs` | `make e2e-test -e E2E_TEST_COMPONENT=<comp>` |
| `api/datasciencecluster/`, `api/dscinitialization/` | `make generate manifests api-docs` | `make e2e-test` (full) |
| `internal/controller/datasciencecluster/` or `dscinitialization/` | None | `make e2e-test` (full) |
| `pkg/`, `config/`, `cmd/main.go` | None | `make e2e-test` (full) |
| `opt/manifests/<comp>/` | None | `make e2e-test -e E2E_TEST_COMPONENT=<comp>` |

## Typical Development Workflow

1. Make code changes
2. Run `make generate manifests` if API types changed
3. Run `make fmt lint` to format and lint
4. Run `make unit-test` for fast feedback
5. Build image: `make image-build IMG=quay.io/<user>/opendatahub-operator:dev`
6. Push image: `make image-push IMG=quay.io/<user>/opendatahub-operator:dev`
7. Deploy: `make deploy IMG=quay.io/<user>/opendatahub-operator:dev`
8. Run relevant e2e tests
9. Check operator logs: `kubectl logs -n opendatahub-operator-system deployment/opendatahub-operator-controller-manager`

## Pre-Commit Requirements

**IMPORTANT**: Before committing any changes, the following checks MUST pass:

1. **Linting**: `make lint` must complete with no errors
2. **E2E Tests**: Relevant end-to-end tests must pass (see Change-to-Test Mapping above)

These checks ensure code quality and prevent regressions. Do not commit changes that fail either check.

## Status and Conditions

Components and services report status using Kubernetes conditions:
- Condition types: `Available`, `Degraded`, `Progressing`, `ReconcileComplete`
- Check DSC status: `kubectl get dsc default-dsc -o yaml`
- Check component status: `kubectl get dashboard default-dashboard -o yaml`

Conditions are managed in `pkg/controller/conditions/` and `pkg/controller/status/`.

## Important Files

Entry points:
- `cmd/main.go` - Operator initialization and manager setup
- `internal/controller/datasciencecluster/datasciencecluster_controller.go` - DSC reconciliation
- `internal/controller/dscinitialization/dscinitialization_controller.go` - DSCI reconciliation

Core infrastructure:
- `pkg/controller/reconciler/reconciler.go` - Generic reconciler engine
- `pkg/deploy/deploy.go` - Manifest deployment logic
- `internal/controller/components/registry/registry.go` - Component registry

## Debugging

### Running Locally

```bash
# Export kubeconfig
export KUBECONFIG=/path/to/kubeconfig

# Run without webhooks (easier for debugging)
make run-nowebhook

# Enable debug logging
export ZAP_LOG_LEVEL=debug
make run-nowebhook
```

### Common Issues

- **CRDs not found**: Run `make install` to install CRDs
- **Webhook errors**: Use `make run-nowebhook` or ensure cert-manager is installed
- **Manifest errors**: Check `opt/manifests/` exists, run `make get-manifests` if needed
- **Component not reconciling**: Check DSC status conditions, operator logs
- **E2E test failures**: Check `E2E_TEST_DELETION_POLICY=never` to preserve resources for debugging

## Additional Resources

- README.md - Installation and usage instructions
- docs/api-overview.md - API reference documentation
- docs/COMPONENT_INTEGRATION.md - Component integration guide
- docs/troubleshooting.md - Troubleshooting guide
- docs/OLMDeployment.md - OLM deployment details

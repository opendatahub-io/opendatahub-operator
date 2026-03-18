# opendatahub-operator Development Guidelines

## What is opendatahub-operator?

The Open Data Hub operator is a Kubernetes operator that deploys and manages a complete data science and AI/ML platform on OpenShift. It orchestrates 16 specialized components (Jupyter notebooks, KServe model serving, Ray distributed computing, ML pipelines, TrustyAI, ModelRegistry, etc.) through two primary Custom Resources: **DataScienceCluster (DSC)** for enabling/configuring components and **DSCInitialization (DSCI)** for platform-level setup. The operator uses a modern architecture with dedicated controllers per component, action-based reconciliation, and dynamic resource ownership.

## CRITICAL: Required Reading for All Agents

**Before starting ANY work on this project, agents MUST read the following documents in their entirety:**

### Core Documentation
- @README.md - Project overview, installation, prerequisites, developer quick start
- @CONTRIBUTING.md - PR workflow, code review, testing requirements, quality gates
- @docs/DESIGN.md - Architecture, CRDs (DSC/DSCI), reconciliation refactor, component controllers
- @docs/COMPONENT_INTEGRATION.md - Step-by-step guide for integrating new components

### Build and Development
- @Makefile - All build, test, deploy, and development commands (run `make help`)

### Specialized Documentation
- @docs/troubleshooting.md - Debugging, common issues, environment setup
- @docs/api-overview.md - Generated API reference for all CRDs
- @docs/cloudmanager-api-overview.md - CloudManager infrastructure APIs
- @docs/OLMDeployment.md - Operator Lifecycle Manager installation
- @docs/integration-testing.md - Integration test architecture and execution
- @docs/release-workflow-guide.md - Release process, branching strategy
- @docs/ACCELERATOR_METRICS.md - GPU/accelerator metrics via OpenTelemetry
- @docs/NAMESPACE_RESTRICTED_METRICS.md - Metrics access control and namespace isolation
- @docs/AUTOMATED_MANIFEST_UPDATES.md - Manifest synchronization and automated updates
- @docs/e2e-update-requirement-guidelines.md - E2E test requirements for new features
- @docs/upgrade-testing.md - Upgrade path testing procedures

## Repository Structure
```
api/                          # CRD definitions
├── components/v1alpha1/      # Component APIs (Dashboard, KServe, Ray, etc.)
├── datasciencecluster/       # DSC API (main user-facing CR)
├── dscinitialization/        # DSCI API (platform initialization)
├── services/                 # Auth, Monitoring, GatewayConfig APIs
├── infrastructure/           # HardwareProfile API
└── common/                   # Shared types & interfaces       

internal/controller/          # Reconciliation logic
├── datasciencecluster/       # DSC controller
├── dscinitialization/        # DSCI controller
├── components/               # Component controllers (16+ components)
│   ├── dashboard/
│   ├── kserve/
│   ├── ray/
│   ├── datasciencepipelines/
│   └── ...                   # (workbenches, trustyai, modelregistry, etc.)
├── services/                 # Service controllers (auth, monitoring, gateway, etc.)
├── cloudmanager/             # CloudManager orchestration
└── status/                   # Status aggregation

pkg/                          # Shared libraries
├── controller/
│   ├── actions/              # Reusable reconciler actions
│   ├── reconciler/           # Generic reconciler builder pattern
│   ├── conditions/           # Condition management
│   ├── handlers/             # Event handlers
│   └── predicates/           # Event filtering predicates
├── cluster/                  # Cluster detection and configuration
├── deploy/                   # Deployment logic
├── manifests/                # Manifest rendering (Kustomize/Helm/Template)
├── resources/                # Resource utilities
├── metadata/                 # Labels and annotations
└── upgrade/                  # Upgrade logic

config/                       # Kubernetes manifests (Kustomize)
├── crd/                      # CRD kustomization and patches
├── rbac/                     # RBAC resources
├── default/                  # Default ODH installation manifests
├── rhoai/                    # RHOAI platform-specific configs
├── manager/                  # Operator deployment manifests
├── monitoring/               # Prometheus and monitoring configs
└── samples/                  # Example CR instances

tests/                        # Test suites
├── e2e/                      # End-to-end tests
└── prometheus_unit_tests/    # Prometheus alert validation

cmd/                          # Entry points
├── main.go                   # Operator entry point
├── component-codegen/        # CLI tool for scaffolding new components
├── cloudmanager/             # CloudManager entry point
├── health-check/             # Cluster health check utility
└── test-retry/               # Test retry utility

opt/                          # External resources (gitignored)
├── manifests/                # Component manifests (fetched via make get-manifests)
└── charts/                   # Helm charts

hack/                         # Developer helper scripts
├── buildLocal.sh             # Build and deploy to local cluster
└── component-dev/            # Custom manifest development tools

Dockerfiles/                  # Container image build configurations
├── Dockerfile                # Main operator image
└── rhoai.Dockerfile          # RHOAI-specific image

docs/                         # Comprehensive documentation
```

## Essential Commands

Run `make help` for the complete list. Most commonly used:

```bash
# Build & Run
make build                        # Build operator binary
make run                          # Run operator locally with webhooks
make run-nowebhook                # Run operator locally without webhooks (easier for dev)
make image-build                  # Build container image
make image-push                   # Push container image to registry

# Code Generation & Formatting
make generate                     # Generate DeepCopy methods
make manifests                    # Generate CRDs, RBAC, webhooks
make api-docs                     # Generate API documentation
make fmt                          # Format code and imports
make get-manifests                # Fetch component manifests from remote repos

# Testing & Quality
make unit-test                    # Run unit tests
make e2e-test                     # Run E2E tests
make lint                         # Run golangci-lint
make vet                          # Run go vet

# Deployment
make install                      # Install CRDs to cluster
make uninstall                    # Uninstall CRDs from cluster
make deploy                       # Deploy operator to cluster
make undeploy                     # Remove operator from cluster

# Component Development
make new-component COMPONENT=name # Generate new component scaffold

# Bundle & Catalog (OLM)
make bundle                       # Generate OLM bundle manifests
make bundle-push                  # Push bundle image
```

## Developer Workflow Essentials

### Local Development with local.mk
Create `local.mk` in the repository root to override default Makefile variables:

Example `local.mk`:
```makefile
# Override default Makefile variables for local development
VERSION=4.4.4
IMAGE_TAG_BASE=quay.io/your-registry/opendatahub-operator
IMG_TAG=dev
OPERATOR_NAMESPACE=my-dev-namespace
APPLICATIONS_NAMESPACE=my-apps-namespace
```

### Deploy to Cluster Workflow
```bash
# 1. Build and push your image
make image-build image-push IMG=quay.io/yourname/opendatahub-operator:dev

# 2. Install CRDs
make install

# 3. Deploy operator with your image
make deploy IMG=quay.io/yourname/opendatahub-operator:dev

# 4. Check operator logs
oc logs -n opendatahub-operator-system deployment/opendatahub-operator-controller-manager -f
```

### Manifest Management
- Component manifests are stored in `opt/manifests/` 
- Fetch latest manifests: `make get-manifests`
- Use local manifests for development: `make image-build USE_LOCAL=true`
- Platform-specific builds: `ODH_PLATFORM_TYPE=OpenDataHub` (default) or `ODH_PLATFORM_TYPE=rhoai`

## Key Architecture Patterns

### Reconciler Builder Pattern
Located in `pkg/controller/reconciler/`. Use fluent API to compose reconcilers:
```go
reconciler.ReconcilerFor(mgr, &componentApi.Dashboard{}).
    Owns(&corev1.ConfigMap{}).
    WithAction(renderAction).
    WithAction(deployAction).
    WithAction(gcAction).  
    Build(ctx)
```

### Action-Based Reconciliation
Located in `pkg/controller/actions/`. Actions have signature:
```go
func(ctx context.Context, rr *types.ReconciliationRequest) error
```

### Component Handler Interface
Each component implements methods defined in `internal/controller/components/registry/registry.go`:
```go
Init(platform common.Platform) error
GetName() string
NewCRObject(ctx context.Context, cli client.Client, dsc *dscv2.DataScienceCluster) (common.PlatformObject, error)
NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error
UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error)
IsEnabled(dsc *dscv2.DataScienceCluster) bool
```

### Error Handling Conventions
- **Wrap errors with context**: `fmt.Errorf("failed to deploy component: %w", err)`
- **Use StopError**: To gracefully halt reconciliation without marking as failure
- **Translate to conditions**: Errors are converted to condition states via `WithError()` option
- **Check Kubernetes errors**: Use `k8serr.IsNotFound(err)`, `k8serr.IsAlreadyExists(err)`, etc.
- **Multierror support**: Use `github.com/hashicorp/go-multierror` for collecting multiple errors

## Critical Rules

1. **Garbage collection action MUST be last** in the action chain
2. **Singleton CRs**: DSC, DSCI, and all component CRs are cluster-scoped singletons
3. **Component naming**: Must match pattern `default-<component>` (enforced by XValidation)
4. **Management states**: `Managed` (deployed), `Removed` (cleaned up), empty/`{}` (treated as Removed)
5. **Platform detection**: Use build tags `-tags=odh` or `-tags=rhoai`
6. **Action execution order matters**: Sequential execution, stops on first error

## Platform-Specific Considerations

- **ODH_PLATFORM_TYPE**: Set to `OpenDataHub` (default) or `rhoai`
- **Namespaces**: ODH uses `opendatahub`, RHOAI uses `redhat-ods-*`
- **OpenShift resources** (Routes, OAuth, ConsoleLinks) may not be available on vanilla K8s
- **Local overrides**: Create `local.mk` to override Makefile variables

## File Locations for Common Tasks

- Add API field: `api/components/v1alpha1/<component>_types.go`
- Implement controller: `internal/controller/components/<component>/<component>_controller.go`
- Add actions: `internal/controller/components/<component>/<component>_controller_actions.go`
- Add E2E test: `tests/e2e/<component>_test.go`

These documents contain critical requirements that MUST be followed.
Failure to read and follow these guidelines will result in code that does not meet project standards.
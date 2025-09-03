# Component Integration

Since the ODH operator is the integration point to deploy ODH component manifests, it is essential to have common processes to integrate new components.

Currently, each component is expected to have its own dedicated internal API/CRD and dedicated reconciler.
To understand the current operator architecture and its inner workings, please refer to [the design document](https://github.com/opendatahub-io/opendatahub-operator/blob/main/docs/DESIGN.md).

The list of the currently integrated ODH components is provided [at the end of this document](#integrated-components).

## Use scaffolding to create boilerplate code

Integrating a new component into the Open Data Hub (ODH) operator is easier with the [component-codegen CLI](../cmd/component-codegen/README.md). The CLI automates much of the boilerplate code and file generation, reducing manual effort and ensuring consistency.

While the CLI handles most of the heavy lifting, it's still important to understand the purpose of each generated file. Please refer to the following sections for a detailed breakdown of the key files and their roles in the integration process.

## Integrating a new component

To ensure a new component is integrated seamlessly into the operator, please follow the steps listed below.

### 1. Update API specs

The first step is to define the internal API spec for the new component and introduce it to the existing DataScienceCluster (DSC) API. Please proceed as follows:

#### Define internal API spec for the new component

1. Create a dedicated `<example_component_name>_types.go` file within `api/component/v1alpha1` directory.

2. Define the internal API spec for the new component according to the expected definitions.
You can use the following pseudo-implementation for reference:

```go
package v1alpha1

import (
    "github.com/opendatahub-io/opendatahub-operator/v2/api/common"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
    // example new component name
    ExampleComponentName         = "examplecomponent"

    // ExampleComponentInstanceName is the name of the new component instance singleton
    // value should match what is set in the kubebuilder markers for XValidation defined below
    ExampleComponentInstanceName = "default-examplecomponent"

    // kubernetes kind of the new component
    ExampleComponentKind         = "ExampleComponent"
)

type ExampleComponentCommonSpec struct {
    // Part of the public DSC API (included in CRD)
    common.DevFlagsSpec `json:",inline"`

    // Part of the public DSC API (included in CRD)
    // ( refer/define here if applicable to the new component )
}

// ExampleComponentSpec defines the desired state of ExampleComponent
type ExampleComponentSpec struct {
    // Part of the public DSC API (included in CRD)
    ExampleComponentCommonSpec `json:",inline"`

    // Internal only (not exposed in CRD)
    // ( refer/define here if applicable to the new component )
}

// ExampleComponentCommonStatus defines the shared observed state of ExampleComponent
type ExampleComponentCommonStatus struct {
    // add fields/attributes if needed
}

// ExampleComponentStatus defines the observed state of ExampleComponent
type ExampleComponentStatus struct {
    common.Status `json:",inline"`
    ExampleComponentCommonStatus `json:",inline"`
}

// default kubebuilder markers for the new component
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-examplecomponent'",message="ExampleComponent name must be default-examplecomponent"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// ExampleComponent is the Schema for the new component API
type ExampleComponent struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   ExampleComponentSpec   `json:"spec,omitempty"`
    Status ExampleComponentStatus `json:"status,omitempty"`
}

// GetDevFlags returns the component's development flags configuration.
// May return nil if DevFlagsSpec is not set. Callers must nil-check the result
// to avoid null pointer exceptions in reconciler code.
func (c *ExampleComponent) GetDevFlags() *common.DevFlags {
    return c.Spec.DevFlags
}

// status getter
func (c *ExampleComponent) GetStatus() *common.Status {
    return &c.Status.Status
}

func (c *ExampleComponent) GetConditions() []common.Condition {
    return c.Status.GetConditions()
}

func (c *ExampleComponent) SetConditions(conditions []common.Condition) {
    c.Status.SetConditions(conditions)
}

func (c *ExampleComponent) GetReleaseStatus() *[]common.ComponentRelease {
    return &c.Status.Releases
}

func (c *ExampleComponent) SetReleaseStatus(releases []common.ComponentRelease) {
    c.Status.Releases = releases
}

// +kubebuilder:object:root=true

// ExampleComponentList contains a list of ExampleComponent
type ExampleComponentList struct {
    metav1.TypeMeta `json:",inline"`
    metav1.ListMeta `json:"metadata,omitempty"`
    Items           []ExampleComponent `json:"items"`
}

// register the defined schemas
func init() {
    SchemeBuilder.Register(&ExampleComponent{}, &ExampleComponentList{})
}

// DSCExampleComponent contains all the configuration exposed in DSC instance for ExampleComponent component
// ( utilize DSC prefix here for naming consistency with the other integrated components )
type DSCExampleComponent struct {
    // configuration fields common across components
    common.ManagementSpec `json:",inline"`

    // new component-specific fields
    ExampleComponentCommonSpec `json:",inline"`
}

// DSCExampleComponentStatus struct holds the status for the ExampleComponent component exposed in the DSC
type DSCExampleComponentStatus struct {
type DSCExampleComponentStatus struct {
    common.Status                  `json:",inline"`
    ExampleComponentCommonStatus   `json:",inline"`
}
```

Alternatively, you can refer to the existing integrated component APIs located within `api/component/v1alpha1` directory.

#### Add Component to DataScienceCluster API spec

DataScienceCluster (DSC) CRD is responsible for enabling individual components and exposing them to end users.
To introduce the newly defined component API, extend the `Components` struct within the DataScienceCluster API spec (located within `api/datasciencecluster/v1`) to include the new API.

```diff
type Components struct {
    // Dashboard component configuration.
    Dashboard componentApi.DSCDashboard `json:"dashboard,omitempty"`

    // Workbenches component configuration.
    Workbenches componentApi.DSCWorkbenches `json:"workbenches,omitempty"`

    // ... other currently integrated components ...

    // add the new component as follows
+    ExampleComponent componentApi.DSCExampleComponent `json:"examplecomponent,omitempty"`
}
```

Additionally, extend the `ComponentsStatus` struct within the same file to include the new component status to be exposed in the DSC.

```diff
// ComponentsStatus defines the custom status of DataScienceCluster components.
type ComponentsStatus struct {
    // Dashboard component status.
    Dashboard componentApi.DSCDashboardStatus `json:"dashboard,omitempty"`

    // Workbenches component status.
    Workbenches componentApi.DSCWorkbenchesStatus `json:"workbenches,omitempty"`

    // ... other currently integrated component statuses ...

    // add the new component status as follows
+    ExampleComponent componentApi.DSCExampleComponentStatus `json:"examplecomponent,omitempty"`
}
```

#### Update kubebuilder_rbac.go

Add kubebuilder RBAC permissions intended for the new component into `internal/controller/datasciencecluster/kubebuilder_rbac.go`.

**Important:** After editing kubebuilder RBAC markers, you must re-run the project generators to ensure CRDs and manifests reflect the RBAC marker changes. This is covered in the next step.

#### Update the dependent files

To fully reflect the API changes brought by the addition of the new component, run the following command:

```make
make generate manifests api-docs bundle
```

This command will (re-)generate the necessary kubebuilder functions, and update both the API documentation and the operator bundle manifests.

### 2. Create a module for the new component reconciliation logic

To add new component-specific reconciler logic, create a dedicated `<example_component_name>` module, located in the `internal/controller/components` directory.
For reference, the `internal/controller/components` directory contains reconciler implementations for the currently integrated components.

#### Implement the component handler interface

Each component that is intended to be managed by the operator is expected to be included in the components registry.
The components registry (currently implemented in `internal/controller/components/registry`) defines a component handler interface which is required to be implemented for the new component.
To do so, create a dedicated `<example_component_name>.go` file within the newly created component module and provide the interface implementation.

The component handler interface requires the following methods to be implemented:

```go
type componentHandler struct{}

func init() { //nolint:gochecknoinits
    cr.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string

func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) operatorv1.ManagementState

func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) common.PlatformObject

func (s *componentHandler) Init(platform common.Platform) error 

func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error

func (s *componentHandler) UpdateDSCStatus(ctx context.Context, rr *types.ReconciliationRequest) (metav1.ConditionStatus, error)

func (s *componentHandler) IsEnabled(dsc *dscv1.DataScienceCluster) bool
```

**Method descriptions:**

- `GetName()` - Returns the component name as a string
- `GetManagementState()` - Returns the management state (Managed/Removed) for the component based on the DSC configuration
- `NewCRObject()` - Constructs the component-specific Custom Resource object
- `Init()` - Initializes the component (e.g., setting up image parameters)
- `NewComponentReconciler()` - Creates and configures the component's reconciler (see details [below](#actions))
- `UpdateDSCStatus()` - Updates the component-specific status in the DataScienceCluster
- `IsEnabled()` - Returns whether the component should be deployed/is active

Please refer the existing component implementations in the `internal/controller/components` directory for further details.

#### Implement the NewComponentReconciler method

The `NewComponentReconciler` method is part of the component handler interface and is responsible for creating the reconciler for the previously introduced `<ExampleComponent>` API.
This method should be implemented in the same `<example_component_name>.go` file where you defined the component handler struct.

`NewComponentReconciler` utilizes a generic builder pattern that supports defining various types of relationships and functionality:

- resource ownership - using `.Owns()`
- watching a resource - using `.Watches()`
- reconciler actions - using `.WithAction()`
  - this includes pre-implemented actions used commonly across components (e.g. manifests rendering), as well as customized, component-specific actions
  - more details on actions are provided [below](#actions)

The example pseudo-implementation should look like as follows:

```go
import (
    "context"
    "fmt"

    ctrl "sigs.k8s.io/controller-runtime"

    componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
    "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)
```

```go
func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
    _, err := reconciler.ReconcilerFor(mgr, &componentApi.ExampleComponent{}).
        Owns(...).
        // ... add other necessary resource ownerships
        Watches(...).
        // ... add other necessary resource watches
        WithAction(initialize).
        WithAction(devFlags).
        // ... add custom actions if needed
        // Mandatory common actions (order matters):
        WithAction(actions.RenderManifests).
        WithAction(actions.DeployManifests).
        WithAction(actions.UpdateStatus).
        // MUST be last:
        WithAction(actions.GarbageCollect).
        Build(ctx)

    if err != nil {
        return fmt.Errorf("building reconciler: %w", err)
    }

    return nil
}
```

##### Actions

Actions are functions that define pieces of component reconciliation logic. Any action is expected to conform to the following signature:

```go
func exampleAction(ctx context.Context, rr *odhtypes.ReconciliationRequest) error
```

Such actions can be then introduced to the reconciler builder using `.WithAction()` calls.
As seen in the existing component reconciler implementations, it would be recommended to include the action implementations in a separate file within the module, such as `<example_component_name>_controller_actions.go`.

"Generic"/commonly-implemented actions for each of the currently integrated components include:

- `initialize()` - to register paths to the component manifests
- `devFlags()` - to override the component manifest paths according to the Dev Flags configuration

In addition, proper generic actions, intended to be used across the components, are provided as part of the operator implementation (located in `pkg/controller/actions`).
These support:

- Manifest rendering — may use caching
- Manifest deployment — may use caching
- Status updating
- Garbage collection — MUST be called last before the final `.Build()` call

**Important:** All other actions (rendering, deployment, status updates) must precede garbage collection to prevent accidental reordering and ensure proper resource cleanup.

If the new component requires additional custom logic, custom actions can also be added to the builder via the respective `.WithAction()` calls.

For practical examples of all the above-mentioned functionality, please refer to the implementations within `internal/controller/components` directory.

#### Update main.go

Import the newly added component:

```diff
package main

import (
    // ... existing imports ...

    // ... component imports for the integrated components ...
+   _ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/<example_component>"
)
```

### 3. Add unit and e2e tests

Please add `unit` tests for any component-specific functions added to the codebase.

Please also add [e2e tests](https://github.com/opendatahub-io/opendatahub-operator/tree/main/tests/e2e) to
the e2e test suite to capture deployments introduced by the new component.
Existing e2e test suites for the integrated components can be also found there.

Lastly, please update the following files to fully integrate new component tests into the overall test suite:

- Update `setupDSCInstance()` in `tests/e2e/helper_test.go` to set the new component in the DSC
- Update `newDSC()` in `internal/webhook/webhook_suite_test.go` to include the new component when creating DSC
- Update `componentsTestSuites` map in `tests/e2e/controller_test.go` to reference the new component e2e test suite

### 4. Update Prometheus config and tests

If the component is planned to be released for downstream, Prometheus rules and promtest need to be updated for the component.

- Rules are located in `config/monitoring/prometheus/apps/prometheus-configs.yaml` file
- Tests are grouped in `tests/prometheus_unit_tests` as `<component>.unit-tests.yaml` files

## Integrated components

The canonical source of truth for currently integrated components is the `Components` struct in [`api/datasciencecluster/v1/datasciencecluster_types.go`](../api/datasciencecluster/v1/datasciencecluster_types.go#L32). This struct defines all supported components and their configuration options.

**Maintenance Note:** This documentation section must be updated whenever components are added, removed, or modified. The authoritative sources to verify and update are:

1. **Component API definitions**: [`api/components/v1alpha1/`](../api/components/v1alpha1/) - Contains the component type definitions
2. **Component controllers**: [`internal/controller/components/`](../internal/controller/components/) - Contains the reconciler implementations
3. **DSC Components struct**: [`api/datasciencecluster/v1/datasciencecluster_types.go`](../api/datasciencecluster/v1/datasciencecluster_types.go#L32) - The canonical source of truth
4. **CI validation**: [`tests/e2e/creation_test.go`](../tests/e2e/creation_test.go#L402) - Contains automated validation to ensure component lists stay in sync

The CI pipeline includes validation that checks if the component lists in tests match the DSC Components struct. If this validation fails, it indicates that the component lists need to be updated across the codebase.

For the most up-to-date list of integrated components, please refer to the `Components` struct in the canonical source file mentioned above.

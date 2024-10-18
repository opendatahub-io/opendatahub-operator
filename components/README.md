# Component Integration

The `components` dir of the codebase is hosts all the component specific logic of the operator. Since, ODH operator is an
integration point to deploy ODH component manifests it is essential to have common processes to integrate new components.

## Integrating a new component

To ensure a component is integrated seamlessly in the operator, follow the steps below:

### Add Component to DataScienceCluster API spec

DataScienceCluster CRD is responsible for defining the component fields and exposing them to end users.
Add your component to it's [api spec](../docs/api-overview.md#datascienceclusterspec):

```go
type Components struct {
   NewComponent newcomponent.newComponentName `json:"newcomponent,omitempty"`
}
```

### Add Component module

- Add a new module, `<newComponent>`,  under `components/` directory to define code specific to the new component. Example
can be found [here](https://github.com/opendatahub-io/opendatahub-operator/tree/main/components/datasciencepipelines)
- Define `Path` and `ComponentName` [variables](https://github.com/opendatahub-io/opendatahub-operator/blob/main/components/datasciencepipelines/datasciencepipelines.go#L11) for the new component.

### Implement common Interface

- Define struct that includes a shared struct `Component` with common fields.
- Implement [interface](https://github.com/opendatahub-io/opendatahub-operator/blob/main/components/component.go#L15) methods according to your component

    ```go
    type ComponentInterface interface {
      ReconcileComponent(ctx context.Context, cli client.Client, logger logr.Logger, owner metav1.Object, DSCISpec *dsciv1.DSCInitializationSpec, currentComponentStatus bool) error
      Cleanup(cli client.Client, DSCISpec *dsciv1.DSCInitializationSpec) error
      GetComponentName() string
      GetManagementState() operatorv1.ManagementState
      OverrideManifests(platform cluster.Platform) error
      UpdatePrometheusConfig(cli client.Client, enable bool, component string) error
      ConfigComponentLogger(logger logr.Logger, component string, dscispec *dsciv1.DSCInitializationSpec) logr.Logger
    }
    ```
  
### Add reconcile and Events

- Once you set up the new component module, add the component to [Reconcile](https://github.com/opendatahub-io/opendatahub-operator/blob/acaaf31f43e371456363f3fd272aec91ba413482/controllers/datasciencecluster/datasciencecluster_controller.go#L135) 
  function in order to deploy manifests.
- This will also enable/add status updates of the component in the operator.

### Reconcile Workflow
![Component Reconcile Workflow.png](Component%20Reconcile%20Workflow.png)

### Add Unit and e2e tests

- Components should add `unit` tests for any component specific functions added to the codebase
- Components should update [e2e tests](https://github.com/opendatahub-io/opendatahub-operator/tree/main/tests/e2e) to
  capture deployments introduced by the new component
## Integrated Components

- [Dashboard](https://github.com/opendatahub-io/opendatahub-operator/tree/main/components/dashboard)
- [Codeflare](https://github.com/opendatahub-io/opendatahub-operator/tree/main/components/codeflare)
- [Ray](https://github.com/opendatahub-io/opendatahub-operator/tree/main/components/ray)
- [Data Science Pipelines](https://github.com/opendatahub-io/opendatahub-operator/tree/main/components/datasciencepipelines)
- [KServe](https://github.com/opendatahub-io/opendatahub-operator/tree/main/components/kserve)
- [ModelMesh Serving](https://github.com/opendatahub-io/opendatahub-operator/tree/main/components/modelmeshserving)
- [Workbenches](https://github.com/opendatahub-io/opendatahub-operator/tree/main/components/workbenches)
- [Kueue](https://github.com/opendatahub-io/kueue)

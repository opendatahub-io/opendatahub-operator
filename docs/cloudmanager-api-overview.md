# API Reference

## Packages
- [infrastructure.opendatahub.io/v1alpha1](#infrastructureopendatahubiov1alpha1)


## infrastructure.opendatahub.io/v1alpha1

Package v1alpha1 contains API Schema definitions for the infrastructure v1alpha1 API group.

### Resource Types
- [AzureKubernetesEngine](#azurekubernetesengine)
- [CoreWeaveKubernetesEngine](#coreweavekubernetesengine)



#### AzureKubernetesEngine



AzureKubernetesEngine is the Schema for the azurekubernetesengines API.
It represents the configuration for an Azure Kubernetes Service (AKS) cluster.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `infrastructure.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `AzureKubernetesEngine` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[AzureKubernetesEngineSpec](#azurekubernetesenginespec)_ |  |  |  |
| `status` _[AzureKubernetesEngineStatus](#azurekubernetesenginestatus)_ |  |  |  |


#### AzureKubernetesEngineSpec



AzureKubernetesEngineSpec defines the desired state of AzureKubernetesEngine.



_Appears in:_
- [AzureKubernetesEngine](#azurekubernetesengine)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dependencies` _[Dependencies](#dependencies)_ | Dependencies defines the dependency configurations for the Azure Kubernetes Engine. |  |  |


#### AzureKubernetesEngineStatus



AzureKubernetesEngineStatus defines the observed state of AzureKubernetesEngine.



_Appears in:_
- [AzureKubernetesEngine](#azurekubernetesengine)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |


#### CoreWeaveKubernetesEngine



CoreWeaveKubernetesEngine is the Schema for the CoreWeaveKubernetesEngines API.
It represents the configuration for a CoreWeave Kubernetes Engine cluster.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `infrastructure.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `CoreWeaveKubernetesEngine` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[CoreWeaveKubernetesEngineSpec](#coreweavekubernetesenginespec)_ |  |  |  |
| `status` _[CoreWeaveKubernetesEngineStatus](#coreweavekubernetesenginestatus)_ |  |  |  |


#### CoreWeaveKubernetesEngineSpec



CoreWeaveKubernetesEngineSpec defines the desired state of CoreWeaveKubernetesEngine.



_Appears in:_
- [CoreWeaveKubernetesEngine](#coreweavekubernetesengine)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dependencies` _[Dependencies](#dependencies)_ | Dependencies defines the dependency configurations for the CoreWeave Kubernetes Engine. |  |  |


#### CoreWeaveKubernetesEngineStatus



CoreWeaveKubernetesEngineStatus defines the observed state of CoreWeaveKubernetesEngine.



_Appears in:_
- [CoreWeaveKubernetesEngine](#coreweavekubernetesengine)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |



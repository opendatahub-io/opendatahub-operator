# API Reference

## Packages
- [components.opendatahub.io/v1](#componentsopendatahubiov1)
- [datasciencecluster.opendatahub.io/v1](#datascienceclusteropendatahubiov1)
- [dscinitialization.opendatahub.io/v1](#dscinitializationopendatahubiov1)


## components.opendatahub.io/v1

Package v1 contains API Schema definitions for the components v1 API group

### Resource Types
- [CodeFlare](#codeflare)
- [CodeFlareList](#codeflarelist)
- [Dashboard](#dashboard)
- [DashboardList](#dashboardlist)
- [DataSciencePipelines](#datasciencepipelines)
- [DataSciencePipelinesList](#datasciencepipelineslist)
- [Kserve](#kserve)
- [KserveList](#kservelist)
- [Kueue](#kueue)
- [KueueList](#kueuelist)
- [ModelMeshServing](#modelmeshserving)
- [ModelMeshServingList](#modelmeshservinglist)
- [ModelRegistry](#modelregistry)
- [ModelRegistryList](#modelregistrylist)
- [Ray](#ray)
- [RayList](#raylist)
- [TrainingOperator](#trainingoperator)
- [TrainingOperatorList](#trainingoperatorlist)
- [TrustyAI](#trustyai)
- [TrustyAIList](#trustyailist)
- [Workbenches](#workbenches)
- [WorkbenchesList](#workbencheslist)



#### CodeFlare



CodeFlare is the Schema for the codeflares API



_Appears in:_
- [CodeFlareList](#codeflarelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `CodeFlare` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[CodeFlareSpec](#codeflarespec)_ |  |  |  |
| `status` _[CodeFlareStatus](#codeflarestatus)_ |  |  |  |


#### CodeFlareList



CodeFlareList contains a list of CodeFlare





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `CodeFlareList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[CodeFlare](#codeflare) array_ |  |  |  |


#### CodeFlareSpec



CodeFlareSpec defines the desired state of CodeFlare



_Appears in:_
- [CodeFlare](#codeflare)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | Foo is an example field of CodeFlare. Edit codeflare_types.go to remove/update |  |  |


#### CodeFlareStatus



CodeFlareStatus defines the observed state of CodeFlare



_Appears in:_
- [CodeFlare](#codeflare)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#condition-v1-meta) array_ |  |  |  |
| `observedGeneration` _integer_ |  |  |  |


#### DSCDashboard



DSCDashboard contains all the configuration exposed in DSC instance for Dashboard component



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### Dashboard



Dashboard is the Schema for the dashboards API



_Appears in:_
- [DashboardList](#dashboardlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `Dashboard` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DashboardSpec](#dashboardspec)_ |  |  |  |
| `status` _[DashboardStatus](#dashboardstatus)_ |  |  |  |


#### DashboardCommonSpec



DashboardCommonSpec spec defines the shared desired state of Dashboard



_Appears in:_
- [DSCDashboard](#dscdashboard)
- [DashboardSpec](#dashboardspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### DashboardList



DashboardList contains a list of Dashboard





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `DashboardList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[Dashboard](#dashboard) array_ |  |  |  |


#### DashboardSpec



DashboardSpec defines the desired state of Dashboard



_Appears in:_
- [Dashboard](#dashboard)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### DashboardStatus



DashboardStatus defines the observed state of Dashboard



_Appears in:_
- [Dashboard](#dashboard)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#condition-v1-meta) array_ |  |  |  |
| `observedGeneration` _integer_ |  |  |  |
| `url` _string_ |  |  |  |


#### DataSciencePipelines



DataSciencePipelines is the Schema for the datasciencepipelines API



_Appears in:_
- [DataSciencePipelinesList](#datasciencepipelineslist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `DataSciencePipelines` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DataSciencePipelinesSpec](#datasciencepipelinesspec)_ |  |  |  |
| `status` _[DataSciencePipelinesStatus](#datasciencepipelinesstatus)_ |  |  |  |


#### DataSciencePipelinesList



DataSciencePipelinesList contains a list of DataSciencePipelines





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `DataSciencePipelinesList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DataSciencePipelines](#datasciencepipelines) array_ |  |  |  |


#### DataSciencePipelinesSpec



DataSciencePipelinesSpec defines the desired state of DataSciencePipelines



_Appears in:_
- [DataSciencePipelines](#datasciencepipelines)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | Foo is an example field of DataSciencePipelines. Edit datasciencepipelines_types.go to remove/update |  |  |


#### DataSciencePipelinesStatus



DataSciencePipelinesStatus defines the observed state of DataSciencePipelines



_Appears in:_
- [DataSciencePipelines](#datasciencepipelines)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#condition-v1-meta) array_ |  |  |  |
| `observedGeneration` _integer_ |  |  |  |


#### Kserve



Kserve is the Schema for the kserves API



_Appears in:_
- [KserveList](#kservelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `Kserve` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[KserveSpec](#kservespec)_ |  |  |  |
| `status` _[KserveStatus](#kservestatus)_ |  |  |  |


#### KserveList



KserveList contains a list of Kserve





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `KserveList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[Kserve](#kserve) array_ |  |  |  |


#### KserveSpec



KserveSpec defines the desired state of Kserve



_Appears in:_
- [Kserve](#kserve)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | Foo is an example field of Kserve. Edit kserve_types.go to remove/update |  |  |


#### KserveStatus



KserveStatus defines the observed state of Kserve



_Appears in:_
- [Kserve](#kserve)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#condition-v1-meta) array_ |  |  |  |
| `observedGeneration` _integer_ |  |  |  |


#### Kueue



Kueue is the Schema for the kueues API



_Appears in:_
- [KueueList](#kueuelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `Kueue` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[KueueSpec](#kueuespec)_ |  |  |  |
| `status` _[KueueStatus](#kueuestatus)_ |  |  |  |


#### KueueList



KueueList contains a list of Kueue





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `KueueList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[Kueue](#kueue) array_ |  |  |  |


#### KueueSpec



KueueSpec defines the desired state of Kueue



_Appears in:_
- [Kueue](#kueue)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | Foo is an example field of Kueue. Edit kueue_types.go to remove/update |  |  |


#### KueueStatus



KueueStatus defines the observed state of Kueue



_Appears in:_
- [Kueue](#kueue)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#condition-v1-meta) array_ |  |  |  |
| `observedGeneration` _integer_ |  |  |  |


#### ModelMeshServing



ModelMeshServing is the Schema for the modelmeshservings API



_Appears in:_
- [ModelMeshServingList](#modelmeshservinglist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `ModelMeshServing` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ModelMeshServingSpec](#modelmeshservingspec)_ |  |  |  |
| `status` _[ModelMeshServingStatus](#modelmeshservingstatus)_ |  |  |  |


#### ModelMeshServingList



ModelMeshServingList contains a list of ModelMeshServing





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `ModelMeshServingList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[ModelMeshServing](#modelmeshserving) array_ |  |  |  |


#### ModelMeshServingSpec



ModelMeshServingSpec defines the desired state of ModelMeshServing



_Appears in:_
- [ModelMeshServing](#modelmeshserving)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | Foo is an example field of ModelMeshServing. Edit modelmeshserving_types.go to remove/update |  |  |


#### ModelMeshServingStatus



ModelMeshServingStatus defines the observed state of ModelMeshServing



_Appears in:_
- [ModelMeshServing](#modelmeshserving)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#condition-v1-meta) array_ |  |  |  |
| `observedGeneration` _integer_ |  |  |  |


#### ModelRegistry



ModelRegistry is the Schema for the modelregistries API



_Appears in:_
- [ModelRegistryList](#modelregistrylist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `ModelRegistry` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ModelRegistrySpec](#modelregistryspec)_ |  |  |  |
| `status` _[ModelRegistryStatus](#modelregistrystatus)_ |  |  |  |


#### ModelRegistryList



ModelRegistryList contains a list of ModelRegistry





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `ModelRegistryList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[ModelRegistry](#modelregistry) array_ |  |  |  |


#### ModelRegistrySpec



ModelRegistrySpec defines the desired state of ModelRegistry



_Appears in:_
- [ModelRegistry](#modelregistry)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | Foo is an example field of ModelRegistry. Edit modelregistry_types.go to remove/update |  |  |


#### ModelRegistryStatus



ModelRegistryStatus defines the observed state of ModelRegistry



_Appears in:_
- [ModelRegistry](#modelregistry)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#condition-v1-meta) array_ |  |  |  |
| `observedGeneration` _integer_ |  |  |  |


#### Ray



Ray is the Schema for the rays API



_Appears in:_
- [RayList](#raylist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `Ray` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[RaySpec](#rayspec)_ |  |  |  |
| `status` _[RayStatus](#raystatus)_ |  |  |  |


#### RayList



RayList contains a list of Ray





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `RayList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[Ray](#ray) array_ |  |  |  |


#### RaySpec



RaySpec defines the desired state of Ray



_Appears in:_
- [Ray](#ray)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | Foo is an example field of Ray. Edit ray_types.go to remove/update |  |  |


#### RayStatus



RayStatus defines the observed state of Ray



_Appears in:_
- [Ray](#ray)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#condition-v1-meta) array_ |  |  |  |
| `observedGeneration` _integer_ |  |  |  |


#### TrainingOperator



TrainingOperator is the Schema for the trainingoperators API



_Appears in:_
- [TrainingOperatorList](#trainingoperatorlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `TrainingOperator` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[TrainingOperatorSpec](#trainingoperatorspec)_ |  |  |  |
| `status` _[TrainingOperatorStatus](#trainingoperatorstatus)_ |  |  |  |


#### TrainingOperatorList



TrainingOperatorList contains a list of TrainingOperator





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `TrainingOperatorList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[TrainingOperator](#trainingoperator) array_ |  |  |  |


#### TrainingOperatorSpec



TrainingOperatorSpec defines the desired state of TrainingOperator



_Appears in:_
- [TrainingOperator](#trainingoperator)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | Foo is an example field of TrainingOperator. Edit trainingoperator_types.go to remove/update |  |  |


#### TrainingOperatorStatus



TrainingOperatorStatus defines the observed state of TrainingOperator



_Appears in:_
- [TrainingOperator](#trainingoperator)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#condition-v1-meta) array_ |  |  |  |
| `observedGeneration` _integer_ |  |  |  |


#### TrustyAI



TrustyAI is the Schema for the trustyais API



_Appears in:_
- [TrustyAIList](#trustyailist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `TrustyAI` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[TrustyAISpec](#trustyaispec)_ |  |  |  |
| `status` _[TrustyAIStatus](#trustyaistatus)_ |  |  |  |


#### TrustyAIList



TrustyAIList contains a list of TrustyAI





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `TrustyAIList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[TrustyAI](#trustyai) array_ |  |  |  |


#### TrustyAISpec



TrustyAISpec defines the desired state of TrustyAI



_Appears in:_
- [TrustyAI](#trustyai)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | Foo is an example field of TrustyAI. Edit trustyai_types.go to remove/update |  |  |


#### TrustyAIStatus



TrustyAIStatus defines the observed state of TrustyAI



_Appears in:_
- [TrustyAI](#trustyai)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#condition-v1-meta) array_ |  |  |  |
| `observedGeneration` _integer_ |  |  |  |


#### Workbenches



Workbenches is the Schema for the workbenches API



_Appears in:_
- [WorkbenchesList](#workbencheslist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `Workbenches` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[WorkbenchesSpec](#workbenchesspec)_ |  |  |  |
| `status` _[WorkbenchesStatus](#workbenchesstatus)_ |  |  |  |


#### WorkbenchesList



WorkbenchesList contains a list of Workbenches





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.opendatahub.io/v1` | | |
| `kind` _string_ | `WorkbenchesList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[Workbenches](#workbenches) array_ |  |  |  |


#### WorkbenchesSpec



WorkbenchesSpec defines the desired state of Workbenches



_Appears in:_
- [Workbenches](#workbenches)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `foo` _string_ | Foo is an example field of Workbenches. Edit workbenches_types.go to remove/update |  |  |


#### WorkbenchesStatus



WorkbenchesStatus defines the observed state of Workbenches



_Appears in:_
- [Workbenches](#workbenches)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#condition-v1-meta) array_ |  |  |  |
| `observedGeneration` _integer_ |  |  |  |



## datasciencecluster.opendatahub.io/codeflare

Package codeflare provides utility functions to config CodeFlare as part of the stack
which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists



#### CodeFlare



CodeFlare struct holds the configuration for the CodeFlare component.



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `Component` _[Component](#component)_ |  |  |  |



## datasciencecluster.opendatahub.io/components




#### Component



Component struct defines the basis for each OpenDataHub component configuration.



_Appears in:_
- [CodeFlare](#codeflare)
- [DataSciencePipelines](#datasciencepipelines)
- [Kserve](#kserve)
- [Kueue](#kueue)
- [ModelMeshServing](#modelmeshserving)
- [ModelRegistry](#modelregistry)
- [Ray](#ray)
- [TrainingOperator](#trainingoperator)
- [TrustyAI](#trustyai)
- [Workbenches](#workbenches)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |






#### DevFlags



DevFlags defines list of fields that can be used by developers to test customizations. This is not recommended
to be used in production environment.



_Appears in:_
- [Component](#component)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `manifests` _[ManifestsConfig](#manifestsconfig) array_ | List of custom manifests for the given component |  |  |


#### DevFlagsSpec



DevFlagsSpec struct defines the component's dev flags configuration.



_Appears in:_
- [Component](#component)
- [DSCDashboard](#dscdashboard)
- [DashboardCommonSpec](#dashboardcommonspec)
- [DashboardSpec](#dashboardspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### ManagementSpec



ManagementSpec struct defines the component's management configuration.



_Appears in:_
- [Component](#component)
- [DSCDashboard](#dscdashboard)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |




#### Status







_Appears in:_
- [CodeFlareStatus](#codeflarestatus)
- [DashboardStatus](#dashboardstatus)
- [DataSciencePipelinesStatus](#datasciencepipelinesstatus)
- [KserveStatus](#kservestatus)
- [KueueStatus](#kueuestatus)
- [ModelMeshServingStatus](#modelmeshservingstatus)
- [ModelRegistryStatus](#modelregistrystatus)
- [RayStatus](#raystatus)
- [TrainingOperatorStatus](#trainingoperatorstatus)
- [TrustyAIStatus](#trustyaistatus)
- [WorkbenchesStatus](#workbenchesstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#condition-v1-meta) array_ |  |  |  |
| `observedGeneration` _integer_ |  |  |  |







## datasciencecluster.opendatahub.io/datasciencepipelines

Package datasciencepipelines provides utility functions to config Data Science Pipelines:
Pipeline solution for end to end MLOps workflows that support the Kubeflow Pipelines SDK, Tekton and Argo Workflows.



#### DataSciencePipelines



DataSciencePipelines struct holds the configuration for the DataSciencePipelines component.



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `Component` _[Component](#component)_ |  |  |  |



## datasciencecluster.opendatahub.io/kserve

Package kserve provides utility functions to config Kserve as the Controller for serving ML models on arbitrary frameworks



#### DefaultDeploymentMode

_Underlying type:_ _string_



_Validation:_
- Pattern: `^(Serverless|RawDeployment)$`

_Appears in:_
- [Kserve](#kserve)



#### Kserve



Kserve struct holds the configuration for the Kserve component.



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `Component` _[Component](#component)_ |  |  |  |
| `serving` _[ServingSpec](#servingspec)_ | Serving configures the KNative-Serving stack used for model serving. A Service<br />Mesh (Istio) is prerequisite, since it is used as networking layer. |  |  |
| `defaultDeploymentMode` _[DefaultDeploymentMode](#defaultdeploymentmode)_ | Configures the default deployment mode for Kserve. This can be set to 'Serverless' or 'RawDeployment'.<br />The value specified in this field will be used to set the default deployment mode in the 'inferenceservice-config' configmap for Kserve.<br />This field is optional. If no default deployment mode is specified, Kserve will use Serverless mode. |  | Enum: [Serverless RawDeployment] <br />Pattern: `^(Serverless\|RawDeployment)$` <br /> |



## datasciencecluster.opendatahub.io/kueue




#### Kueue



Kueue struct holds the configuration for the Kueue component.



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `Component` _[Component](#component)_ |  |  |  |



## datasciencecluster.opendatahub.io/modelmeshserving

Package modelmeshserving provides utility functions to config MoModelMesh, a general-purpose model serving management/routing layer



#### ModelMeshServing



ModelMeshServing struct holds the configuration for the ModelMeshServing component.



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `Component` _[Component](#component)_ |  |  |  |



## datasciencecluster.opendatahub.io/modelregistry

Package modelregistry provides utility functions to config ModelRegistry, an ML Model metadata repository service



#### ModelRegistry







_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `Component` _[Component](#component)_ |  |  |  |
| `registriesNamespace` _string_ | Namespace for model registries to be installed, configurable only once when model registry is enabled, defaults to "odh-model-registries" | odh-model-registries | MaxLength: 63 <br />Pattern: `^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$` <br /> |



## datasciencecluster.opendatahub.io/ray

Package ray provides utility functions to config Ray as part of the stack
which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists



#### Ray



Ray struct holds the configuration for the Ray component.



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `Component` _[Component](#component)_ |  |  |  |



## datasciencecluster.opendatahub.io/trainingoperator

Package trainingoperator provides utility functions to config trainingoperator as part of the stack
which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists



#### TrainingOperator



TrainingOperator struct holds the configuration for the TrainingOperator component.



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `Component` _[Component](#component)_ |  |  |  |



## datasciencecluster.opendatahub.io/trustyai

Package trustyai provides utility functions to config TrustyAI, a bias/fairness and explainability toolkit



#### TrustyAI



TrustyAI struct holds the configuration for the TrustyAI component.



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `Component` _[Component](#component)_ |  |  |  |



## datasciencecluster.opendatahub.io/v1


### Resource Types
- [DataScienceCluster](#datasciencecluster)



#### AuthSpec







_Appears in:_
- [ServiceMeshSpec](#servicemeshspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `namespace` _string_ | Namespace where it is deployed. If not provided, the default is to<br />use '-auth-provider' suffix on the ApplicationsNamespace of the DSCI. |  | MaxLength: 63 <br />Pattern: `^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$` <br /> |
| `audiences` _string_ | Audiences is a list of the identifiers that the resource server presented<br />with the token identifies as. Audience-aware token authenticators will verify<br />that the token was intended for at least one of the audiences in this list.<br />If no audiences are provided, the audience will default to the audience of the<br />Kubernetes apiserver (kubernetes.default.svc). | [https://kubernetes.default.svc] |  |


#### CertType

_Underlying type:_ _string_





_Appears in:_
- [CertificateSpec](#certificatespec)

| Field | Description |
| --- | --- |
| `SelfSigned` |  |
| `Provided` |  |
| `OpenshiftDefaultIngress` |  |


#### CertificateSpec



CertificateSpec represents the specification of the certificate securing communications of
an Istio Gateway.



_Appears in:_
- [GatewaySpec](#gatewayspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `secretName` _string_ | SecretName specifies the name of the Kubernetes Secret resource that contains a<br />TLS certificate secure HTTP communications for the KNative network. |  |  |
| `type` _[CertType](#certtype)_ | Type specifies if the TLS certificate should be generated automatically, or if the certificate<br />is provided by the user. Allowed values are:<br />* SelfSigned: A certificate is going to be generated using an own private key.<br />* Provided: Pre-existence of the TLS Secret (see SecretName) with a valid certificate is assumed.<br />* OpenshiftDefaultIngress: Default ingress certificate configured for OpenShift | OpenshiftDefaultIngress | Enum: [SelfSigned Provided OpenshiftDefaultIngress] <br /> |


#### Components







_Appears in:_
- [DataScienceClusterSpec](#datascienceclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dashboard` _[DSCDashboard](#dscdashboard)_ | Dashboard component configuration. |  |  |
| `workbenches` _[Workbenches](#workbenches)_ | Workbenches component configuration. |  |  |
| `modelmeshserving` _[ModelMeshServing](#modelmeshserving)_ | ModelMeshServing component configuration.<br />Does not support enabled Kserve at the same time |  |  |
| `datasciencepipelines` _[DataSciencePipelines](#datasciencepipelines)_ | DataServicePipeline component configuration.<br />Require OpenShift Pipelines Operator to be installed before enable component |  |  |
| `kserve` _[Kserve](#kserve)_ | Kserve component configuration.<br />Require OpenShift Serverless and OpenShift Service Mesh Operators to be installed before enable component<br />Does not support enabled ModelMeshServing at the same time |  |  |
| `kueue` _[Kueue](#kueue)_ | Kueue component configuration. |  |  |
| `codeflare` _[CodeFlare](#codeflare)_ | CodeFlare component configuration.<br />If CodeFlare Operator has been installed in the cluster, it should be uninstalled first before enabled component. |  |  |
| `ray` _[Ray](#ray)_ | Ray component configuration. |  |  |
| `trustyai` _[TrustyAI](#trustyai)_ | TrustyAI component configuration. |  |  |
| `modelregistry` _[ModelRegistry](#modelregistry)_ | ModelRegistry component configuration. |  |  |
| `trainingoperator` _[TrainingOperator](#trainingoperator)_ | Training Operator component configuration. |  |  |


#### ComponentsStatus



ComponentsStatus defines the custom status of DataScienceCluster components.



_Appears in:_
- [DataScienceClusterStatus](#datascienceclusterstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `modelregistry` _[ModelRegistryStatus](#modelregistrystatus)_ | ModelRegistry component status |  |  |


#### ControlPlaneSpec







_Appears in:_
- [ServiceMeshSpec](#servicemeshspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is a name Service Mesh Control Plane. Defaults to "data-science-smcp". | data-science-smcp |  |
| `namespace` _string_ | Namespace is a namespace where Service Mesh is deployed. Defaults to "istio-system". | istio-system | MaxLength: 63 <br />Pattern: `^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$` <br /> |
| `metricsCollection` _string_ | MetricsCollection specifies if metrics from components on the Mesh namespace<br />should be collected. Setting the value to "Istio" will collect metrics from the<br />control plane and any proxies on the Mesh namespace (like gateway pods). Setting<br />to "None" will disable metrics collection. | Istio | Enum: [Istio None] <br /> |


#### DataScienceCluster



DataScienceCluster is the Schema for the datascienceclusters API.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `datasciencecluster.opendatahub.io/v1` | | |
| `kind` _string_ | `DataScienceCluster` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DataScienceClusterSpec](#datascienceclusterspec)_ |  |  |  |
| `status` _[DataScienceClusterStatus](#datascienceclusterstatus)_ |  |  |  |


#### DataScienceClusterSpec



DataScienceClusterSpec defines the desired state of the cluster.



_Appears in:_
- [DataScienceCluster](#datasciencecluster)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `components` _[Components](#components)_ | Override and fine tune specific component configurations. |  |  |


#### DataScienceClusterStatus



DataScienceClusterStatus defines the observed state of DataScienceCluster.



_Appears in:_
- [DataScienceCluster](#datasciencecluster)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ | Phase describes the Phase of DataScienceCluster reconciliation state<br />This is used by OLM UI to provide status information to the user |  |  |
| `conditions` _Condition array_ | Conditions describes the state of the DataScienceCluster resource. |  |  |
| `relatedObjects` _[ObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectreference-v1-core) array_ | RelatedObjects is a list of objects created and maintained by this operator.<br />Object references will be added to this list after they have been created AND found in the cluster. |  |  |
| `errorMessage` _string_ |  |  |  |
| `installedComponents` _object (keys:string, values:boolean)_ | List of components with status if installed or not |  |  |
| `components` _[ComponentsStatus](#componentsstatus)_ | Expose component's specific status |  |  |
| `release` _[Release](#release)_ | Version and release type |  |  |


#### GatewaySpec



GatewaySpec represents the configuration of the Ingress Gateways.



_Appears in:_
- [ServingSpec](#servingspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `domain` _string_ | Domain specifies the host name for intercepting incoming requests.<br />Most likely, you will want to use a wildcard name, like *.example.com.<br />If not set, the domain of the OpenShift Ingress is used.<br />If you choose to generate a certificate, this is the domain used for the certificate request. |  |  |
| `certificate` _[CertificateSpec](#certificatespec)_ | Certificate specifies configuration of the TLS certificate securing communication<br />for the gateway. |  |  |


#### ServiceMeshSpec



ServiceMeshSpec configures Service Mesh.



_Appears in:_
- [DSCInitializationSpec](#dscinitializationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ |  | Removed | Enum: [Managed Unmanaged Removed] <br /> |
| `controlPlane` _[ControlPlaneSpec](#controlplanespec)_ | ControlPlane holds configuration of Service Mesh used by Opendatahub. |  |  |
| `auth` _[AuthSpec](#authspec)_ | Auth holds configuration of authentication and authorization services<br />used by Service Mesh in Opendatahub. |  |  |


#### ServingSpec



ServingSpec specifies the configuration for the KNative Serving components and their
bindings with the Service Mesh.



_Appears in:_
- [Kserve](#kserve)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ |  | Managed | Enum: [Managed Unmanaged Removed] <br /> |
| `name` _string_ | Name specifies the name of the KNativeServing resource that is going to be<br />created to instruct the KNative Operator to deploy KNative serving components.<br />This resource is created in the "knative-serving" namespace. | knative-serving |  |
| `ingressGateway` _[GatewaySpec](#gatewayspec)_ | IngressGateway allows to customize some parameters for the Istio Ingress Gateway<br />that is bound to KNative-Serving. |  |  |



## datasciencecluster.opendatahub.io/workbenches

Package workbenches provides utility functions to config Workbenches to secure Jupyter Notebook in Kubernetes environments with support for OAuth



#### Workbenches



Workbenches struct holds the configuration for the Workbenches component.



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `Component` _[Component](#component)_ |  |  |  |



## dscinitialization.opendatahub.io/v1

Package v1 contains API Schema definitions for the dscinitialization v1 API group

### Resource Types
- [DSCInitialization](#dscinitialization)



#### DSCInitialization



DSCInitialization is the Schema for the dscinitializations API.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `dscinitialization.opendatahub.io/v1` | | |
| `kind` _string_ | `DSCInitialization` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DSCInitializationSpec](#dscinitializationspec)_ |  |  |  |
| `status` _[DSCInitializationStatus](#dscinitializationstatus)_ |  |  |  |


#### DSCInitializationSpec



DSCInitializationSpec defines the desired state of DSCInitialization.



_Appears in:_
- [DSCInitialization](#dscinitialization)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `applicationsNamespace` _string_ | Namespace for applications to be installed, non-configurable, default to "opendatahub" | opendatahub | MaxLength: 63 <br />Pattern: `^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$` <br /> |
| `monitoring` _[Monitoring](#monitoring)_ | Enable monitoring on specified namespace |  |  |
| `serviceMesh` _[ServiceMeshSpec](#servicemeshspec)_ | Configures Service Mesh as networking layer for Data Science Clusters components.<br />The Service Mesh is a mandatory prerequisite for single model serving (KServe) and<br />you should review this configuration if you are planning to use KServe.<br />For other components, it enhances user experience; e.g. it provides unified<br />authentication giving a Single Sign On experience. |  |  |
| `trustedCABundle` _[TrustedCABundleSpec](#trustedcabundlespec)_ | When set to `Managed`, adds odh-trusted-ca-bundle Configmap to all namespaces that includes<br />cluster-wide Trusted CA Bundle in .data["ca-bundle.crt"].<br />Additionally, this fields allows admins to add custom CA bundles to the configmap using the .CustomCABundle field. |  |  |
| `devFlags` _[DevFlags](#devflags)_ | Internal development useful field to test customizations.<br />This is not recommended to be used in production environment. |  |  |


#### DSCInitializationStatus



DSCInitializationStatus defines the observed state of DSCInitialization.



_Appears in:_
- [DSCInitialization](#dscinitialization)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ | Phase describes the Phase of DSCInitializationStatus<br />This is used by OLM UI to provide status information to the user |  |  |
| `conditions` _Condition array_ | Conditions describes the state of the DSCInitializationStatus resource |  |  |
| `relatedObjects` _[ObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectreference-v1-core) array_ | RelatedObjects is a list of objects created and maintained by this operator.<br />Object references will be added to this list after they have been created AND found in the cluster |  |  |
| `errorMessage` _string_ |  |  |  |
| `release` _[Release](#release)_ | Version and release type |  |  |


#### DevFlags



DevFlags defines list of fields that can be used by developers to test customizations. This is not recommended
to be used in production environment.



_Appears in:_
- [DSCInitializationSpec](#dscinitializationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `manifestsUri` _string_ | Custom manifests uri for odh-manifests |  |  |
| `logmode` _string_ |  | production | Enum: [devel development prod production default] <br /> |


#### Monitoring







_Appears in:_
- [DSCInitializationSpec](#dscinitializationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so.<br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it. |  | Enum: [Managed Removed] <br /> |
| `namespace` _string_ | Namespace for monitoring if it is enabled | opendatahub | MaxLength: 63 <br />Pattern: `^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$` <br /> |


#### TrustedCABundleSpec







_Appears in:_
- [DSCInitializationSpec](#dscinitializationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | managementState indicates whether and how the operator should manage customized CA bundle | Removed | Enum: [Managed Removed Unmanaged] <br /> |
| `customCABundle` _string_ | A custom CA bundle that will be available for  all  components in the<br />Data Science Cluster(DSC). This bundle will be stored in odh-trusted-ca-bundle<br />ConfigMap .data.odh-ca-bundle.crt . |  |  |



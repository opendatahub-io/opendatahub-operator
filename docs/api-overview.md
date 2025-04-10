# API Reference

## Packages
- [components.platform.opendatahub.io/v1alpha1](#componentsplatformopendatahubiov1alpha1)
- [datasciencecluster.opendatahub.io/v1](#datascienceclusteropendatahubiov1)
- [dscinitialization.opendatahub.io/v1](#dscinitializationopendatahubiov1)
- [services.platform.opendatahub.io/v1alpha1](#servicesplatformopendatahubiov1alpha1)


## components.platform.opendatahub.io/v1alpha1

Package v1 contains API Schema definitions for the components v1 API group

### Resource Types
- [CodeFlare](#codeflare)
- [CodeFlareList](#codeflarelist)
- [Dashboard](#dashboard)
- [DashboardList](#dashboardlist)
- [DataSciencePipelines](#datasciencepipelines)
- [DataSciencePipelinesList](#datasciencepipelineslist)
- [FeastOperator](#feastoperator)
- [FeastOperatorList](#feastoperatorlist)
- [Kserve](#kserve)
- [KserveList](#kservelist)
- [Kueue](#kueue)
- [KueueList](#kueuelist)
- [ModelController](#modelcontroller)
- [ModelControllerList](#modelcontrollerlist)
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
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `CodeFlare` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[CodeFlareSpec](#codeflarespec)_ |  |  |  |
| `status` _[CodeFlareStatus](#codeflarestatus)_ |  |  |  |


#### CodeFlareCommonSpec







_Appears in:_
- [CodeFlareSpec](#codeflarespec)
- [DSCCodeFlare](#dsccodeflare)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### CodeFlareCommonStatus



CodeFlareCommonStatus defines the shared observed state of CodeFlare



_Appears in:_
- [CodeFlareStatus](#codeflarestatus)
- [DSCCodeFlareStatus](#dsccodeflarestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### CodeFlareList



CodeFlareList contains a list of CodeFlare





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `CodeFlareList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[CodeFlare](#codeflare) array_ |  |  |  |


#### CodeFlareSpec







_Appears in:_
- [CodeFlare](#codeflare)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### CodeFlareStatus



CodeFlareStatus defines the observed state of CodeFlare



_Appears in:_
- [CodeFlare](#codeflare)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### DSCCodeFlare







_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### DSCCodeFlareStatus



DSCCodeFlareStatus contains the observed state of the CodeFlare exposed in the DSC instance



_Appears in:_
- [ComponentsStatus](#componentsstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |


#### DSCDashboard



DSCDashboard contains all the configuration exposed in DSC instance for Dashboard component



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### DSCDashboardStatus



DSCDashboardStatus contains the observed state of the Dashboard exposed in the DSC instance



_Appears in:_
- [ComponentsStatus](#componentsstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |


#### DSCDataSciencePipelines



DSCDataSciencePipelines contains all the configuration exposed in DSC instance for DataSciencePipelines component



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### DSCDataSciencePipelinesStatus



DSCDataSciencePipelinesStatus contains the observed state of the DataSciencePipelines exposed in the DSC instance



_Appears in:_
- [ComponentsStatus](#componentsstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |


#### DSCFeastOperator



DSCFeastOperator defines the configuration exposed in the DSC instance for FeastOperator



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### DSCFeastOperatorStatus



DSCFeastOperatorStatus struct holds the status for the FeastOperator component exposed in the DSC



_Appears in:_
- [ComponentsStatus](#componentsstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |


#### DSCKserve



DSCKserve contains all the configuration exposed in DSC instance for Kserve component



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |
| `serving` _[ServingSpec](#servingspec)_ | Serving configures the KNative-Serving stack used for model serving. A Service<br />Mesh (Istio) is prerequisite, since it is used as networking layer. |  |  |
| `defaultDeploymentMode` _[DefaultDeploymentMode](#defaultdeploymentmode)_ | Configures the default deployment mode for Kserve. This can be set to 'Serverless' or 'RawDeployment'.<br />The value specified in this field will be used to set the default deployment mode in the 'inferenceservice-config' configmap for Kserve.<br />This field is optional. If no default deployment mode is specified, Kserve will use Serverless mode. |  | Enum: [Serverless RawDeployment] <br />Pattern: `^(Serverless\|RawDeployment)$` <br /> |
| `rawDeploymentServiceConfig` _[RawServiceConfig](#rawserviceconfig)_ | Configures the type of service that is created for InferenceServices using RawDeployment.<br />The values for RawDeploymentServiceConfig can be "Headless" or "Headed".<br />Headless : sets "ServiceClusterIPNone = true" in the 'inferenceservice-config' configmap for Kserve.<br />Headed : sets "ServiceClusterIPNone = false" in the 'inferenceservice-config' configmap for Kserve. | Headless | Enum: [Headless Headed] <br />Pattern: `^(Headless\|Headed)$` <br /> |
| `nim` _[NimSpec](#nimspec)_ | Configures and enables NVIDIA NIM integration |  |  |


#### DSCKserveStatus



DSCKserveStatus contains the observed state of the Kserve exposed in the DSC instance



_Appears in:_
- [ComponentsStatus](#componentsstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |


#### DSCKueue



DSCKueue contains all the configuration exposed in DSC instance for Kueue component



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### DSCKueueStatus



DSCKueueStatus contains the observed state of the Kueue exposed in the DSC instance



_Appears in:_
- [ComponentsStatus](#componentsstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |


#### DSCModelMeshServing



DSCModelMeshServing contains all the configuration exposed in DSC instance for ModelMeshServing component



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### DSCModelMeshServingStatus



DSCModelMeshServingStatus contains the observed state of the ModelMeshServing exposed in the DSC instance



_Appears in:_
- [ComponentsStatus](#componentsstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |


#### DSCModelRegistry



DSCModelRegistry contains all the configuration exposed in DSC instance for ModelRegistry component



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |
| `registriesNamespace` _string_ | Namespace for model registries to be installed, configurable only once when model registry is enabled, defaults to "rhoai-model-registries" | rhoai-model-registries | MaxLength: 63 <br />Pattern: `^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$` <br /> |


#### DSCModelRegistryStatus



DSCModelRegistryStatus struct holds the status for the ModelRegistry component exposed in the DSC



_Appears in:_
- [ComponentsStatus](#componentsstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |


#### DSCRay



DSCRay contains all the configuration exposed in DSC instance for Ray component



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### DSCRayStatus



DSCRayStatus struct holds the status for the Ray component exposed in the DSC



_Appears in:_
- [ComponentsStatus](#componentsstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |


#### DSCTrainingOperator



DSCTrainingOperator contains all the configuration exposed in DSC instance for TrainingOperator component



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### DSCTrainingOperatorStatus



DSCTrainingOperatorStatus struct holds the status for the TrainingOperator component exposed in the DSC



_Appears in:_
- [ComponentsStatus](#componentsstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |


#### DSCTrustyAI



DSCTrustyAI contains all the configuration exposed in DSC instance for TrustyAI component



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### DSCTrustyAIStatus



DSCTrustyAIStatus struct holds the status for the TrustyAI component exposed in the DSC



_Appears in:_
- [ComponentsStatus](#componentsstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |


#### DSCWorkbenches



DSCWorkbenches contains all the configuration exposed in DSC instance for Workbenches component



_Appears in:_
- [Components](#components)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |
| `workbenchNamespace` _string_ | Namespace for workbenches to be installed, configurable only once when workbenches are enabled, defaults to "rhods-notebooks" | rhods-notebooks | MaxLength: 63 <br />Pattern: `^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$` <br /> |


#### DSCWorkbenchesStatus



DSCWorkbenchesStatus struct holds the status for the Workbenches component exposed in the DSC



_Appears in:_
- [ComponentsStatus](#componentsstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |


#### Dashboard



Dashboard is the Schema for the dashboards API



_Appears in:_
- [DashboardList](#dashboardlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
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


#### DashboardCommonStatus



DashboardCommonStatus defines the shared observed state of Dashboard



_Appears in:_
- [DSCDashboardStatus](#dscdashboardstatus)
- [DashboardStatus](#dashboardstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `url` _string_ |  |  |  |


#### DashboardList



DashboardList contains a list of Dashboard





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
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
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |
| `url` _string_ |  |  |  |


#### DataSciencePipelines



DataSciencePipelines is the Schema for the datasciencepipelines API



_Appears in:_
- [DataSciencePipelinesList](#datasciencepipelineslist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `DataSciencePipelines` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DataSciencePipelinesSpec](#datasciencepipelinesspec)_ |  |  |  |
| `status` _[DataSciencePipelinesStatus](#datasciencepipelinesstatus)_ |  |  |  |


#### DataSciencePipelinesCommonSpec







_Appears in:_
- [DSCDataSciencePipelines](#dscdatasciencepipelines)
- [DataSciencePipelinesSpec](#datasciencepipelinesspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### DataSciencePipelinesCommonStatus



DataSciencePipelinesCommonStatus defines the shared observed state of DataSciencePipelines



_Appears in:_
- [DSCDataSciencePipelinesStatus](#dscdatasciencepipelinesstatus)
- [DataSciencePipelinesStatus](#datasciencepipelinesstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### DataSciencePipelinesList



DataSciencePipelinesList contains a list of DataSciencePipelines





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
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
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### DataSciencePipelinesStatus



DataSciencePipelinesStatus defines the observed state of DataSciencePipelines



_Appears in:_
- [DataSciencePipelines](#datasciencepipelines)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### DefaultDeploymentMode

_Underlying type:_ _string_



_Validation:_
- Pattern: `^(Serverless|RawDeployment)$`

_Appears in:_
- [DSCKserve](#dsckserve)
- [KserveCommonSpec](#kservecommonspec)
- [KserveSpec](#kservespec)

| Field | Description |
| --- | --- |
| `Serverless` | Serverless will be used as the default deployment mode for Kserve. This requires Serverless and ServiceMesh operators configured as dependencies.<br /> |
| `RawDeployment` | RawDeployment will be used as the default deployment mode for Kserve.<br /> |


#### FeastOperator



FeastOperator is the Schema for the FeastOperator API



_Appears in:_
- [FeastOperatorList](#feastoperatorlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `FeastOperator` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[FeastOperatorSpec](#feastoperatorspec)_ |  |  |  |
| `status` _[FeastOperatorStatus](#feastoperatorstatus)_ |  |  |  |


#### FeastOperatorCommonSpec



FeastOperatorCommonSpec defines the common spec shared across APIs for FeastOperator



_Appears in:_
- [DSCFeastOperator](#dscfeastoperator)
- [FeastOperatorSpec](#feastoperatorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### FeastOperatorCommonStatus



FeastOperatorCommonStatus defines the shared observed state of FeastOperator



_Appears in:_
- [DSCFeastOperatorStatus](#dscfeastoperatorstatus)
- [FeastOperatorStatus](#feastoperatorstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### FeastOperatorList



FeastOperatorList contains a list of FeastOperator objects





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `FeastOperatorList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[FeastOperator](#feastoperator) array_ |  |  |  |


#### FeastOperatorSpec



FeastOperatorSpec defines the desired state of FeastOperator



_Appears in:_
- [FeastOperator](#feastoperator)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### FeastOperatorStatus



FeastOperatorStatus defines the observed state of FeastOperator



_Appears in:_
- [FeastOperator](#feastoperator)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### Kserve



Kserve is the Schema for the kserves API



_Appears in:_
- [KserveList](#kservelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `Kserve` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[KserveSpec](#kservespec)_ |  |  |  |
| `status` _[KserveStatus](#kservestatus)_ |  |  |  |


#### KserveCommonSpec



KserveCommonSpec spec defines the shared desired state of Kserve



_Appears in:_
- [DSCKserve](#dsckserve)
- [KserveSpec](#kservespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |
| `serving` _[ServingSpec](#servingspec)_ | Serving configures the KNative-Serving stack used for model serving. A Service<br />Mesh (Istio) is prerequisite, since it is used as networking layer. |  |  |
| `defaultDeploymentMode` _[DefaultDeploymentMode](#defaultdeploymentmode)_ | Configures the default deployment mode for Kserve. This can be set to 'Serverless' or 'RawDeployment'.<br />The value specified in this field will be used to set the default deployment mode in the 'inferenceservice-config' configmap for Kserve.<br />This field is optional. If no default deployment mode is specified, Kserve will use Serverless mode. |  | Enum: [Serverless RawDeployment] <br />Pattern: `^(Serverless\|RawDeployment)$` <br /> |
| `rawDeploymentServiceConfig` _[RawServiceConfig](#rawserviceconfig)_ | Configures the type of service that is created for InferenceServices using RawDeployment.<br />The values for RawDeploymentServiceConfig can be "Headless" or "Headed".<br />Headless : sets "ServiceClusterIPNone = true" in the 'inferenceservice-config' configmap for Kserve.<br />Headed : sets "ServiceClusterIPNone = false" in the 'inferenceservice-config' configmap for Kserve. | Headless | Enum: [Headless Headed] <br />Pattern: `^(Headless\|Headed)$` <br /> |
| `nim` _[NimSpec](#nimspec)_ | Configures and enables NVIDIA NIM integration |  |  |


#### KserveCommonStatus



KserveCommonStatus defines the shared observed state of Kserve



_Appears in:_
- [DSCKserveStatus](#dsckservestatus)
- [KserveStatus](#kservestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `defaultDeploymentMode` _string_ | DefaultDeploymentMode is the value of the defaultDeploymentMode field<br />as read from the "deploy" JSON in the inferenceservice-config ConfigMap |  |  |
| `serverlessMode` _[ManagementState](#managementstate)_ |  |  |  |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### KserveList



KserveList contains a list of Kserve





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
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
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |
| `serving` _[ServingSpec](#servingspec)_ | Serving configures the KNative-Serving stack used for model serving. A Service<br />Mesh (Istio) is prerequisite, since it is used as networking layer. |  |  |
| `defaultDeploymentMode` _[DefaultDeploymentMode](#defaultdeploymentmode)_ | Configures the default deployment mode for Kserve. This can be set to 'Serverless' or 'RawDeployment'.<br />The value specified in this field will be used to set the default deployment mode in the 'inferenceservice-config' configmap for Kserve.<br />This field is optional. If no default deployment mode is specified, Kserve will use Serverless mode. |  | Enum: [Serverless RawDeployment] <br />Pattern: `^(Serverless\|RawDeployment)$` <br /> |
| `rawDeploymentServiceConfig` _[RawServiceConfig](#rawserviceconfig)_ | Configures the type of service that is created for InferenceServices using RawDeployment.<br />The values for RawDeploymentServiceConfig can be "Headless" or "Headed".<br />Headless : sets "ServiceClusterIPNone = true" in the 'inferenceservice-config' configmap for Kserve.<br />Headed : sets "ServiceClusterIPNone = false" in the 'inferenceservice-config' configmap for Kserve. | Headless | Enum: [Headless Headed] <br />Pattern: `^(Headless\|Headed)$` <br /> |
| `nim` _[NimSpec](#nimspec)_ | Configures and enables NVIDIA NIM integration |  |  |


#### KserveStatus



KserveStatus defines the observed state of Kserve



_Appears in:_
- [Kserve](#kserve)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |
| `defaultDeploymentMode` _string_ | DefaultDeploymentMode is the value of the defaultDeploymentMode field<br />as read from the "deploy" JSON in the inferenceservice-config ConfigMap |  |  |
| `serverlessMode` _[ManagementState](#managementstate)_ |  |  |  |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### Kueue



Kueue is the Schema for the kueues API



_Appears in:_
- [KueueList](#kueuelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `Kueue` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[KueueSpec](#kueuespec)_ |  |  |  |
| `status` _[KueueStatus](#kueuestatus)_ |  |  |  |


#### KueueCommonSpec







_Appears in:_
- [DSCKueue](#dsckueue)
- [KueueSpec](#kueuespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### KueueCommonStatus



KueueCommonStatus defines the shared observed state of Kueue



_Appears in:_
- [DSCKueueStatus](#dsckueuestatus)
- [KueueStatus](#kueuestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### KueueList



KueueList contains a list of Kueue





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
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
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### KueueStatus



KueueStatus defines the observed state of Kueue



_Appears in:_
- [Kueue](#kueue)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### ModelController



ModelController is the Schema for the modelcontroller API, it is a shared component between kserve and modelmeshserving



_Appears in:_
- [ModelControllerList](#modelcontrollerlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `ModelController` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ModelControllerSpec](#modelcontrollerspec)_ |  |  |  |
| `status` _[ModelControllerStatus](#modelcontrollerstatus)_ |  |  |  |


#### ModelControllerKerveSpec



a mini version of the DSCKserve only keep devflags and management spec



_Appears in:_
- [ModelControllerSpec](#modelcontrollerspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ |  |  |  |
| `nim` _[NimSpec](#nimspec)_ |  |  |  |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### ModelControllerList



ModelControllerList contains a list of ModelController





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `ModelControllerList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[ModelController](#modelcontroller) array_ |  |  |  |


#### ModelControllerMMSpec



a mini version of the DSCModelMeshServing only keep devflags and management spec



_Appears in:_
- [ModelControllerSpec](#modelcontrollerspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ |  |  |  |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### ModelControllerMRSpec







_Appears in:_
- [ModelControllerSpec](#modelcontrollerspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ |  |  |  |


#### ModelControllerSpec



ModelControllerSpec defines the desired state of ModelController



_Appears in:_
- [ModelController](#modelcontroller)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `kserve` _[ModelControllerKerveSpec](#modelcontrollerkervespec)_ | ModelMeshServing DSCModelMeshServing `json:"modelMeshServing,omitempty"` |  |  |
| `modelMeshServing` _[ModelControllerMMSpec](#modelcontrollermmspec)_ |  |  |  |
| `modelRegistry` _[ModelControllerMRSpec](#modelcontrollermrspec)_ |  |  |  |


#### ModelControllerStatus



ModelControllerStatus defines the observed state of ModelController



_Appears in:_
- [ModelController](#modelcontroller)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |


#### ModelMeshServing



ModelMeshServing is the Schema for the modelmeshservings API



_Appears in:_
- [ModelMeshServingList](#modelmeshservinglist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `ModelMeshServing` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ModelMeshServingSpec](#modelmeshservingspec)_ |  |  |  |
| `status` _[ModelMeshServingStatus](#modelmeshservingstatus)_ |  |  |  |


#### ModelMeshServingCommonSpec







_Appears in:_
- [DSCModelMeshServing](#dscmodelmeshserving)
- [ModelMeshServingSpec](#modelmeshservingspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### ModelMeshServingCommonStatus



ModelMeshServingCommonStatus defines the shared observed state of ModelMeshServing



_Appears in:_
- [DSCModelMeshServingStatus](#dscmodelmeshservingstatus)
- [ModelMeshServingStatus](#modelmeshservingstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### ModelMeshServingList



ModelMeshServingList contains a list of ModelMeshServing





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
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
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### ModelMeshServingStatus



ModelMeshServingStatus defines the observed state of ModelMeshServing



_Appears in:_
- [ModelMeshServing](#modelmeshserving)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### ModelRegistry



ModelRegistry is the Schema for the modelregistries API



_Appears in:_
- [ModelRegistryList](#modelregistrylist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `ModelRegistry` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ModelRegistrySpec](#modelregistryspec)_ |  |  |  |
| `status` _[ModelRegistryStatus](#modelregistrystatus)_ |  |  |  |


#### ModelRegistryCommonSpec



ModelRegistryCommonSpec spec defines the shared desired state of ModelRegistry



_Appears in:_
- [DSCModelRegistry](#dscmodelregistry)
- [ModelRegistrySpec](#modelregistryspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |
| `registriesNamespace` _string_ | Namespace for model registries to be installed, configurable only once when model registry is enabled, defaults to "rhoai-model-registries" | rhoai-model-registries | MaxLength: 63 <br />Pattern: `^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$` <br /> |


#### ModelRegistryCommonStatus



ModelRegistryCommonStatus defines the shared observed state of ModelRegistry



_Appears in:_
- [DSCModelRegistryStatus](#dscmodelregistrystatus)
- [ModelRegistryStatus](#modelregistrystatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `registriesNamespace` _string_ |  |  |  |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### ModelRegistryList



ModelRegistryList contains a list of ModelRegistry





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
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
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |
| `registriesNamespace` _string_ | Namespace for model registries to be installed, configurable only once when model registry is enabled, defaults to "rhoai-model-registries" | rhoai-model-registries | MaxLength: 63 <br />Pattern: `^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$` <br /> |


#### ModelRegistryStatus



ModelRegistryStatus defines the observed state of ModelRegistry



_Appears in:_
- [ModelRegistry](#modelregistry)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |
| `registriesNamespace` _string_ |  |  |  |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### NimSpec



nimSpec enables NVIDIA NIM integration



_Appears in:_
- [DSCKserve](#dsckserve)
- [KserveCommonSpec](#kservecommonspec)
- [KserveSpec](#kservespec)
- [ModelControllerKerveSpec](#modelcontrollerkervespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ |  | Managed | Enum: [Managed Removed] <br /> |


#### RawServiceConfig

_Underlying type:_ _string_



_Validation:_
- Pattern: `^(Headless|Headed)$`

_Appears in:_
- [DSCKserve](#dsckserve)
- [KserveCommonSpec](#kservecommonspec)
- [KserveSpec](#kservespec)

| Field | Description |
| --- | --- |
| `Headless` |  |
| `Headed` |  |


#### Ray



Ray is the Schema for the rays API



_Appears in:_
- [RayList](#raylist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `Ray` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[RaySpec](#rayspec)_ |  |  |  |
| `status` _[RayStatus](#raystatus)_ |  |  |  |


#### RayCommonSpec







_Appears in:_
- [DSCRay](#dscray)
- [RaySpec](#rayspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### RayCommonStatus



RayCommonStatus defines the shared observed state of Ray



_Appears in:_
- [DSCRayStatus](#dscraystatus)
- [RayStatus](#raystatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### RayList



RayList contains a list of Ray





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
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
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### RayStatus



RayStatus defines the observed state of Ray



_Appears in:_
- [Ray](#ray)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### TrainingOperator



TrainingOperator is the Schema for the trainingoperators API



_Appears in:_
- [TrainingOperatorList](#trainingoperatorlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `TrainingOperator` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[TrainingOperatorSpec](#trainingoperatorspec)_ |  |  |  |
| `status` _[TrainingOperatorStatus](#trainingoperatorstatus)_ |  |  |  |


#### TrainingOperatorCommonSpec







_Appears in:_
- [DSCTrainingOperator](#dsctrainingoperator)
- [TrainingOperatorSpec](#trainingoperatorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### TrainingOperatorCommonStatus



TrainingOperatorCommonStatus defines the shared observed state of TrainingOperator



_Appears in:_
- [DSCTrainingOperatorStatus](#dsctrainingoperatorstatus)
- [TrainingOperatorStatus](#trainingoperatorstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### TrainingOperatorList



TrainingOperatorList contains a list of TrainingOperator





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
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
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### TrainingOperatorStatus



TrainingOperatorStatus defines the observed state of TrainingOperator



_Appears in:_
- [TrainingOperator](#trainingoperator)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### TrustyAI



TrustyAI is the Schema for the trustyais API



_Appears in:_
- [TrustyAIList](#trustyailist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `TrustyAI` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[TrustyAISpec](#trustyaispec)_ |  |  |  |
| `status` _[TrustyAIStatus](#trustyaistatus)_ |  |  |  |


#### TrustyAICommonSpec







_Appears in:_
- [DSCTrustyAI](#dsctrustyai)
- [TrustyAISpec](#trustyaispec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### TrustyAICommonStatus



TrustyAICommonStatus defines the shared observed state of TrustyAI



_Appears in:_
- [DSCTrustyAIStatus](#dsctrustyaistatus)
- [TrustyAIStatus](#trustyaistatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### TrustyAIList



TrustyAIList contains a list of TrustyAI





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
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
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |


#### TrustyAIStatus



TrustyAIStatus defines the observed state of TrustyAI



_Appears in:_
- [TrustyAI](#trustyai)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |


#### Workbenches



Workbenches is the Schema for the workbenches API



_Appears in:_
- [WorkbenchesList](#workbencheslist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `Workbenches` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[WorkbenchesSpec](#workbenchesspec)_ |  |  |  |
| `status` _[WorkbenchesStatus](#workbenchesstatus)_ |  |  |  |


#### WorkbenchesCommonSpec







_Appears in:_
- [DSCWorkbenches](#dscworkbenches)
- [WorkbenchesSpec](#workbenchesspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |
| `workbenchNamespace` _string_ | Namespace for workbenches to be installed, configurable only once when workbenches are enabled, defaults to "rhods-notebooks" | rhods-notebooks | MaxLength: 63 <br />Pattern: `^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$` <br /> |


#### WorkbenchesCommonStatus



WorkbenchesCommonStatus defines the shared observed state of Workbenches



_Appears in:_
- [DSCWorkbenchesStatus](#dscworkbenchesstatus)
- [WorkbenchesStatus](#workbenchesstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |
| `workbenchNamespace` _string_ |  |  |  |


#### WorkbenchesList



WorkbenchesList contains a list of Workbenches





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `components.platform.opendatahub.io/v1alpha1` | | |
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
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |  |  |
| `workbenchNamespace` _string_ | Namespace for workbenches to be installed, configurable only once when workbenches are enabled, defaults to "rhods-notebooks" | rhods-notebooks | MaxLength: 63 <br />Pattern: `^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$` <br /> |


#### WorkbenchesStatus



WorkbenchesStatus defines the observed state of Workbenches



_Appears in:_
- [Workbenches](#workbenches)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |
| `releases` _[ComponentRelease](#componentrelease) array_ |  |  |  |
| `workbenchNamespace` _string_ |  |  |  |



## datasciencecluster.opendatahub.io/components







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
| `workbenches` _[DSCWorkbenches](#dscworkbenches)_ | Workbenches component configuration. |  |  |
| `modelmeshserving` _[DSCModelMeshServing](#dscmodelmeshserving)_ | ModelMeshServing component configuration. |  |  |
| `datasciencepipelines` _[DSCDataSciencePipelines](#dscdatasciencepipelines)_ | DataSciencePipeline component configuration.<br />Requires OpenShift Pipelines Operator to be installed before enable component |  |  |
| `kserve` _[DSCKserve](#dsckserve)_ | Kserve component configuration.<br />Requires OpenShift Serverless and OpenShift Service Mesh Operators to be installed before enable component<br />Does not support enabled ModelMeshServing at the same time |  |  |
| `kueue` _[DSCKueue](#dsckueue)_ | Kueue component configuration. |  |  |
| `codeflare` _[DSCCodeFlare](#dsccodeflare)_ | CodeFlare component configuration.<br />If CodeFlare Operator has been installed in the cluster, it should be uninstalled first before enabling component. |  |  |
| `ray` _[DSCRay](#dscray)_ | Ray component configuration. |  |  |
| `trustyai` _[DSCTrustyAI](#dsctrustyai)_ | TrustyAI component configuration. |  |  |
| `modelregistry` _[DSCModelRegistry](#dscmodelregistry)_ | ModelRegistry component configuration. |  |  |
| `trainingoperator` _[DSCTrainingOperator](#dsctrainingoperator)_ | Training Operator component configuration. |  |  |
| `feastoperator` _[DSCFeastOperator](#dscfeastoperator)_ | Feast Operator component configuration. |  |  |


#### ComponentsStatus



ComponentsStatus defines the custom status of DataScienceCluster components.



_Appears in:_
- [DataScienceClusterStatus](#datascienceclusterstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dashboard` _[DSCDashboardStatus](#dscdashboardstatus)_ | Dashboard component status. |  |  |
| `workbenches` _[DSCWorkbenchesStatus](#dscworkbenchesstatus)_ | Workbenches component status. |  |  |
| `modelmeshserving` _[DSCModelMeshServingStatus](#dscmodelmeshservingstatus)_ | ModelMeshServing component status. |  |  |
| `datasciencepipelines` _[DSCDataSciencePipelinesStatus](#dscdatasciencepipelinesstatus)_ | DataSciencePipeline component status. |  |  |
| `kserve` _[DSCKserveStatus](#dsckservestatus)_ | Kserve component status. |  |  |
| `kueue` _[DSCKueueStatus](#dsckueuestatus)_ | Kueue component status. |  |  |
| `codeflare` _[DSCCodeFlareStatus](#dsccodeflarestatus)_ | CodeFlare component status. |  |  |
| `ray` _[DSCRayStatus](#dscraystatus)_ | Ray component status. |  |  |
| `trustyai` _[DSCTrustyAIStatus](#dsctrustyaistatus)_ | TrustyAI component status. |  |  |
| `modelregistry` _[DSCModelRegistryStatus](#dscmodelregistrystatus)_ | ModelRegistry component status. |  |  |
| `trainingoperator` _[DSCTrainingOperatorStatus](#dsctrainingoperatorstatus)_ | Training Operator component status. |  |  |
| `feastoperator` _[DSCFeastOperatorStatus](#dscfeastoperatorstatus)_ | Feast Operator component status. |  |  |


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
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |
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
- [DSCKserve](#dsckserve)
- [KserveCommonSpec](#kservecommonspec)
- [KserveSpec](#kservespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ |  | Managed | Enum: [Managed Unmanaged Removed] <br /> |
| `name` _string_ | Name specifies the name of the KNativeServing resource that is going to be<br />created to instruct the KNative Operator to deploy KNative serving components.<br />This resource is created in the "knative-serving" namespace. | knative-serving |  |
| `ingressGateway` _[GatewaySpec](#gatewayspec)_ | IngressGateway allows to customize some parameters for the Istio Ingress Gateway<br />that is bound to KNative-Serving. |  |  |



## dscinitialization.opendatahub.io/services







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
| `applicationsNamespace` _string_ | Namespace for applications to be installed, non-configurable, default to "redhat-ods-applications" | redhat-ods-applications | MaxLength: 63 <br />Pattern: `^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$` <br /> |
| `monitoring` _[DSCIMonitoring](#dscimonitoring)_ | Enable monitoring on specified namespace |  |  |
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
| `manifestsUri` _string_ | ## DEPRECATED ## : ManifestsUri set on DSCI is not maintained.<br />Custom manifests uri for odh-manifests |  |  |
| `logmode` _string_ | ## DEPRECATED ##: Ignored, use LogLevel instead | production | Enum: [devel development prod production default] <br /> |
| `logLevel` _string_ | Override Zap log level. Can be "debug", "info", "error" or a number (more verbose). |  |  |


#### TrustedCABundleSpec







_Appears in:_
- [DSCInitializationSpec](#dscinitializationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | managementState indicates whether and how the operator should manage customized CA bundle | Removed | Enum: [Managed Removed Unmanaged] <br /> |
| `customCABundle` _string_ | A custom CA bundle that will be available for  all  components in the<br />Data Science Cluster(DSC). This bundle will be stored in odh-trusted-ca-bundle<br />ConfigMap .data.odh-ca-bundle.crt . |  |  |



## services.platform.opendatahub.io/v1alpha1

Package v1 contains API Schema definitions for the services v1 API group

### Resource Types
- [Auth](#auth)
- [AuthList](#authlist)
- [Monitoring](#monitoring)
- [MonitoringList](#monitoringlist)



#### Auth



Auth is the Schema for the auths API



_Appears in:_
- [AuthList](#authlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `services.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `Auth` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[AuthSpec](#authspec)_ |  |  |  |
| `status` _[AuthStatus](#authstatus)_ |  |  |  |


#### AuthList



AuthList contains a list of Auth





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `services.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `AuthList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[Auth](#auth) array_ |  |  |  |


#### AuthSpec



AuthSpec defines the desired state of Auth



_Appears in:_
- [Auth](#auth)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `adminGroups` _string array_ |  |  |  |
| `allowedGroups` _string array_ |  |  |  |


#### AuthStatus



AuthStatus defines the observed state of Auth



_Appears in:_
- [Auth](#auth)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |


#### DSCIMonitoring







_Appears in:_
- [DSCInitializationSpec](#dscinitializationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values:<br /><br />- "Managed" : the operator is actively managing the component and trying to keep it active.<br />              It will only upgrade the component if it is safe to do so<br /><br />- "Removed" : the operator is actively managing the component and will not install it,<br />              or if it is installed, the operator will try to remove it |  | Enum: [Managed Removed] <br /> |
| `namespace` _string_ | monitoring spec exposed to DSCI api<br />Namespace for monitoring if it is enabled | redhat-ods-monitoring | MaxLength: 63 <br />Pattern: `^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$` <br /> |


#### Monitoring



Monitoring is the Schema for the monitorings API



_Appears in:_
- [MonitoringList](#monitoringlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `services.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `Monitoring` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[MonitoringSpec](#monitoringspec)_ |  |  |  |
| `status` _[MonitoringStatus](#monitoringstatus)_ |  |  |  |


#### MonitoringCommonSpec



MonitoringCommonSpec spec defines the shared desired state of Dashboard



_Appears in:_
- [DSCIMonitoring](#dscimonitoring)
- [MonitoringSpec](#monitoringspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `namespace` _string_ | monitoring spec exposed to DSCI api<br />Namespace for monitoring if it is enabled | redhat-ods-monitoring | MaxLength: 63 <br />Pattern: `^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$` <br /> |


#### MonitoringList



MonitoringList contains a list of Monitoring





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `services.platform.opendatahub.io/v1alpha1` | | |
| `kind` _string_ | `MonitoringList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  |  |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  |  |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[Monitoring](#monitoring) array_ |  |  |  |


#### MonitoringSpec



MonitoringSpec defines the desired state of Monitoring



_Appears in:_
- [Monitoring](#monitoring)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `namespace` _string_ | monitoring spec exposed to DSCI api<br />Namespace for monitoring if it is enabled | redhat-ods-monitoring | MaxLength: 63 <br />Pattern: `^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$` <br /> |


#### MonitoringStatus



MonitoringStatus defines the observed state of Monitoring



_Appears in:_
- [Monitoring](#monitoring)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ |  |  |  |
| `observedGeneration` _integer_ | The generation observed by the resource controller. |  |  |
| `conditions` _[Condition](#condition) array_ |  |  |  |
| `url` _string_ |  |  |  |



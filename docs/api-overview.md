# API Reference

## Packages
- [datasciencecluster.opendatahub.io/v1](#datascienceclusteropendatahubiov1)
- [dscinitialization.opendatahub.io/v1](#dscinitializationopendatahubiov1)
- [features.opendatahub.io/v1](#featuresopendatahubiov1)


## datasciencecluster.opendatahub.io/v1

Package v1 contains API Schema definitions for the datasciencecluster v1 API group

### Resource Types
- [DataScienceCluster](#datasciencecluster)



#### Components





_Appears in:_
- [DataScienceClusterSpec](#datascienceclusterspec)

| Field | Description |
| --- | --- |
| `dashboard` _[Dashboard](#dashboard)_ | Dashboard component configuration. |
| `workbenches` _[Workbenches](#workbenches)_ | Workbenches component configuration. |
| `modelmeshserving` _[ModelMeshServing](#modelmeshserving)_ | ModelMeshServing component configuration. Does not support enabled Kserve at the same time |
| `datasciencepipelines` _[DataSciencePipelines](#datasciencepipelines)_ | DataServicePipeline component configuration. Require OpenShift Pipelines Operator to be installed before enable component |
| `kserve` _[Kserve](#kserve)_ | Kserve component configuration. Require OpenShift Serverless and OpenShift Service Mesh Operators to be installed before enable component Does not support enabled ModelMeshServing at the same time |
| `kueue` _[Kueue](#kueue)_ | Kueue component configuration. |
| `codeflare` _[CodeFlare](#codeflare)_ | CodeFlare component configuration. If CodeFlare Operator has been installed in the cluster, it should be uninstalled first before enabled component. |
| `ray` _[Ray](#ray)_ | Ray component configuration. |
| `trustyai` _[TrustyAI](#trustyai)_ | TrustyAI component configuration. |
| `modelregistry` _[ModelRegistry](#modelregistry)_ | ModelRegistry component configuration. |


#### DataScienceCluster



DataScienceCluster is the Schema for the datascienceclusters API.



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `datasciencecluster.opendatahub.io/v1`
| `kind` _string_ | `DataScienceCluster`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.22/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[DataScienceClusterSpec](#datascienceclusterspec)_ |  |
| `status` _[DataScienceClusterStatus](#datascienceclusterstatus)_ |  |


#### DataScienceClusterSpec



DataScienceClusterSpec defines the desired state of the cluster.

_Appears in:_
- [DataScienceCluster](#datasciencecluster)

| Field | Description |
| --- | --- |
| `components` _[Components](#components)_ | Override and fine tune specific component configurations. |


#### DataScienceClusterStatus



DataScienceClusterStatus defines the observed state of DataScienceCluster.

_Appears in:_
- [DataScienceCluster](#datasciencecluster)

| Field | Description |
| --- | --- |
| `phase` _string_ | Phase describes the Phase of DataScienceCluster reconciliation state This is used by OLM UI to provide status information to the user |
| `conditions` _Condition array_ | Conditions describes the state of the DataScienceCluster resource. |
| `relatedObjects` _[ObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.22/#objectreference-v1-core) array_ | RelatedObjects is a list of objects created and maintained by this operator. Object references will be added to this list after they have been created AND found in the cluster. |
| `errorMessage` _string_ |  |
| `installedComponents` _object (keys:string, values:boolean)_ | List of components with status if installed or not |



## dscinitialization.opendatahub.io/v1

Package v1 contains API Schema definitions for the dscinitialization v1 API group

### Resource Types
- [DSCInitialization](#dscinitialization)



#### DSCInitialization



DSCInitialization is the Schema for the dscinitializations API.



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `dscinitialization.opendatahub.io/v1`
| `kind` _string_ | `DSCInitialization`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.22/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[DSCInitializationSpec](#dscinitializationspec)_ |  |
| `status` _[DSCInitializationStatus](#dscinitializationstatus)_ |  |


#### DSCInitializationSpec



DSCInitializationSpec defines the desired state of DSCInitialization.

_Appears in:_
- [DSCInitialization](#dscinitialization)

| Field | Description |
| --- | --- |
| `applicationsNamespace` _string_ | Namespace for applications to be installed, non-configurable, default to "opendatahub" |
| `monitoring` _[Monitoring](#monitoring)_ | Enable monitoring on specified namespace |
| `serviceMesh` _[ServiceMeshSpec](#servicemeshspec)_ | Configures Service Mesh as networking layer for Data Science Clusters components. The Service Mesh is a mandatory prerequisite for single model serving (KServe) and you should review this configuration if you are planning to use KServe. For other components, it enhances user experience; e.g. it provides unified authentication giving a Single Sign On experience. |
| `trustedCABundle` _[TrustedCABundleSpec](#trustedcabundlespec)_ | When set to `Managed`, adds odh-trusted-ca-bundle Configmap to all namespaces that includes cluster-wide Trusted CA Bundle in .data["ca-bundle.crt"]. Additionally, this fields allows admins to add custom CA bundles to the configmap using the .CustomCABundle field. |
| `devFlags` _[DevFlags](#devflags)_ | Internal development useful field to test customizations. This is not recommended to be used in production environment. |


#### DSCInitializationStatus



DSCInitializationStatus defines the observed state of DSCInitialization.

_Appears in:_
- [DSCInitialization](#dscinitialization)

| Field | Description |
| --- | --- |
| `phase` _string_ | Phase describes the Phase of DSCInitializationStatus This is used by OLM UI to provide status information to the user |
| `conditions` _Condition array_ | Conditions describes the state of the DSCInitializationStatus resource |
| `relatedObjects` _[ObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.22/#objectreference-v1-core) array_ | RelatedObjects is a list of objects created and maintained by this operator. Object references will be added to this list after they have been created AND found in the cluster |
| `errorMessage` _string_ |  |


#### DevFlags



DevFlags defines list of fields that can be used by developers to test customizations. This is not recommended to be used in production environment.

_Appears in:_
- [DSCInitializationSpec](#dscinitializationspec)

| Field | Description |
| --- | --- |
| `manifestsUri` _string_ | Custom manifests uri for odh-manifests |


#### Monitoring





_Appears in:_
- [DSCInitializationSpec](#dscinitializationspec)

| Field | Description |
| --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values: - "Managed" : the operator is actively managing the component and trying to keep it active. It will only upgrade the component if it is safe to do so. - "Removed" : the operator is actively managing the component and will not install it, or if it is installed, the operator will try to remove it. |
| `namespace` _string_ | Namespace for monitoring if it is enabled |


#### TrustedCABundleSpec





_Appears in:_
- [DSCInitializationSpec](#dscinitializationspec)

| Field | Description |
| --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | managementState indicates whether and how the operator should manage customized CA bundle |
| `customCABundle` _string_ | A custom CA bundle that will be available for  all  components in the Data Science Cluster(DSC). This bundle will be stored in odh-trusted-ca-bundle ConfigMap .data.odh-ca-bundle.crt . |



## features.opendatahub.io/v1

Package v1 contains API Schema definitions for the datasciencecluster v1 API group

### Resource Types
- [FeatureTracker](#featuretracker)





#### FeatureTracker



FeatureTracker represents a cluster-scoped resource in the Data Science Cluster, specifically designed for monitoring and managing objects created via the internal Features API. This resource serves a crucial role in cross-namespace resource management, acting as an owner reference for various resources. The primary purpose of the FeatureTracker is to enable efficient garbage collection by Kubernetes. This is essential for ensuring that resources are automatically cleaned up and reclaimed when they are no longer required.



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `features.opendatahub.io/v1`
| `kind` _string_ | `FeatureTracker`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.22/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[FeatureTrackerSpec](#featuretrackerspec)_ |  |
| `status` _[FeatureTrackerStatus](#featuretrackerstatus)_ |  |


#### FeatureTrackerSpec



FeatureTrackerSpec defines the desired state of FeatureTracker.

_Appears in:_
- [FeatureTracker](#featuretracker)

| Field | Description |
| --- | --- |
| `source` _[Source](#source)_ |  |
| `appNamespace` _string_ |  |


#### FeatureTrackerStatus



FeatureTrackerStatus defines the observed state of FeatureTracker.

_Appears in:_
- [FeatureTracker](#featuretracker)

| Field | Description |
| --- | --- |
| `conditions` _[Condition](#condition)_ |  |


#### OwnerType

_Underlying type:_ _string_



_Appears in:_
- [Source](#source)



#### Source



Source describes the type of object that created the related Feature to this FeatureTracker.

_Appears in:_
- [FeatureTrackerSpec](#featuretrackerspec)

| Field | Description |
| --- | --- |
| `type` _[OwnerType](#ownertype)_ |  |
| `name` _string_ |  |



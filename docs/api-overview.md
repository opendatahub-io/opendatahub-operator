# API Reference

## Packages
- [datasciencecluster.opendatahub.io/v1](#datascienceclusteropendatahubiov1)
- [dscinitialization.opendatahub.io/v1](#dscinitializationopendatahubiov1)


## datasciencecluster.opendatahub.io/codeflare

Package codeflare provides utility functions to config CodeFlare as part of the stack
which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists



#### CodeFlare



CodeFlare struct holds the configuration for the CodeFlare component.

_Appears in:_
- [Components](#components)

| Field | Description |
| --- | --- |
| `Component` _[Component](#component)_ |  |



## datasciencecluster.opendatahub.io/components




#### Component



Component struct defines the basis for each OpenDataHub component configuration.

_Appears in:_
- [CodeFlare](#codeflare)
- [Dashboard](#dashboard)
- [DataSciencePipelines](#datasciencepipelines)
- [Kserve](#kserve)
- [Kueue](#kueue)
- [ModelMeshServing](#modelmeshserving)
- [ModelRegistry](#modelregistry)
- [Ray](#ray)
- [TrustyAI](#trustyai)
- [Workbenches](#workbenches)

| Field | Description |
| --- | --- |
| `managementState` _[ManagementState](#managementstate)_ | Set to one of the following values: <br /><br /> - "Managed" : the operator is actively managing the component and trying to keep it active. It will only upgrade the component if it is safe to do so <br /><br /> - "Removed" : the operator is actively managing the component and will not install it, or if it is installed, the operator will try to remove it |
| `devFlags` _[DevFlags](#devflags)_ | Add developer fields |




#### DevFlags



DevFlags defines list of fields that can be used by developers to test customizations. This is not recommended to be used in production environment.

_Appears in:_
- [Component](#component)

| Field | Description |
| --- | --- |
| `manifests` _[ManifestsConfig](#manifestsconfig) array_ | List of custom manifests for the given component |


#### ManifestsConfig





_Appears in:_
- [DevFlags](#devflags)

| Field | Description |
| --- | --- |
| `uri` _string_ | uri is the URI point to a git repo with tag/branch. e.g.  https://github.com/org/repo/tarball/<tag/branch> |
| `contextDir` _string_ | contextDir is the relative path to the folder containing manifests in a repository |
| `sourcePath` _string_ | sourcePath is the subpath within contextDir where kustomize builds start. Examples include any sub-folder or path: `base`, `overlays/dev`, `default`, `odh` etc. |



## datasciencecluster.opendatahub.io/dashboard

Package dashboard provides utility functions to config Open Data Hub Dashboard: A web dashboard that displays
installed Open Data Hub components with easy access to component UIs and documentation



#### Dashboard



Dashboard struct holds the configuration for the Dashboard component.

_Appears in:_
- [Components](#components)

| Field | Description |
| --- | --- |
| `Component` _[Component](#component)_ |  |



## datasciencecluster.opendatahub.io/datasciencepipelines

Package datasciencepipelines provides utility functions to config Data Science Pipelines:
Pipeline solution for end to end MLOps workflows that support the Kubeflow Pipelines SDK and Tekton



#### DataSciencePipelines



DataSciencePipelines struct holds the configuration for the DataSciencePipelines component.

_Appears in:_
- [Components](#components)

| Field | Description |
| --- | --- |
| `Component` _[Component](#component)_ |  |



## datasciencecluster.opendatahub.io/kserve

Package kserve provides utility functions to config Kserve as the Controller for serving ML models on arbitrary frameworks



#### DefaultDeploymentMode

_Underlying type:_ _string_



_Appears in:_
- [Kserve](#kserve)



#### Kserve



Kserve struct holds the configuration for the Kserve component.

_Appears in:_
- [Components](#components)

| Field | Description |
| --- | --- |
| `Component` _[Component](#component)_ |  |
| `serving` _[ServingSpec](#servingspec)_ | Serving configures the KNative-Serving stack used for model serving. A Service Mesh (Istio) is prerequisite, since it is used as networking layer. |
| `defaultDeploymentMode` _[DefaultDeploymentMode](#defaultdeploymentmode)_ | Configures the default deployment mode for Kserve. This can be set to 'Serverless' or 'RawDeployment'. The value specified in this field will be used to set the default deployment mode in the 'inferenceservice-config' configmap for Kserve If no default deployment mode is specified, Kserve will use Serverless mode |



## datasciencecluster.opendatahub.io/kueue




#### Kueue



Kueue struct holds the configuration for the Kueue component.

_Appears in:_
- [Components](#components)

| Field | Description |
| --- | --- |
| `Component` _[Component](#component)_ |  |



## datasciencecluster.opendatahub.io/modelmeshserving

Package modelmeshserving provides utility functions to config MoModelMesh, a general-purpose model serving management/routing layer



#### ModelMeshServing



ModelMeshServing struct holds the configuration for the ModelMeshServing component.

_Appears in:_
- [Components](#components)

| Field | Description |
| --- | --- |
| `Component` _[Component](#component)_ |  |



## datasciencecluster.opendatahub.io/modelregistry

Package modelregistry provides utility functions to config ModelRegistry, an ML Model metadata repository service



#### ModelRegistry



ModelRegistry struct holds the configuration for the ModelRegistry component.

_Appears in:_
- [Components](#components)

| Field | Description |
| --- | --- |
| `Component` _[Component](#component)_ |  |



## datasciencecluster.opendatahub.io/ray

Package ray provides utility functions to config Ray as part of the stack
which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists



#### Ray



Ray struct holds the configuration for the Ray component.

_Appears in:_
- [Components](#components)

| Field | Description |
| --- | --- |
| `Component` _[Component](#component)_ |  |



## datasciencecluster.opendatahub.io/trustyai

Package trustyai provides utility functions to config TrustyAI, a bias/fairness and explainability toolkit



#### TrustyAI



TrustyAI struct holds the configuration for the TrustyAI component.

_Appears in:_
- [Components](#components)

| Field | Description |
| --- | --- |
| `Component` _[Component](#component)_ |  |



## datasciencecluster.opendatahub.io/v1


### Resource Types
- [DataScienceCluster](#datasciencecluster)



#### AuthSpec





_Appears in:_
- [ServiceMeshSpec](#servicemeshspec)

| Field | Description |
| --- | --- |
| `namespace` _string_ | Namespace where it is deployed. If not provided, the default is to use '-auth-provider' suffix on the ApplicationsNamespace of the DSCI. |
| `audiences` _string_ | Audiences is a list of the identifiers that the resource server presented with the token identifies as. Audience-aware token authenticators will verify that the token was intended for at least one of the audiences in this list. If no audiences are provided, the audience will default to the audience of the Kubernetes apiserver (kubernetes.default.svc). |


#### CertType

_Underlying type:_ _string_



_Appears in:_
- [CertificateSpec](#certificatespec)



#### CertificateSpec



CertificateSpec represents the specification of the certificate securing communications of an Istio Gateway.

_Appears in:_
- [IngressGatewaySpec](#ingressgatewayspec)

| Field | Description |
| --- | --- |
| `secretName` _string_ | SecretName specifies the name of the Kubernetes Secret resource that contains a TLS certificate secure HTTP communications for the KNative network. |
| `type` _[CertType](#certtype)_ | Type specifies if the TLS certificate should be generated automatically, or if the certificate is provided by the user. Allowed values are: * SelfSigned: A certificate is going to be generated using an own private key. * Provided: Pre-existence of the TLS Secret (see SecretName) with a valid certificate is assumed. |


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


#### ControlPlaneSpec





_Appears in:_
- [ServiceMeshSpec](#servicemeshspec)

| Field | Description |
| --- | --- |
| `name` _string_ | Name is a name Service Mesh Control Plane. Defaults to "data-science-smcp". |
| `namespace` _string_ | Namespace is a namespace where Service Mesh is deployed. Defaults to "istio-system". |
| `metricsCollection` _string_ | MetricsCollection specifies if metrics from components on the Mesh namespace should be collected. Setting the value to "Istio" will collect metrics from the control plane and any proxies on the Mesh namespace (like gateway pods). Setting to "None" will disable metrics collection. |


#### DataScienceCluster



DataScienceCluster is the Schema for the datascienceclusters API.



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `datasciencecluster.opendatahub.io/v1`
| `kind` _string_ | `DataScienceCluster`
| `kind` _string_ | Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
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
| `relatedObjects` _[ObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#objectreference-v1-core) array_ | RelatedObjects is a list of objects created and maintained by this operator. Object references will be added to this list after they have been created AND found in the cluster. |
| `errorMessage` _string_ |  |
| `installedComponents` _object (keys:string, values:boolean)_ | List of components with status if installed or not |


#### IngressGatewaySpec



IngressGatewaySpec represents the configuration of the Ingress Gateways.

_Appears in:_
- [ServingSpec](#servingspec)

| Field | Description |
| --- | --- |
| `domain` _string_ | Domain specifies the DNS name for intercepting ingress requests coming from outside the cluster. Most likely, you will want to use a wildcard name, like *.example.com. If not set, the domain of the OpenShift Ingress is used. If you choose to generate a certificate, this is the domain used for the certificate request. |
| `certificate` _[CertificateSpec](#certificatespec)_ | Certificate specifies configuration of the TLS certificate securing communications of the for Ingress Gateway. |


#### ServiceMeshSpec



ServiceMeshSpec configures Service Mesh.

_Appears in:_
- [DSCInitializationSpec](#dscinitializationspec)

| Field | Description |
| --- | --- |
| `managementState` _[ManagementState](#managementstate)_ |  |
| `controlPlane` _[ControlPlaneSpec](#controlplanespec)_ | ControlPlane holds configuration of Service Mesh used by Opendatahub. |
| `auth` _[AuthSpec](#authspec)_ | Auth holds configuration of authentication and authorization services used by Service Mesh in Opendatahub. |


#### ServingSpec



ServingSpec specifies the configuration for the KNative Serving components and their bindings with the Service Mesh.

_Appears in:_
- [Kserve](#kserve)

| Field | Description |
| --- | --- |
| `managementState` _[ManagementState](#managementstate)_ |  |
| `name` _string_ | Name specifies the name of the KNativeServing resource that is going to be created to instruct the KNative Operator to deploy KNative serving components. This resource is created in the "knative-serving" namespace. |
| `ingressGateway` _[IngressGatewaySpec](#ingressgatewayspec)_ | IngressGateway allows to customize some parameters for the Istio Ingress Gateway that is bound to KNative-Serving. |



## datasciencecluster.opendatahub.io/workbenches

Package workbenches provides utility functions to config Workbenches to secure Jupyter Notebook in Kubernetes environments with support for OAuth



#### Workbenches



Workbenches struct holds the configuration for the Workbenches component.

_Appears in:_
- [Components](#components)

| Field | Description |
| --- | --- |
| `Component` _[Component](#component)_ |  |



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
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
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
| `relatedObjects` _[ObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#objectreference-v1-core) array_ | RelatedObjects is a list of objects created and maintained by this operator. Object references will be added to this list after they have been created AND found in the cluster |
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



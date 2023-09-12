package datasciencecluster

//+kubebuilder:rbac:groups="datasciencecluster.opendatahub.io",resources=datascienceclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="datasciencecluster.opendatahub.io",resources=datascienceclusters/finalizers,verbs=update;patch
//+kubebuilder:rbac:groups="datasciencecluster.opendatahub.io",resources=datascienceclusters,verbs=get;list;watch;create;update;patch;delete

/* This is for DSP */
//+kubebuilder:rbac:groups="datasciencepipelinesapplications.opendatahub.io",resources=datasciencepipelinesapplications/status,verbs=update;patch;get
//+kubebuilder:rbac:groups="datasciencepipelinesapplications.opendatahub.io",resources=datasciencepipelinesapplications/finalizers,verbs=update;patch
//+kubebuilder:rbac:groups="datasciencepipelinesapplications.opendatahub.io",resources=datasciencepipelinesapplications,verbs=create;delete;list;update;watch;patch;get
//+kubebuilder:rbac:groups="image.openshift.io",resources=imagestreamtags,verbs=get
//+kubebuilder:rbac:groups="authentication.k8s.io",resources=tokenreviews,verbs=create
//+kubebuilder:rbac:groups="authorization.k8s.io",resources=subjectaccessreviews,verbs=create

/* This is for dashboard */
// +kubebuilder:rbac:groups="opendatahub.io",resources=odhdashboardconfigs,verbs=create;get;patch;watch;update;delete;list
// +kubebuilder:rbac:groups="console.openshift.io",resources=odhquickstarts,verbs=create;get;patch;list;delete
// +kubebuilder:rbac:groups="dashboard.opendatahub.io",resources=odhdocuments,verbs=create;get;patch;list;delete
// +kubebuilder:rbac:groups="dashboard.opendatahub.io",resources=odhapplications,verbs=create;get;patch;list;delete

// +kubebuilder:rbac:groups="operators.coreos.com",resources=clusterserviceversions,verbs=get;list;watch
// +kubebuilder:rbac:groups="operators.coreos.com",resources=customresourcedefinitions,verbs=create;get;patch;delete
// +kubebuilder:rbac:groups="operators.coreos.com",resources=subscriptions,verbs=get;list;watch
// +kubebuilder:rbac:groups="operators.coreos.com",resources=operatorconditions,verbs=get;list;watch

// +kubebuilder:rbac:groups="apiextensions.k8s.io",resources=customresourcedefinitions,verbs=get;list;watch

// +kubebuilder:rbac:groups="user.openshift.io",resources=users,verbs=list;watch;patch;delete

// +kubebuilder:rbac:groups="template.openshift.io",resources=templates,verbs=*

// +kubebuilder:rbac:groups="tekton.dev",resources=*,verbs=*

// +kubebuilder:rbac:groups="snapshot.storage.k8s.io",resources=volumesnapshots,verbs=create;delete;patch;get

// +kubebuilder:rbac:groups="serving.kserve.io",resources=trainedmodels/status,verbs=update;patch;delete
// +kubebuilder:rbac:groups="serving.kserve.io",resources=trainedmodels,verbs=create;delete;list;update;watch;patch
// +kubebuilder:rbac:groups="serving.kserve.io",resources=servingruntimes/status,verbs=update;patch
// +kubebuilder:rbac:groups="serving.kserve.io",resources=servingruntimes/finalizers,verbs=create;delete;list;update;watch;patch;get
// +kubebuilder:rbac:groups="serving.kserve.io",resources=servingruntimes,verbs=*
// +kubebuilder:rbac:groups="serving.kserve.io",resources=predictors/status,verbs=update;patch;delete
// +kubebuilder:rbac:groups="serving.kserve.io",resources=predictors/finalizers,verbs=update;patch
// +kubebuilder:rbac:groups="serving.kserve.io",resources=predictors,verbs=create;delete;list;update;watch;patch
// +kubebuilder:rbac:groups="serving.kserve.io",resources=inferenceservices/status,verbs=update;patch;delete
// +kubebuilder:rbac:groups="serving.kserve.io",resources=inferenceservices/finalizers,verbs=create;delete;list;update;watch;patch;get
// +kubebuilder:rbac:groups="serving.kserve.io",resources=inferenceservices,verbs=create;delete;list;update;watch;patch;get
// +kubebuilder:rbac:groups="serving.kserve.io",resources=inferencegraphs/status,verbs=update;patch;delete
// +kubebuilder:rbac:groups="serving.kserve.io",resources=inferencegraphs,verbs=create;delete;list;update;watch;patch
// +kubebuilder:rbac:groups="serving.kserve.io",resources=clusterservingruntimes/status,verbs=update;patch;delete
// +kubebuilder:rbac:groups="serving.kserve.io",resources=clusterservingruntimes/finalizers,verbs=create;delete;list;update;watch;patch
// +kubebuilder:rbac:groups="serving.kserve.io",resources=clusterservingruntimes,verbs=create;delete;list;update;watch;patch

// +kubebuilder:rbac:groups="serving.knative.dev",resources=services/status,verbs=update;patch;delete
// +kubebuilder:rbac:groups="serving.knative.dev",resources=services/finalizers,verbs=create;delete;list;watch;update;patch
// +kubebuilder:rbac:groups="serving.knative.dev",resources=services,verbs=create;delete;list;watch;update;patch

// +kubebuilder:rbac:groups="security.openshift.io",resources=securitycontextconstraints,verbs=*,resourceNames=restricted
// +kubebuilder:rbac:groups="security.openshift.io",resources=securitycontextconstraints,verbs=*,resourceNames=anyuid
// +kubebuilder:rbac:groups="security.openshift.io",resources=securitycontextconstraints,verbs=*

// +kubebuilder:rbac:groups="route.openshift.io",resources=routes,verbs=get;list;watch;create;delete;update;patch

// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=*

// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=rolebindings,verbs=*

// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterroles,verbs=*

// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterrolebindings,verbs=*

// +kubebuilder:rbac:groups="ray.io",resources=rayservices,verbs=create;delete;list;watch;update;patch;get
// +kubebuilder:rbac:groups="ray.io",resources=rayjobs,verbs=create;delete;list;update;watch;patch;get
// +kubebuilder:rbac:groups="ray.io",resources=rayclusters,verbs=create;delete;list;patch;get

// +kubebuilder:rbac:groups="operator.openshift.io",resources=consoles,verbs=list;watch;patch;delete

// +kubebuilder:rbac:groups="oauth.openshift.io",resources=oauthclients,verbs=*

// +kubebuilder:rbac:groups="networking.k8s.io",resources=networkpolicies,verbs=get;create;list;watch;delete;update;patch
// +kubebuilder:rbac:groups="networking.k8s.io",resources=ingresses,verbs=create;delete;list;update;watch;patch;get

// +kubebuilder:rbac:groups="networking.istio.io",resources=virtualservices/status,verbs=update;patch;delete
// +kubebuilder:rbac:groups="networking.istio.io",resources=virtualservices/finalizers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="networking.istio.io",resources=virtualservices,verbs=*

// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=servicemonitors,verbs=get;create;delete;update;watch;list;patch
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=podmonitors,verbs=get;create;delete;update;watch;list;patch
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=prometheusrules,verbs=get;create;patch;delete
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=prometheuses,verbs=get;create;patch;delete

// +kubebuilder:rbac:groups="mcad.ibm.com",resources=appwrappers,verbs=create;delete;list;patch;get

// +kubebuilder:rbac:groups="machinelearning.seldon.io",resources=seldondeployments,verbs=*

// +kubebuilder:rbac:groups="machine.openshift.io",resources=machinesets,verbs=list;patch;delete
// +kubebuilder:rbac:groups="machine.openshift.io",resources=machineautoscalers,verbs=list;patch;delete

/* TODO: cleanup once kfdef is not needed*/
// +kubebuilder:rbac:groups="kubeflow.org",resources=*,verbs=*
// +kubebuilder:rbac:groups="kfdef.apps.kubeflow.org",resources=kfdefs,verbs=get;list;watch;patch;delete

// +kubebuilder:rbac:groups="integreatly.org",resources=rhmis,verbs=list;watch;patch;delete

// +kubebuilder:rbac:groups="image.openshift.io",resources=imagestreams,verbs=patch;create;update;delete
// +kubebuilder:rbac:groups="image.openshift.io",resources=imagestreams,verbs=create;list;watch;patch;delete

// +kubebuilder:rbac:groups="extensions",resources=replicasets,verbs=*
// +kubebuilder:rbac:groups="extensions",resources=ingresses,verbs=list;watch;patch;delete;get

// +kubebuilder:rbac:groups="custom.tekton.dev",resources=pipelineloops,verbs=*

// +kubebuilder:rbac:groups="core",resources=services/finalizers,verbs=create;delete;list;update;watch;patch
// +kubebuilder:rbac:groups="core",resources=services,verbs=get;create;watch;update;patch;list;delete
// +kubebuilder:rbac:groups="core",resources=services,verbs=*
// +kubebuilder:rbac:groups="*",resources=services,verbs=*

// +kubebuilder:rbac:groups="core",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups="core",resources=secrets,verbs=*

// +kubebuilder:rbac:groups="core",resources=rhmis,verbs=watch;list

// +kubebuilder:rbac:groups="core",resources=pods/log,verbs=*
// +kubebuilder:rbac:groups="core",resources=pods/exec,verbs=*
// +kubebuilder:rbac:groups="core",resources=pods,verbs=*

// +kubebuilder:rbac:groups="core",resources=persistentvolumes,verbs=*
// +kubebuilder:rbac:groups="core",resources=persistentvolumeclaims,verbs=*

// +kubebuilder:rbac:groups="core",resources=namespaces/finalizers,verbs=update;list;watch;patch;delete
// +kubebuilder:rbac:groups="core",resources=namespaces,verbs=get;create;patch;delete;watch;update;list

// +kubebuilder:rbac:groups="core",resources=events,verbs=get;create;watch;update;list;patch;delete
// +kubebuilder:rbac:groups="events.k8s.io",resources=events,verbs=list;watch;patch;delete

// +kubebuilder:rbac:groups="core",resources=endpoints,verbs=watch;list

// +kubebuilder:rbac:groups="core",resources=configmaps/status,verbs=get;update;patch;delete
// +kubebuilder:rbac:groups="core",resources=configmaps,verbs=get;create;watch;patch;delete;list

// +kubebuilder:rbac:groups="core",resources=clusterversions,verbs=watch;list
// +kubebuilder:rbac:groups="config.openshift.io",resources=clusterversions,verbs=watch;list

// +kubebuilder:rbac:groups="coordination.k8s.io",resources=leases,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups="controller-runtime.sigs.k8s.io",resources=controllermanagerconfigs,verbs=get;create;patch;delete

// +kubebuilder:rbac:groups="codeflare.codeflare.dev",resources=mcads,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="codeflare.codeflare.dev",resources=instascales,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups="cert-manager.io",resources=certificates;issuers,verbs=create;patch

// +kubebuilder:rbac:groups="build.openshift.io",resources=builds,verbs=create;patch;delete;list;watch
// +kubebuilder:rbac:groups="build.openshift.io",resources=buildconfigs/instantiate,verbs=create;patch;delete;get;list;watch
// +kubebuilder:rbac:groups="build.openshift.io",resources=buildconfigs,verbs=list;watch;create;patch;delete

// +kubebuilder:rbac:groups="batch",resources=jobs/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="batch",resources=jobs,verbs=*
// +kubebuilder:rbac:groups="batch",resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="batch",resources=cronjobs,verbs=create;get;patch

// +kubebuilder:rbac:groups="autoscaling",resources=horizontalpodautoscalers,verbs=watch;create;update;delete;list;patch
// +kubebuilder:rbac:groups="autoscaling.openshift.io",resources=machinesets,verbs=list;patch;delete
// +kubebuilder:rbac:groups="autoscaling.openshift.io",resources=machineautoscalers,verbs=list;patch;delete

// +kubebuilder:rbac:groups="authorization.openshift.io",resources=roles,verbs=*
// +kubebuilder:rbac:groups="authorization.openshift.io",resources=rolebindings,verbs=*
// +kubebuilder:rbac:groups="authorization.openshift.io",resources=clusterroles,verbs=*
// +kubebuilder:rbac:groups="authorization.openshift.io",resources=clusterrolebindings,verbs=*

// +kubebuilder:rbac:groups="argoproj.io",resources=workflows,verbs=*

// +kubebuilder:rbac:groups="apps",resources=statefulsets,verbs=*

// +kubebuilder:rbac:groups="apps",resources=replicasets,verbs=*

// +kubebuilder:rbac:groups="apps",resources=deployments/finalizers,verbs=*
// +kubebuilder:rbac:groups="core",resources=deployments,verbs=*
// +kubebuilder:rbac:groups="apps",resources=deployments,verbs=*
// +kubebuilder:rbac:groups="*",resources=deployments,verbs=*
// +kubebuilder:rbac:groups="extensions",resources=deployments,verbs=*

// +kubebuilder:rbac:groups="apps.openshift.io",resources=deploymentconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="apps.openshift.io",resources=deploymentconfigs/instantiate,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups="apiextensions.k8s.io",resources=customresourcedefinitions,verbs=get;list;watch;create;patch;delete

// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=validatingwebhookconfigurations,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=mutatingwebhookconfigurations,verbs=create;delete;list;update;watch;patch

/* This is needed to derterminiate cluster type */
// +kubebuilder:rbac:groups="addons.managed.openshift.io",resources=addons,verbs=get

// +kubebuilder:rbac:groups="*",resources=statefulsets,verbs=create;update;get;list;watch;patch;delete

// +kubebuilder:rbac:groups="*",resources=replicasets,verbs=*

// +kubebuilder:rbac:groups="*",resources=customresourcedefinitions,verbs=get;list;watch

// +kubebuilder:rbac:groups="maistra.io",resources=servicemeshcontrolplanes,verbs=create;get;list;patch;update;use;watch
// +kubebuilder:rbac:groups="maistra.io",resources=servicemeshmemberrolls,verbs=create;get;list;patch;update;use;watch
// +kubebuilder:rbac:groups="maistra.io",resources=servicemeshmembers,verbs=create;get;list;patch;update;use;watch
// +kubebuilder:rbac:groups="maistra.io",resources=servicemeshmembers/finalizers,verbs=create;get;list;patch;update;use;watch

/* Only for RHODS */
// +kubebuilder:rbac:groups="user.openshift.io",resources=groups,verbs=get;create;list;watch;patch;delete
// +kubebuilder:rbac:groups="console.openshift.io",resources=consolelinks,verbs=create;get;patch;delete

package dscinitialization

// +kubebuilder:rbac:groups="dscinitialization.opendatahub.io",resources=dscinitializations/status,verbs=get;update;patch;delete
// +kubebuilder:rbac:groups="dscinitialization.opendatahub.io",resources=dscinitializations/finalizers,verbs=get;update;patch;delete
// +kubebuilder:rbac:groups="dscinitialization.opendatahub.io",resources=dscinitializations,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups="features.opendatahub.io",resources=featuretrackers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="features.opendatahub.io",resources=featuretrackers/status,verbs=get;update;patch;delete
// +kubebuilder:rbac:groups="features.opendatahub.io",resources=featuretrackers/finalizers,verbs=update;patch;get

/* Auth */
// +kubebuilder:rbac:groups="config.openshift.io",resources=authentications,verbs=get;watch;list

/* Service Mesh Integration */
// +kubebuilder:rbac:groups="maistra.io",resources=servicemeshcontrolplanes,verbs=create;get;list;patch;update;use;watch
// +kubebuilder:rbac:groups="maistra.io",resources=servicemeshmemberrolls,verbs=create;get;list;patch;update;use;watch
// +kubebuilder:rbac:groups="maistra.io",resources=servicemeshmembers,verbs=create;get;list;patch;update;use;watch
// +kubebuilder:rbac:groups="maistra.io",resources=servicemeshmembers/finalizers,verbs=create;get;list;patch;update;use;watch
// +kubebuilder:rbac:groups="networking.istio.io",resources=virtualservices/status,verbs=update;patch;delete;get
// +kubebuilder:rbac:groups="networking.istio.io",resources=virtualservices/finalizers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="networking.istio.io",resources=virtualservices,verbs=*
// +kubebuilder:rbac:groups="networking.istio.io",resources=gateways,verbs=*
// +kubebuilder:rbac:groups="networking.istio.io",resources=envoyfilters,verbs=*
// +kubebuilder:rbac:groups="security.istio.io",resources=authorizationpolicies,verbs=*
// +kubebuilder:rbac:groups="authorino.kuadrant.io",resources=authconfigs,verbs=*
// +kubebuilder:rbac:groups="operator.authorino.kuadrant.io",resources=authorinos,verbs=*

// TODO: move to monitoring own file
// +kubebuilder:rbac:groups="route.openshift.io",resources=routers/metrics,verbs=get
// +kubebuilder:rbac:groups="route.openshift.io",resources=routers/federate,verbs=get
// +kubebuilder:rbac:groups="image.openshift.io",resources=registry/metrics,verbs=get

// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=servicemonitors,verbs=get;create;delete;update;watch;list;patch;deletecollection
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=podmonitors,verbs=get;create;delete;update;watch;list;patch
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=prometheusrules,verbs=get;create;patch;delete;deletecollection
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=prometheuses,verbs=get;create;patch;delete;deletecollection
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=prometheuses/finalizers,verbs=get;create;patch;delete;deletecollection
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=prometheuses/status,verbs=get;create;patch;delete;deletecollection
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=alertmanagers,verbs=get;create;patch;delete;deletecollection
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=alertmanagers/finalizers,verbs=get;create;patch;delete;deletecollection
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=alertmanagers/status,verbs=get;create;patch;delete;deletecollection
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=alertmanagerconfigs,verbs=get;create;patch;delete;deletecollection
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=thanosrulers,verbs=get;create;patch;delete;deletecollection
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=thanosrulers/finalizers,verbs=get;create;patch;delete;deletecollection
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=thanosrulers/status,verbs=get;create;patch;delete;deletecollection
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=probes,verbs=get;create;patch;delete;deletecollection

//+kubebuilder:rbac:groups=services.platform.opendatahub.io,resources=monitorings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=services.platform.opendatahub.io,resources=monitorings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=services.platform.opendatahub.io,resources=monitorings/finalizers,verbs=update

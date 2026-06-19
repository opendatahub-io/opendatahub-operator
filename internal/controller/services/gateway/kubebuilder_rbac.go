package gateway

// Gateway
// CR management
// +kubebuilder:rbac:groups=services.platform.opendatahub.io,resources=gatewayconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=services.platform.opendatahub.io,resources=gatewayconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=services.platform.opendatahub.io,resources=gatewayconfigs/finalizers,verbs=update

// Dashboard CR (read-only, to check if Dashboard is deployed before creating redirects)
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=dashboards,verbs=get;list;watch

// OpenShift APIServer config (TLS security profile for kube-auth-proxy)
// +kubebuilder:rbac:groups="config.openshift.io",resources=apiservers,verbs=get;list;watch

// Gateway API resources (what the controller actually creates)
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways;gatewayclasses;httproutes,verbs=get;list;watch;create;update;patch;delete
// Gateway controller creates and manages the following Istio resources
// +kubebuilder:rbac:groups=networking.istio.io,resources=destinationrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.istio.io,resources=envoyfilters,verbs=get;list;watch;create;update;patch;delete

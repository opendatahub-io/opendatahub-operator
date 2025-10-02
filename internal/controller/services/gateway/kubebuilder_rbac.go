package gateway

// Gateway CR management
// +kubebuilder:rbac:groups=services.platform.opendatahub.io,resources=gatewayconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=services.platform.opendatahub.io,resources=gatewayconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=services.platform.opendatahub.io,resources=gatewayconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="operator.openshift.io",resources=ingresscontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups="oauth.openshift.io",resources=oauthclients,verbs=create;delete;list;watch;update;patch;get

// Gateway API resources (what the controller actually creates)
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways;gatewayclasses;httproutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="networking.istio.io",resources=envoyfilters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="networking.istio.io",resources=destinationrules,verbs=get;list;watch;create;update;patch;delete

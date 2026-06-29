package modules

// +kubebuilder:rbac:groups="config.opendatahub.io",resources=platforms,verbs=get;list;watch
// +kubebuilder:rbac:groups="config.opendatahub.io",resources=platforms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="config.opendatahub.io",resources=platforms/finalizers,verbs=update

// AIGateway: new mega module
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=aigateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=aigateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=aigateways/finalizers,verbs=update

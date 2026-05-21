package modules

// +kubebuilder:rbac:groups="config.opendatahub.io",resources=platforms,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="config.opendatahub.io",resources=platforms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="config.opendatahub.io",resources=platforms/finalizers,verbs=update

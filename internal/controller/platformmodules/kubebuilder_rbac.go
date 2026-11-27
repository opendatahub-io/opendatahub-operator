package platformmodules

// +kubebuilder:rbac:groups="config.opendatahub.io",resources=platformmodules,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="config.opendatahub.io",resources=platformmodules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="config.opendatahub.io",resources=platformmodules/finalizers,verbs=update

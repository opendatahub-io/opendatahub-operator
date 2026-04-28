package eks

// +kubebuilder:rbac:groups=infrastructure.opendatahub.io,resources=ekskubernetesengines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.opendatahub.io,resources=ekskubernetesengines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.opendatahub.io,resources=ekskubernetesengines/finalizers,verbs=update

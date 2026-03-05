package azure

// +kubebuilder:rbac:groups=infrastructure.opendatahub.io,resources=azurekubernetesengines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.opendatahub.io,resources=azurekubernetesengines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.opendatahub.io,resources=azurekubernetesengines/finalizers,verbs=update

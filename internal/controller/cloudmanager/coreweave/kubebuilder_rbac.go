package coreweave

// +kubebuilder:rbac:groups=infrastructure.opendatahub.io,resources=coreweavekubernetesengines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.opendatahub.io,resources=coreweavekubernetesengines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.opendatahub.io,resources=coreweavekubernetesengines/finalizers,verbs=update

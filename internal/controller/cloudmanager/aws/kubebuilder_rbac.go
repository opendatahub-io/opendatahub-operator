package aws

// +kubebuilder:rbac:groups=infrastructure.opendatahub.io,resources=awskubernetesengines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.opendatahub.io,resources=awskubernetesengines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.opendatahub.io,resources=awskubernetesengines/finalizers,verbs=update
